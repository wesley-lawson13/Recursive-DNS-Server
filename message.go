package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"strings"
)

type message struct {
	hdr headers
	flg flags
	qn  question
	ans []record
}

type headers struct {
	Id           uint16
	Flags        uint16
	NumQuestions uint16
	NumAnswers   uint16
	NumAuthRRs   uint16
	NumAddRRs    uint16
}

type flags struct {
	QR     byte
	OpCode byte
	AA     byte
	TC     byte
	RD     byte
	RA     byte
	RCode  byte
}

type question struct {
	Name  string
	QType uint16
	Class uint16
}

// create a DNS message from the buffer
func newMessage(buf []byte) (message, error) {
	var msg message

	// use the bytes type to read the through the buffer
	bufr := bytes.NewReader(buf)

	// parse the 6 initial header values
	err := msg.parseHeaders(bufr)
	if err != nil {
		return msg, err
	}

	// we are ignoring auth and additional records
	msg.hdr.NumAuthRRs = 0
	msg.hdr.NumAddRRs = 0

	// parse a single question
	if msg.hdr.NumQuestions == 1 {
		err = msg.parseQuestion(bufr, buf)
		if err != nil {
			return msg, err
		}
	}

	// parse all the answers
	msg.ans = make([]record, 0)
	for range msg.hdr.NumAnswers {
		err = msg.parseAnswer(bufr, buf)
		if err != nil {
			return msg, err
		}
	}

	return msg, nil
}

// read the 6 initial header values from the message buffer
func (m *message) parseHeaders(bufr *bytes.Reader) error {
	// read all 6 header values
	err := binary.Read(bufr, binary.BigEndian, &m.hdr)
	if err != nil {
		return err
	}

	// parse out the flags independently
	m.parseFlags()

	return nil
}

// fill in each individual flag value from the full flags
func (m *message) parseFlags() {
	m.flg.QR = byte(m.hdr.Flags >> 15 & 0x1)
	m.flg.OpCode = byte(m.hdr.Flags >> 11 & 0xf)
	m.flg.AA = byte(m.hdr.Flags >> 10 & 0x1)
	m.flg.TC = byte(m.hdr.Flags >> 9 & 0x1)
	m.flg.RD = byte(m.hdr.Flags >> 8 & 0x1)
	m.flg.RA = byte(m.hdr.Flags >> 7 & 0x1)
	m.flg.RCode = byte(m.hdr.Flags & 0xf)
}

// read a compressed name from the buffer
func parseCompressedName(labellen byte, bufr *bytes.Reader, buf []byte) (string, error) {
	var next byte
	err := binary.Read(bufr, binary.BigEndian, &next)
	if err != nil {
		return "", err
	}

	offset := uint16(labellen)<<8 | uint16(next)
	offset = offset & 0x3ff // strip the top 2 flag bits (0xC0) per RFC 1035 §4.1.4 pointer format

	return parseName(bytes.NewReader(buf[offset:]), buf)
}

// read the next name label from the message buffer.
// done=true means the name is fully consumed (either via a compression pointer or a parse error);
// the caller must stop reading further labels.
func parseLabel(bufr *bytes.Reader, buf []byte) (string, error, bool) {
	var labelLen, next byte
	var label string

	// read the length first
	err := binary.Read(bufr, binary.BigEndian, &labelLen)

	// handle label compression, top two bits are 1s
	if labelLen >= 0xc0 {
		label, lerr := parseCompressedName(labelLen, bufr, buf)
		return label, lerr, true
	}

	// then read length bytes as chars
	for err == nil && len(label) != int(labelLen) {
		err = binary.Read(bufr, binary.BigEndian, &next)
		label += string(next)
	}
	if err != nil {
		return "", err, true
	}

	return label, nil, false
}

// read a name out of the message buffer
func parseName(bufr *bytes.Reader, buf []byte) (string, error) {
	var name string

	// read labels until the zero byte
	label, err, done := parseLabel(bufr, buf)
	for err == nil && len(label) != 0 {
		name += label + "."
		if done {
			break
		}
		label, err, done = parseLabel(bufr, buf)
	}
	if err != nil {
		return "", err
	}

	// get rid of the extra dot added in the loop
	return name[:len(name)-1], nil
}

// read a full question question from the message buffer
func (m *message) parseQuestion(bufr *bytes.Reader, buf []byte) error {
	// parse the name first
	var err error
	m.qn.Name, err = parseName(bufr, buf)
	if err != nil {
		return err
	}

	// parse the type
	err = binary.Read(bufr, binary.BigEndian, &m.qn.QType)
	if err != nil {
		return err
	}

	// parse the class
	err = binary.Read(bufr, binary.BigEndian, &m.qn.Class)
	if err != nil {
		return err
	}

	return nil
}

