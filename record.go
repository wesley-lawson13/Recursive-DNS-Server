package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type record struct {
	Name    string
	QType   uint16
	Class   uint16
	TTL     uint32
	DLen    uint16
	Data    string
	AddedAt time.Time // cache bookkeeping only; not part of the DNS wire format
}

// only support two record types for now
var TYPES = map[uint16]string{
	1: "A",
	5: "CNAME",
}

// create a DNS record from one line in a zone file
func newRecord(recordLine string) (record, error) {
	var rec record

	fields := strings.Fields(recordLine)
	if len(fields) != 5 {
		return rec, fmt.Errorf("invalid zone format: had %d fields, expecting 5", len(fields))
	}

	ttl, err := strconv.ParseUint(fields[1], 10, 32)
	if err != nil {
		return rec, errors.New("invalid record ttl")
	}

	class, err2 := classToUint(fields[2])
	if err2 != nil {
		return rec, err2
	}

	qtype, err3 := typeToUint(fields[3])
	if err3 != nil {
		return rec, err3
	}

	// will never err -- qtype will always be verified to this point
	dlen, err4 := getDataLen(qtype, fields[4])
	if err4 != nil {
		return rec, err4
	}

	rec.Name = fields[0]
	rec.QType = qtype
	rec.Class = class
	rec.TTL = uint32(ttl)
	rec.DLen = dlen
	rec.Data = fields[4]

	return rec, nil
}

// create a DNS record from fields parsed from a message
func newAnswerRecord(name string, qtype uint16, class uint16, ttl uint32, data string) (record, error) {
	var rec record

	rec.Name = name
	rec.QType = qtype
	rec.Class = class
	rec.TTL = ttl
	rec.Data = data

	var err error
	rec.DLen, err = getDataLen(qtype, data)
	if err != nil {
		return rec, err
	}

	return rec, nil
}

// convert record type from string to int
func typeToUint(typeStr string) (uint16, error) {
	for i, s := range TYPES {
		if typeStr == s {
			return i, nil
		}
	}
	return 0, errors.New("invalid record type")
}

// convert record type from int to string
func typeToString(typeInt uint16) string {
	typeStr, ok := TYPES[typeInt]
	if ok {
		return typeStr
	} else {
		return fmt.Sprintf("%v", typeInt)
	}
}

// convert record class from string to int
func classToUint(classStr string) (uint16, error) {
	if classStr == "IN" {
		return uint16(1), nil
	} else {
		return 0, errors.New("invalid record class")
	}
}

// convert record class from int to string
func classToString(classInt uint16) string {
	if classInt == 1 {
		return "IN"
	} else {
		return fmt.Sprintf("%v", classInt)
	}
}

// set the data length based on the record type and data
func getDataLen(qtype uint16, data string) (uint16, error) {
	// A records store an IP address
	if qtype == 1 {
		return 4, nil

		// CNAME records store names
	} else if qtype == 5 {
		// wire format: each dot maps 1:1 to a label-length byte, plus 1 leading
		return uint16(len(data) + 2), nil
	}

	return 0, errors.New("this server only supports A and CNAME records")
}

func (r record) String() string {
	qtype := typeToString(r.QType)
	qclass := classToString(r.Class)
	return fmt.Sprintf("%v, %v, %v, %v, %v", r.Name, qtype, qclass, r.TTL, r.Data)
}
