package main

import (
	"net"
	"slices"
	"testing"
	"time"
)

func initCache(t *testing.T) *cache {
	t.Helper()

	return &cache{items: make(map[cacheKey][]record)}
}

func TestMakeKey(t *testing.T) {
	q := question{Name: "example.com", QType: 1, Class: 1}
	got := makeKey(q)
	want := cacheKey{Name: "example.com", QType: 1, Class: 1}

	if got != want {
		t.Errorf("makeKey(%+v) = %+v, want %+v", q, got, want)
	}
}

func TestAddRecords(t *testing.T) {
	q := question{Name: "example.com", QType: 1, Class: 1}

	tests := []struct {
		name     string
		calls    [][]record
		wantData []string // Data of each surviving record, in order; nil means no entry
	}{
		{
			name:     "new entry is cached",
			calls:    [][]record{{aRec}},
			wantData: []string{aRec.Data},
		},
		{
			name:     "empty answers does nothing, existing entry survives",
			calls:    [][]record{{aRec}, nil},
			wantData: []string{aRec.Data},
		},
		{
			name:     "a later call overwrites the earlier one, not merges",
			calls:    [][]record{{aRec}, {cNameRec}},
			wantData: []string{cNameRec.Data},
		},
		{
			name:     "an empty first call never creates an entry",
			calls:    [][]record{nil},
			wantData: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := initCache(t)
			for _, ans := range tt.calls {
				ch.addRecords(message{ans: ans}, message{qn: q})
			}

			got, ok := ch.returnRecords(q)
			if tt.wantData == nil {
				if ok {
					t.Fatalf("returnRecords() = %v, true, want no entry", got)
				}
				return
			}
			if !ok {
				t.Fatal("expected cache hit")
			}

			gotData := make([]string, len(got))
			for i, r := range got {
				gotData[i] = r.Data
			}
			if !slices.Equal(gotData, tt.wantData) {
				t.Errorf("cached Data = %v, want %v", gotData, tt.wantData)
			}
		})
	}
}

func TestReturnRecords_Miss(t *testing.T) {
	ch := initCache(t)
	q := question{Name: "missing.com", QType: 1, Class: 1}

	got, ok := ch.returnRecords(q)
	if ok || got != nil {
		t.Errorf("returnRecords() = %v, %v, want nil, false", got, ok)
	}
}

func TestRecordExpired(t *testing.T) {
	base := time.Now()

	tests := []struct {
		name    string
		ttl     uint32
		elapsed time.Duration
		want    bool
	}{
		{"well under TTL", 300, 10 * time.Second, false},
		{"well past TTL", 300, 400 * time.Second, true},
		{"exactly at TTL boundary", 300, 300 * time.Second, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := record{TTL: tt.ttl, AddedAt: base.Add(-tt.elapsed)}
			if got := rec.expired(base); got != tt.want {
				t.Errorf("expired() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCacheUpdate_Survival(t *testing.T) {
	q := question{Name: "example.com", QType: 1, Class: 1}
	key := makeKey(q)

	tests := []struct {
		name     string
		seed     []record
		wantData []string // nil means the key is deleted entirely
	}{
		{
			name:     "expired record is removed and the key deleted",
			seed:     []record{{Data: "expired", TTL: 10, AddedAt: time.Now().Add(-20 * time.Second)}},
			wantData: nil,
		},
		{
			name:     "non-expired record survives",
			seed:     []record{{Data: "surviving", TTL: 300, AddedAt: time.Now().Add(-10 * time.Second)}},
			wantData: []string{"surviving"},
		},
		{
			name: "partial expiry keeps only the survivors",
			seed: []record{
				{Data: "expired", TTL: 10, AddedAt: time.Now().Add(-20 * time.Second)},
				{Data: "surviving", TTL: 300, AddedAt: time.Now().Add(-10 * time.Second)},
			},
			wantData: []string{"surviving"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := initCache(t)
			ch.items[key] = tt.seed

			ch.update()

			got, ok := ch.items[key]
			if tt.wantData == nil {
				if ok {
					t.Fatalf("ch.items[key] = %v, want key deleted", got)
				}
				return
			}

			gotData := make([]string, len(got))
			for i, r := range got {
				gotData[i] = r.Data
			}
			if !slices.Equal(gotData, tt.wantData) {
				t.Errorf("surviving Data = %v, want %v", gotData, tt.wantData)
			}
		})
	}
}

func TestCacheUpdate_DecrementsTTLAndResetsAddedAt(t *testing.T) {
	ch := initCache(t)
	q := question{Name: "example.com", QType: 1, Class: 1}
	key := makeKey(q)

	const ttl = 300
	const elapsed = 100 * time.Second
	addedAt := time.Now().Add(-elapsed)
	ch.items[key] = []record{{Data: aRec.Data, TTL: ttl, AddedAt: addedAt}}

	ch.update()

	got, ok := ch.items[key]
	if !ok || len(got) != 1 {
		t.Fatalf("ch.items[key] = %v, %v, want 1 surviving record", got, ok)
	}

	rec := got[0]
	const want = ttl - 100
	if rec.TTL > want || rec.TTL < want-2 {
		t.Errorf("TTL = %d, want ~%d (allowing a couple seconds of test slack)", rec.TTL, want)
	}
	if !rec.AddedAt.After(addedAt) {
		t.Errorf("AddedAt = %v, want reset forward past %v", rec.AddedAt, addedAt)
	}
}

// compares an uncached (real network) DNS resolution against a cache hit
// for the same answer, to demonstrate the improvement in
// performance the cache provides.
func TestCacheLatencyImprovement(t *testing.T) {
	const host = "example.com"

	if _, err := net.LookupHost(host); err != nil {
		t.Skipf("skipping: no network access available: %v", err)
	}

	ch := initCache(t)
	q := question{Name: host, QType: 1, Class: 1}
	reply := message{qn: q, ans: []record{aRec}}
	ch.addRecords(reply, reply)

	const iterations = 20

	uncachedStart := time.Now()
	for range iterations {
		if _, err := net.LookupHost(host); err != nil {
			t.Fatalf("uncached lookup failed: %v", err)
		}
	}
	uncachedElapsed := time.Since(uncachedStart)

	cachedStart := time.Now()
	for range iterations {
		if _, ok := ch.returnRecords(q); !ok {
			t.Fatal("expected cache hit")
		}
	}
	cachedElapsed := time.Since(cachedStart)

	t.Logf("uncached (network) resolution: %v total, %v/op", uncachedElapsed, uncachedElapsed/iterations)
	t.Logf("cached resolution:             %v total, %v/op", cachedElapsed, cachedElapsed/iterations)
	t.Logf("cache is ~%.0fx faster", float64(uncachedElapsed)/float64(cachedElapsed))

	if cachedElapsed >= uncachedElapsed {
		t.Errorf("expected cached lookups (%v) to be faster than uncached network lookups (%v)", cachedElapsed, uncachedElapsed)
	}
}

// BenchmarkCacheHit isolates the raw cost of a cache lookup, useful for
// tracking cache performance on its own (run with: go test -bench=CacheHit).
func BenchmarkCacheHit(b *testing.B) {
	ch := &cache{items: make(map[cacheKey][]record)}
	q := question{Name: "example.com", QType: 1, Class: 1}
	reply := message{qn: q, ans: []record{aRec}}
	ch.addRecords(reply, reply)

	b.ResetTimer()
	for range b.N {
		if _, ok := ch.returnRecords(q); !ok {
			b.Fatal("expected cache hit")
		}
	}
}