// read an IP out of the message buffer, into a string
func parseIP(bufr *bytes.Reader) (string, error) {
	var ip [4]byte

	err := binary.Read(bufr, binary.BigEndian, &ip)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%v.%v.%v.%v", ip[0], ip[1], ip[2], ip[3]), nil

}

// read a full answer from the message buffer
func (m *message) parseAnswer(bufr *bytes.Reader, buf []byte) error {
	// parse the name first
	var err error
	var name string
	name, err = parseName(bufr, buf)
	if err != nil {
		return err
	}

	// parse the type
	var qtype uint16
	err = binary.Read(bufr, binary.BigEndian, &qtype)
	if err != nil {
		return err
	}

	// parse the class
	var class uint16
	err = binary.Read(bufr, binary.BigEndian, &class)
	if err != nil {
		return err
	}

	// parse the TTL
	var ttl uint32
	err = binary.Read(bufr, binary.BigEndian, &ttl)
	if err != nil {
		return err
	}

	// parse the data length
	var tmp uint16
	err = binary.Read(bufr, binary.BigEndian, &tmp)
	if err != nil {
		return err
	}

	// parse the data
	var data string
	if qtype == 1 {
		// 1 is an A record
		data, err = parseIP(bufr)
		if err != nil {
			return err
		}
	} else {
		// otherwise assume CNAME
		data, err = parseName(bufr, buf)
		if err != nil {
			return err
		}
	}

	rec, err := newAnswerRecord(name, qtype, class, ttl, data)
	if err != nil {
		return err
	}

	m.ans = append(m.ans, rec)

	return nil
}

// create a response message
func newResponse(query message, recs []record, authoritative bool) message {
	var resp message

	// fill in the header fields and flags
	resp.makeHeader(query, recs, authoritative)

	// copy the question section over
	resp.qn = query.qn

	// copy in the answser records
	resp.ans = recs

	return resp
}

func (resp *message) makeHeader(query message, recs []record, authoritative bool) {
	// ID, OpCode, RD flag, and #Qs all come from request message
	resp.hdr.Id = query.hdr.Id
	resp.flg.OpCode = query.flg.OpCode
	resp.flg.RD = query.flg.RD
	resp.hdr.NumQuestions = query.hdr.NumQuestions

	// this is a response
	resp.flg.QR = 1

	// we support recursion
	resp.flg.RA = 1

	if authoritative {
		// we are the authoritative server for these answers
		resp.flg.AA = 1
	} else {
		// we are not authoritative for these answers (cached)
		resp.flg.AA = 0
	}

	if len(recs) != 0 {
		// if we found a record, set the flags to include an answer
		resp.flg.RCode = 0
		resp.hdr.NumAnswers = uint16(len(recs))
	} else {
		// if we didn't find a record, send NXDomain
		resp.flg.RCode = 3
		resp.hdr.NumAnswers = 0
	}

	// zero out the other flags and fields
	resp.flg.TC = 0
	resp.hdr.NumAuthRRs = 0
	resp.hdr.NumAddRRs = 0

	resp.makeFlags()
}

// set the full 16-bit flags field from individual flags
func (m *message) makeFlags() {
	m.hdr.Flags = 0

	m.hdr.Flags |= uint16(m.flg.QR&0x1) << 15
	m.hdr.Flags |= uint16(m.flg.OpCode&0xf) << 11
	m.hdr.Flags |= uint16(m.flg.AA&0x1) << 10
	m.hdr.Flags |= uint16(m.flg.TC&0x1) << 9
	m.hdr.Flags |= uint16(m.flg.RD&0x1) << 8
	m.hdr.Flags |= uint16(m.flg.RA&0x1) << 7
	m.hdr.Flags |= uint16(m.flg.RCode & 0xf)
}

// send the response message
func (resp *message) send(socket net.PacketConn, client net.Addr) error {
	// first have to create a buffer with the message contents
	buf, err := resp.fillBuffer()
	if err != nil {
		return err
	}

	// then send the message
	_, err2 := socket.WriteTo(buf[:], client)
	if err2 != nil {
		return err2
	}

	return nil
}

// turn the message into a []byte
func (resp *message) fillBuffer() ([]byte, error) {
	var bufw bytes.Buffer

	// write the six header values into the buffer
	if err := resp.fillHeaders(&bufw); err != nil {
		return bufw.Bytes(), err
	}

	// write the question section
	if err := resp.fillQuestion(&bufw); err != nil {
		return bufw.Bytes(), err
	}

	// write the answer section
	if err := resp.fillAnswer(&bufw); err != nil {
		return bufw.Bytes(), err
	}

	return bufw.Bytes(), nil
}

// write the header fields into the buffer
func (resp *message) fillHeaders(buf *bytes.Buffer) error {
	if err := binary.Write(buf, binary.BigEndian, resp.hdr); err != nil {
		return err
	}
	return nil
}

