package main

import (
	"testing"
)

func TestTypeToString(t *testing.T) {
	tests := []struct {
		name     string
		typeInt  uint16
		expected string
	}{
		{"'CNAME' type", 5, "CNAME"},
		{"'A' type", 1, "A"},
		{"invalid typeInt", 8, "8"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := typeToString(tt.typeInt)
			if res != tt.expected {
				t.Errorf("typeToString(%d) = %s, want %s", tt.typeInt, res, tt.expected)
			}
		})
	}
}

func TestTypeToUint(t *testing.T) {
	tests := []struct {
		name     string
		typeStr  string
		wantErr  bool
		expected uint16
	}{
		{"'CNAME' type", "CNAME", false, 5},
		{"'A' type", "A", false, 1},
		{"invalid type", "invalid", true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := typeToUint(tt.typeStr)

			if (err != nil) != tt.wantErr {
				t.Fatalf("typeToUint(%s) error = %v, wantErr = %v", tt.typeStr, err, tt.wantErr)
			}

			if res != tt.expected {
				t.Errorf("typeToUint(%s) = %d, want %d", tt.typeStr, res, tt.expected)
			}

		})
	}
}

func TestClassToString(t *testing.T) {
	tests := []struct {
		name     string
		classInt uint16
		expected string
	}{
		{"'IN' type", 1, "IN"},
		{"invalid type", 2, "2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := classToString(tt.classInt)

			if res != tt.expected {
				t.Errorf("classToString(%d) = %s, want %s", tt.classInt, res, tt.expected)
			}
		})
	}
}

func TestClassToUint(t *testing.T) {
	tests := []struct {
		name     string
		classStr string
		wantErr  bool
		expected uint16
	}{
		{"'IN' type", "IN", false, 1},
		{"invalid type", "test", true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := classToUint(tt.classStr)

			if (err != nil) != tt.wantErr {
				t.Fatalf("classToUint(%s) error = %v, wantErr = %v", tt.classStr, err, tt.wantErr)
			}

			if res != tt.expected {
				t.Errorf("classToUint(%s) = %d, want %d", tt.classStr, res, tt.expected)
			}
		})
	}
}

const cNameDataStr = "test data string"

func TestGetDataLen(t *testing.T) {
	tests := []struct {
		name     string
		qtype    uint16
		data     string
		wantErr  bool
		expected uint16
	}{
		{"qtype 1", 1, cNameDataStr, false, 4},
		{"qtype 5", 5, cNameDataStr, false, uint16(len(cNameDataStr) + 2)},
		{"invalid qtype", 4, cNameDataStr, true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := getDataLen(tt.qtype, tt.data)

			if (err != nil) != tt.wantErr {
				t.Fatalf("getDataLen(%d, %s) error = %v, wantErr = %v", tt.qtype, tt.data, err, tt.wantErr)
			}

			if res != tt.expected {
				t.Errorf("getDataLen(%d, %s) = %d, want = %d", tt.qtype, tt.data, res, tt.expected)
			}
		})
	}

}

var aRec = record{
	Name:  "test",
	QType: 1,
	Class: 1,
	TTL:   300,
	Data:  "1.2.3.4",
	DLen:  4,
}

var cNameRec = record{
	Name:  "test",
	QType: 5,
	Class: 1,
	TTL:   300,
	Data:  cNameDataStr,
	DLen:  uint16(len(cNameDataStr) + 2),
}

var errRec = record{
	Name:  "test",
	QType: 4,
	Class: 1,
	TTL:   300,
	Data:  cNameDataStr,
	DLen:  0,
}

func TestNewAnswerRecord(t *testing.T) {

	tests := []struct {
		name         string
		rName        string
		qtype, class uint16
		ttl          uint32
		data         string
		wantErr      bool
		expected     record
	}{
		{"'A' record answer", aRec.Name, aRec.QType, aRec.Class, aRec.TTL, aRec.Data, false, aRec},
		{"'CNAME' record answer", cNameRec.Name, cNameRec.QType, cNameRec.Class, cNameRec.TTL, cNameRec.Data, false, cNameRec},
		{"invalid qtype", errRec.Name, errRec.QType, errRec.Class, errRec.TTL, errRec.Data, true, errRec},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			rec, err := newAnswerRecord(tt.rName, tt.qtype, tt.class, tt.ttl, tt.data)

			if (err != nil) != tt.wantErr {
				t.Fatalf("newAnswerRecord(...) error = %v, wantErr = %v", err, tt.wantErr)
			}

			if rec != tt.expected {
				t.Errorf("newAnswerRecord(...) = %v, want = %v", rec, tt.expected)
			}
		})
	}
}

const happyRecordLine = "test  300 IN  A   1.2.3.4"

func TestNewRecord(t *testing.T) {
	tests := []struct {
		name       string
		recordLine string
		wantErr    bool
		expected   record
	}{
		{"happy path", happyRecordLine, false, aRec},
		{"too many fields", happyRecordLine + " errHere", true, record{}},
		{"invalid ttl", "test -1 IN A 1.2.3.4", true, record{}},
		{"class err", "test 300 wrongClass A 1.2.3.4", true, record{}},
		{"type err", "test 300 IN wrongType 1.2.3.4", true, record{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, err := newRecord(tt.recordLine)

			if (err != nil) != tt.wantErr {
				t.Fatalf("newRecord(%s) error = %v, wantErr = %v", tt.recordLine, err, tt.wantErr)
			}

			if rec != tt.expected {
				t.Errorf("newRecord(%s) = %v, expected %v", tt.recordLine, rec, tt.expected)
			}
		})
	}
}

func TestRecordString(t *testing.T) {
	expected := "test, A, IN, 300, 1.2.3.4"

	t.Run("Record String() Method", func(t *testing.T) {
		res := aRec.String()

		if res != expected {
			t.Fatalf("record.String() = %v, want = %v", res, expected)
		}
	})
}
