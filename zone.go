package main

import (
	"bufio"
	"errors"
	"os"
	"strings"
)

// the zone is just a slice of records
type zone struct {
	records []record
}

// create the zone struct from a given zone file
func newZone(filename string) (zone, error) {
	var zn zone
	zn.records = make([]record, 0)

	file, err := os.Open(filename)
	if err != nil {
		return zn, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	// each line of the zone file contains one record
	for scanner.Scan() {
		line := scanner.Text()

		// parse one line into a record struct
		rec, err2 := newRecord(line)
		if err2 != nil {
			return zone{}, err2
		}

		zn.records = append(zn.records, rec)
	}

	if err := scanner.Err(); err != nil {
		return zone{}, err
	}

	if len(zn.records) == 0 {
		return zn, errors.New("no records in zone file")
	}

	return zn, nil
}

// find all records in the zone matching a given question
func (z *zone) lookup(q question) []record {

	// start with a slice with capacity 1
	records := make([]record, 0, 1)

	for _, rec := range z.records {
		// the name, type, and class all have to match
		if rec.Name == q.Name && rec.QType == q.QType && rec.Class == q.Class {
			records = append(records, rec)
		}
	}

	return records
}

func (z zone) String() string {
	var sb strings.Builder

	for _, record := range z.records {
		sb.WriteString(record.String() + "\n")
	}

	return sb.String()
}