// write a name into the buffer
func fillName(name string, buf *bytes.Buffer) error {
	labels := strings.Split(name, ".")

	// fill in each label in the name
	for _, label := range labels {
		length := byte(len(label))

		// fill in the label length
		if err := binary.Write(buf, binary.BigEndian, length); err != nil {
			return err
		}

		// fill in each character as one byte
		for _, char := range label {
			if err := binary.Write(buf, binary.BigEndian, byte(char)); err != nil {
				return err
			}
		}
	}

	// fill in the zero byte so the name is done
	zero := byte(0)
	if err := binary.Write(buf, binary.BigEndian, zero); err != nil {
		return err
	}

	return nil
}

// write an IPv4 address into the buffer
func fillIP(ip string, buf *bytes.Buffer) error {
	octets := strings.Split(ip, ".")

	// fill in each octet as a single byte
	for _, octet := range octets {
		num, err := strconv.ParseUint(octet, 10, 8)
		if err != nil {
			return err
		}

		if err := binary.Write(buf, binary.BigEndian, byte(num)); err != nil {
			return err
		}
	}

	return nil
}

// write the question section into the buffer
func (resp *message) fillQuestion(buf *bytes.Buffer) error {
	// only support one question
	if resp.hdr.NumQuestions != 1 {
		return nil
	}

	// fill in the question name
	if err := fillName(resp.qn.Name, buf); err != nil {
		return err
	}

	// fill in the question type
	if err := binary.Write(buf, binary.BigEndian, resp.qn.QType); err != nil {
		return err
	}

	// fill in the question class
	if err := binary.Write(buf, binary.BigEndian, resp.qn.Class); err != nil {
		return err
	}

	return nil
}

// write the answer section into the buffer
func (resp *message) fillAnswer(buf *bytes.Buffer) error {

	// write each answer record into the buffer
	for _, rec := range resp.ans {
		// fill in the answer name
		if err := fillName(rec.Name, buf); err != nil {
			return err
		}

		// fill in the answer type
		if err := binary.Write(buf, binary.BigEndian, rec.QType); err != nil {
			return err
		}

		// fill in the answer class
		if err := binary.Write(buf, binary.BigEndian, rec.Class); err != nil {
			return err
		}

		// fill in the answer ttl
		if err := binary.Write(buf, binary.BigEndian, rec.TTL); err != nil {
			return err
		}

		// fill in the answer data length
		if err := binary.Write(buf, binary.BigEndian, rec.DLen); err != nil {
			return err
		}

		// fill in the answer data
		if rec.QType == 1 {
			// A records contain IPs
			if err := fillIP(rec.Data, buf); err != nil {
				return err
			}
		} else {
			// otherwise, it's a name
			if err := fillName(rec.Data, buf); err != nil {
				return err
			}
		}
	}

	return nil
}

func (m message) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("ID:    0x%04x\n", m.hdr.Id))
	sb.WriteString(fmt.Sprintf("Flags: 0x%04x\n", m.hdr.Flags))
	if m.flg.QR == 0 && m.flg.OpCode == 0 {
		sb.WriteString("- Standard Query\n")
	} else if m.flg.QR == 1 && m.flg.RCode == 0 {
		sb.WriteString("- Standard Response\n")
	} else if m.flg.QR == 1 && m.flg.RCode == 3 {
		sb.WriteString("- Response NXDomain\n")
	} else {
		sb.WriteString("- Unexpected QR/opcode\n")
	}
	if m.flg.RD == 1 {
		sb.WriteString("- Recursion Requested\n")
	}
	if m.flg.RA == 1 {
		sb.WriteString("- Recursion Available\n")
	}
	if m.flg.AA == 1 {
		sb.WriteString("- Authoritative Answer\n")
	}
	sb.WriteString(fmt.Sprintf("# Questions: %v\n", m.hdr.NumQuestions))
	sb.WriteString(fmt.Sprintf("# Answers:   %v\n", m.hdr.NumAnswers))
	sb.WriteString(fmt.Sprintf("# Auth RRs:  %v\n", m.hdr.NumAuthRRs))
	sb.WriteString(fmt.Sprintf("# Addl RRs:  %v\n", m.hdr.NumAddRRs))

	if m.hdr.NumQuestions == 1 {
		sb.WriteString("Questions:\n")
		qtype := typeToString(m.qn.QType)
		qclass := classToString(m.qn.Class)
		sb.WriteString(fmt.Sprintf("- %v, %v, %v\n", m.qn.Name, qtype, qclass))
	}

	if m.hdr.NumAnswers != 0 {
		sb.WriteString("Answers:\n")
		for _, rec := range m.ans {
			sb.WriteString(fmt.Sprintf("- %v\n", rec))
		}
	}

	return sb.String()
}
