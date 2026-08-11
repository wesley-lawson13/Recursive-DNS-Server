package main

import "testing"

const testZone = "testdata/test.zone"

func TestNewZone(t *testing.T) {
	tests := []struct {
		name           string
		filename       string
		wantErr        bool
		expectedRecLen int
	}{
		{"happy path", testZone, false, 4},
		{"bad zone data", "testdata/bad.zone", true, 0},
		{"empty zone", "testdata/empty.zone", true, 0},
		{"missing zone file", "testdata/nonexistent.zone", true, 0},
		{"blank line in zone", "testdata/blankline.zone", true, 0},
		{"line exceeds scanner buffer", "testdata/toolong.zone", true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			zn, err := newZone(tt.filename)

			if (err != nil) != tt.wantErr {
				t.Fatalf("newZone(%s) error = %v, wantErr = %v", tt.filename, err, tt.wantErr)
			}

			if len(zn.records) != tt.expectedRecLen {
				t.Errorf("newZone(%s) records = %d, want %d", tt.filename, len(zn.records), tt.expectedRecLen)
			}
		})
	}

}

func createZone(t *testing.T) zone {
	t.Helper()

	zn, err := newZone(testZone)
	if err != nil {
		t.Fatalf("Error in helper createZone function: %v", err)
	}

	return zn
}

var testQn = question{
	Name:  "test.csci3363.net",
	QType: 1,
	Class: 1,
}

var missingQn = question{
	Name:  "non-existent record",
	QType: 1,
	Class: 1,
}

var mailQn = question{
	Name:  "mail.csci3363.net",
	QType: 1,
	Class: 1,
}

var aliasCnameQn = question{
	Name:  "alias.csci3363.net",
	QType: 5,
	Class: 1,
}

// same name as the CNAME record above, but the wrong QType
var aliasWrongTypeQn = question{
	Name:  "alias.csci3363.net",
	QType: 1,
	Class: 1,
}

func TestLookup(t *testing.T) {

	zn := createZone(t)

	tests := []struct {
		name           string
		qn             question
		expectedRetLen int
	}{
		{"happy path", testQn, 2},
		{"single match", mailQn, 1},
		{"CNAME match", aliasCnameQn, 1},
		{"non-existent record", missingQn, 0},
		{"name matches but QType doesn't", aliasWrongTypeQn, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := zn.lookup(tt.qn)

			if len(rec) != tt.expectedRetLen {
				t.Fatalf("zone.lookup(%s) length = %d, want %d", tt.qn.Name, len(rec), tt.expectedRetLen)
			}
		})
	}
}

func TestZoneString(t *testing.T) {

	zn := zone{
		records: []record{aRec, cNameRec},
	}

	expected := aRec.String() + "\n" + cNameRec.String() + "\n"

	t.Run("Zone String() Method", func(t *testing.T) {
		res := zn.String()

		if res != expected {
			t.Fatalf("zone.String() = %v, want %v", res, expected)
		}
	})

	t.Run("Empty zone String() Method", func(t *testing.T) {
		empty := zone{}
		res := empty.String()

		if res != "" {
			t.Fatalf("zone.String() = %q, want empty string", res)
		}
	})
}
