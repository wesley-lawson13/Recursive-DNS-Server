package main

import (
	"net"
	"reflect"
	"sync"
	"testing"
)

func initPendingMap(t *testing.T) *pendingQueryMap {
	t.Helper()

	pq := &pendingQueryMap{
		items:  make(map[uint16]queryInfo),
		nextID: 0,
	}

	return pq
}

var genericClient = net.TCPAddr{
	IP:   net.ParseIP("127.0.0.1"),
	Port: 8080,
}

var baseQueryInf = queryInfo{
	query:  message{}, // init to an empty message for tests that don't need the message at all
	client: &genericClient,
}

func TestStore(t *testing.T) {
	pq := initPendingMap(t)

	for want := uint16(0); want < 5; want++ {
		if id := pq.store(baseQueryInf); id != want {
			t.Fatalf("pq.store() call #%d = %d, want = %d", want, id, want)
		}
	}

}

func TestPop(t *testing.T) {
	pq := initPendingMap(t)
	const n = 1000

	tests := []struct {
		name     string
		id       uint16
		wantOk   bool
		expected queryInfo
	}{
		{"pop on existing id", 0, true, baseQueryInf},
		{"re-accessing id", 0, false, baseQueryInf},
		{"non-existent id", n + 1, false, baseQueryInf},
	}
	for range n {
		pq.store(baseQueryInf)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, ok := pq.pop(tt.id)

			if ok != tt.wantOk {
				t.Fatalf("pq.pop(%d) 'ok' = %v, want %v", tt.id, ok, tt.wantOk)
			}

			if ok && !reflect.DeepEqual(info, tt.expected) {
				t.Fatalf("pq.pop(%d) = %v, want %v", tt.id, info, tt.expected)
			}

		})
	}
}

func TestStoreAndPopConcurrent(t *testing.T) {
	pq := initPendingMap(t)
	const n = 1000

	var wg sync.WaitGroup
	ids := make(chan uint16, n)

	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := pq.store(baseQueryInf)
			ids <- id
		}()
	}
	wg.Wait()
	close(ids)

	var wg2 sync.WaitGroup
	for id := range ids {
		wg2.Add(1)
		go func(id uint16) {
			defer wg2.Done()
			_, ok := pq.pop(id)
			if !ok {
				t.Errorf("pop(%d) failed, expected to find entry", id)
			}
		}(id)
	}
	wg2.Wait()

	if len(pq.items) != 0 {
		t.Fatalf("items = %d, want 0 after popping everything", len(pq.items))
	}
}
