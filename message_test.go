package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

// --- HELPER ---

func encodeName(name string) []byte {
	var buf bytes.Buffer
	for _, label := range strings.Split(name, ".") {
		buf.WriteByte(byte(len(label)))
		buf.WriteString(label)
	}
	buf.WriteByte(0)
	return buf.Bytes()
}

// --- HELPER ---

func be16(v uint16) []byte {
	return []byte{byte(v >> 8), byte(v)}
}

func TestParseFlags(t *testing.T) {
	tests := []struct {
		name     string
		raw      uint16
		expected flags
	}{
		{"standard query", 0x0000, flags{}},
		{"standard response, no answer flags", 1 << 15, flags{QR: 1}},
		{"nxdomain response", (1 << 15) | 3, flags{QR: 1, RCode: 3}},
		{"rd/ra/aa/tc set", (1 << 8) | (1 << 7) | (1 << 10) | (1 << 9), flags{AA: 1, TC: 1, RD: 1, RA: 1}},
		{"all bits set", 0xFFFF, flags{QR: 1, OpCode: 0xf, AA: 1, TC: 1, RD: 1, RA: 1, RCode: 0xf}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := message{hdr: headers{Flags: tt.raw}}
			m.parseFlags()

			if m.flg != tt.expected {
				t.Errorf("parseFlags(0x%04x) = %+v, want %+v", tt.raw, m.flg, tt.expected)
			}
		})
	}
}

func TestParseHeaders(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		h := headers{Id: 0x1234, Flags: 0x8180, NumQuestions: 1, NumAnswers: 2, NumAuthRRs: 0, NumAddRRs: 0}

		var raw bytes.Buffer
		if err := binary.Write(&raw, binary.BigEndian, h); err != nil {
			t.Fatalf("failed to build fixture: %v", err)
		}

		var m message
		bufr := bytes.NewReader(raw.Bytes())
		if err := m.parseHeaders(bufr); err != nil {
			t.Fatalf("parseHeaders() error = %v", err)
		}

		if m.hdr != h {
			t.Errorf("parseHeaders() hdr = %+v, want %+v", m.hdr, h)
		}

		wantFlg := flags{QR: 1, OpCode: 0, AA: 0, TC: 0, RD: 1, RA: 1, RCode: 0}
		if m.flg != wantFlg {
			t.Errorf("parseHeaders() flg = %+v, want %+v", m.flg, wantFlg)
		}
	})

	t.Run("truncated buffer", func(t *testing.T) {
		var m message
		bufr := bytes.NewReader([]byte{0x00, 0x01})

		if err := m.parseHeaders(bufr); err == nil {
			t.Error("parseHeaders() with truncated buffer expected error, got nil")
		}
	})
}

func TestParseLabel(t *testing.T) {
	t.Run("regular label", func(t *testing.T) {
		buf := []byte{3, 'a', 'b', 'c'}
		bufr := bytes.NewReader(buf)

		label, err, done := parseLabel(bufr, buf)
		if err != nil {
			t.Fatalf("parseLabel() error = %v", err)
		}
		if label != "abc" || done {
			t.Errorf("parseLabel() = (%q, done=%v), want (\"abc\", done=false)", label, done)
		}
	})

	t.Run("zero-length label", func(t *testing.T) {
		buf := []byte{0}
		bufr := bytes.NewReader(buf)

		label, err, done := parseLabel(bufr, buf)
		if err != nil {
			t.Fatalf("parseLabel() error = %v", err)
		}
		if label != "" || done {
			t.Errorf("parseLabel() = (%q, done=%v), want (\"\", done=false)", label, done)
		}
	})

	t.Run("compressed label", func(t *testing.T) {
		target := encodeName("x")
		full := append(append([]byte{}, target...), 0xC0, 0x00)

		bufr := bytes.NewReader(full[len(target):])
		label, err, done := parseLabel(bufr, full)
		if err != nil {
			t.Fatalf("parseLabel() error = %v", err)
		}
		if label != "x" || !done {
			t.Errorf("parseLabel() = (%q, done=%v), want (\"x\", done=true)", label, done)
		}
	})

	t.Run("truncated label data", func(t *testing.T) {
		buf := []byte{5, 'a', 'b'}
		bufr := bytes.NewReader(buf)

		_, err, done := parseLabel(bufr, buf)
		if err == nil {
			t.Error("parseLabel() with truncated data expected error, got nil")
		}
		if !done {
			t.Error("parseLabel() on error expected done=true")
		}
	})
}

func TestParseName(t *testing.T) {
	t.Run("simple name", func(t *testing.T) {
		buf := encodeName("test.csci3363.net")
		name, err := parseName(bytes.NewReader(buf), buf)
		if err != nil {
			t.Fatalf("parseName() error = %v", err)
		}
		if name != "test.csci3363.net" {
			t.Errorf("parseName() = %q, want %q", name, "test.csci3363.net")
		}
	})

	t.Run("compressed name", func(t *testing.T) {
		first := encodeName("abc.def")
		pointer := []byte{0xC0, 0x00}
		full := append(append([]byte{}, first...), pointer...)

		bufr := bytes.NewReader(full[len(first):])
		name, err := parseName(bufr, full)
		if err != nil {
			t.Fatalf("parseName() error = %v", err)
		}
		if name != "abc.def" {
			t.Errorf("parseName() = %q, want %q", name, "abc.def")
		}
	})

	t.Run("truncated name", func(t *testing.T) {
		buf := []byte{5, 'a', 'b'}
		_, err := parseName(bytes.NewReader(buf), buf)
		if err == nil {
			t.Error("parseName() with truncated buffer expected error, got nil")
		}
	})
}

func TestParseCompressedName(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		target := encodeName("abc")
		full := append(append([]byte{}, target...), 0xC0, 0x00)

		bufr := bytes.NewReader(full[len(target)+1:])
		name, err := parseCompressedName(0xC0, bufr, full)
		if err != nil {
			t.Fatalf("parseCompressedName() error = %v", err)
		}
		if name != "abc" {
			t.Errorf("parseCompressedName() = %q, want %q", name, "abc")
		}
	})

	t.Run("truncated pointer", func(t *testing.T) {
		bufr := bytes.NewReader([]byte{})
		_, err := parseCompressedName(0xC0, bufr, []byte{})
		if err == nil {
			t.Error("parseCompressedName() with truncated pointer expected error, got nil")
		}
	})
}

var msgTestQn = question{Name: "example.com", QType: 1, Class: 1}

func TestParseQuestion(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		buf := append(append([]byte{}, encodeName(msgTestQn.Name)...), be16(msgTestQn.QType)...)
		buf = append(buf, be16(msgTestQn.Class)...)

		var m message
		if err := m.parseQuestion(bytes.NewReader(buf), buf); err != nil {
			t.Fatalf("parseQuestion() error = %v", err)
		}
		if m.qn != msgTestQn {
			t.Errorf("parseQuestion() qn = %+v, want %+v", m.qn, msgTestQn)
		}
	})

	t.Run("truncated buffer", func(t *testing.T) {
		buf := encodeName(msgTestQn.Name)
		var m message
		if err := m.parseQuestion(bytes.NewReader(buf), buf); err == nil {
			t.Error("parseQuestion() with truncated buffer expected error, got nil")
		}
	})
}

func TestParseIP(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		buf := []byte{1, 2, 3, 4}
		ip, err := parseIP(bytes.NewReader(buf))
		if err != nil {
			t.Fatalf("parseIP() error = %v", err)
		}
		if ip != "1.2.3.4" {
			t.Errorf("parseIP() = %q, want %q", ip, "1.2.3.4")
		}
	})

	t.Run("truncated buffer", func(t *testing.T) {
		buf := []byte{1, 2}
		_, err := parseIP(bytes.NewReader(buf))
		if err == nil {
			t.Error("parseIP() with truncated buffer expected error, got nil")
		}
	})
}

// --- HELPER ---

func buildAnswerBytes(rec record) []byte {
	buf := append([]byte{}, encodeName(rec.Name)...)
	buf = append(buf, be16(rec.QType)...)
	buf = append(buf, be16(rec.Class)...)

	var ttlBuf bytes.Buffer
	binary.Write(&ttlBuf, binary.BigEndian, rec.TTL)
	buf = append(buf, ttlBuf.Bytes()...)

	buf = append(buf, be16(rec.DLen)...)

	if rec.QType == 1 {
		octets := strings.Split(rec.Data, ".")
		for _, o := range octets {
			var b byte
			fmt.Sscanf(o, "%d", &b)
			buf = append(buf, b)
		}
	} else {
		buf = append(buf, encodeName(rec.Data)...)
	}

	return buf
}

func TestParseAnswer(t *testing.T) {
	t.Run("A record", func(t *testing.T) {
		buf := buildAnswerBytes(aRec)

		var m message
		if err := m.parseAnswer(bytes.NewReader(buf), buf); err != nil {
			t.Fatalf("parseAnswer() error = %v", err)
		}
		if len(m.ans) != 1 || m.ans[0] != aRec {
			t.Errorf("parseAnswer() ans = %+v, want [%+v]", m.ans, aRec)
		}
	})

	t.Run("CNAME record", func(t *testing.T) {
		buf := buildAnswerBytes(cNameRec)

		var m message
		if err := m.parseAnswer(bytes.NewReader(buf), buf); err != nil {
			t.Fatalf("parseAnswer() error = %v", err)
		}
		if len(m.ans) != 1 || m.ans[0] != cNameRec {
			t.Errorf("parseAnswer() ans = %+v, want [%+v]", m.ans, cNameRec)
		}
	})

	t.Run("truncated buffer", func(t *testing.T) {
		buf := encodeName(aRec.Name)
		var m message
		if err := m.parseAnswer(bytes.NewReader(buf), buf); err == nil {
			t.Error("parseAnswer() with truncated buffer expected error, got nil")
		}
	})
}

func TestNewMessage(t *testing.T) {
	t.Run("query, no answers", func(t *testing.T) {
		h := headers{Id: 0xABCD, Flags: 0x0100, NumQuestions: 1, NumAnswers: 0}

		var raw bytes.Buffer
		binary.Write(&raw, binary.BigEndian, h)
		raw.Write(encodeName(msgTestQn.Name))
		raw.Write(be16(msgTestQn.QType))
		raw.Write(be16(msgTestQn.Class))

		m, err := newMessage(raw.Bytes())
		if err != nil {
			t.Fatalf("newMessage() error = %v", err)
		}
		if m.hdr.Id != h.Id || m.hdr.NumQuestions != 1 {
			t.Errorf("newMessage() hdr = %+v", m.hdr)
		}
		if m.qn != msgTestQn {
			t.Errorf("newMessage() qn = %+v, want %+v", m.qn, msgTestQn)
		}
		if len(m.ans) != 0 {
			t.Errorf("newMessage() ans = %+v, want empty", m.ans)
		}
	})

	t.Run("response with one answer", func(t *testing.T) {
		h := headers{Id: 0xABCD, Flags: 0x8180, NumQuestions: 1, NumAnswers: 1}

		var raw bytes.Buffer
		binary.Write(&raw, binary.BigEndian, h)
		raw.Write(encodeName(msgTestQn.Name))
		raw.Write(be16(msgTestQn.QType))
		raw.Write(be16(msgTestQn.Class))
		raw.Write(buildAnswerBytes(aRec))

		m, err := newMessage(raw.Bytes())
		if err != nil {
			t.Fatalf("newMessage() error = %v", err)
		}
		if len(m.ans) != 1 || m.ans[0] != aRec {
			t.Errorf("newMessage() ans = %+v, want [%+v]", m.ans, aRec)
		}
		if m.hdr.NumAuthRRs != 0 || m.hdr.NumAddRRs != 0 {
			t.Errorf("newMessage() expected auth/addl RRs zeroed, got %+v", m.hdr)
		}
	})

	t.Run("truncated buffer", func(t *testing.T) {
		if _, err := newMessage([]byte{0x00, 0x01}); err == nil {
			t.Error("newMessage() with truncated buffer expected error, got nil")
		}
	})
}

func TestMakeFlags(t *testing.T) {
	tests := []struct {
		name     string
		flg      flags
		expected uint16
	}{
		{"all zero", flags{}, 0x0000},
		{"standard response", flags{QR: 1}, 1 << 15},
		{"nxdomain", flags{QR: 1, RCode: 3}, (1 << 15) | 3},
		{"all bits", flags{QR: 1, OpCode: 0xf, AA: 1, TC: 1, RD: 1, RA: 1, RCode: 0xf}, 0xFF8F},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := message{flg: tt.flg}
			m.makeFlags()

			if m.hdr.Flags != tt.expected {
				t.Errorf("makeFlags() = 0x%04x, want 0x%04x", m.hdr.Flags, tt.expected)
			}
		})
	}
}

func TestNewResponse(t *testing.T) {
	query := message{
		hdr: headers{Id: 0x1234, NumQuestions: 1},
		flg: flags{OpCode: 2, RD: 1},
		qn:  msgTestQn,
	}

	t.Run("authoritative answer found", func(t *testing.T) {
		resp := newResponse(query, []record{aRec}, true)

		if resp.hdr.Id != query.hdr.Id {
			t.Errorf("Id = 0x%04x, want 0x%04x", resp.hdr.Id, query.hdr.Id)
		}
		if resp.flg.OpCode != query.flg.OpCode || resp.flg.RD != query.flg.RD {
			t.Errorf("OpCode/RD not carried over from query: got %+v", resp.flg)
		}
		if resp.qn != query.qn {
			t.Errorf("qn = %+v, want %+v", resp.qn, query.qn)
		}
		if len(resp.ans) != 1 || resp.ans[0] != aRec {
			t.Errorf("ans = %+v, want [%+v]", resp.ans, aRec)
		}
		if resp.flg.QR != 1 || resp.flg.RA != 1 || resp.flg.AA != 1 || resp.flg.RCode != 0 {
			t.Errorf("flags = %+v, want QR=1 RA=1 AA=1 RCode=0", resp.flg)
		}
		if resp.hdr.NumAnswers != 1 {
			t.Errorf("NumAnswers = %d, want 1", resp.hdr.NumAnswers)
		}
	})

	t.Run("cached (non-authoritative) answer found", func(t *testing.T) {
		resp := newResponse(query, []record{aRec}, false)

		if resp.flg.AA != 0 {
			t.Errorf("AA = %d, want 0 for cached answer", resp.flg.AA)
		}
	})

	t.Run("no answer found (nxdomain)", func(t *testing.T) {
		resp := newResponse(query, []record{}, true)

		if resp.flg.RCode != 3 {
			t.Errorf("RCode = %d, want 3", resp.flg.RCode)
		}
		if resp.hdr.NumAnswers != 0 {
			t.Errorf("NumAnswers = %d, want 0", resp.hdr.NumAnswers)
		}
	})

	t.Run("auth and addl RRs always zeroed", func(t *testing.T) {
		resp := newResponse(query, []record{aRec}, true)

		if resp.hdr.NumAuthRRs != 0 || resp.hdr.NumAddRRs != 0 {
			t.Errorf("expected auth/addl RRs zeroed, got %+v", resp.hdr)
		}
	})
}

func TestFillName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []byte
	}{
		{"single label", "test", []byte{4, 't', 'e', 's', 't', 0}},
		{"multi label", "a.bc", []byte{1, 'a', 2, 'b', 'c', 0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := fillName(tt.input, &buf); err != nil {
				t.Fatalf("fillName() error = %v", err)
			}
			if !bytes.Equal(buf.Bytes(), tt.expected) {
				t.Errorf("fillName(%q) = %v, want %v", tt.input, buf.Bytes(), tt.expected)
			}
		})
	}
}

func TestFillIP(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		var buf bytes.Buffer
		if err := fillIP("1.2.3.4", &buf); err != nil {
			t.Fatalf("fillIP() error = %v", err)
		}
		if !bytes.Equal(buf.Bytes(), []byte{1, 2, 3, 4}) {
			t.Errorf("fillIP(1.2.3.4) = %v, want [1 2 3 4]", buf.Bytes())
		}
	})

	t.Run("non-numeric octet", func(t *testing.T) {
		var buf bytes.Buffer
		if err := fillIP("a.2.3.4", &buf); err == nil {
			t.Error("fillIP() with non-numeric octet expected error, got nil")
		}
	})

	t.Run("octet out of uint8 range", func(t *testing.T) {
		var buf bytes.Buffer
		if err := fillIP("256.2.3.4", &buf); err == nil {
			t.Error("fillIP() with out-of-range octet expected error, got nil")
		}
	})
}

func TestFillHeaders(t *testing.T) {
	h := headers{Id: 0x1234, Flags: 0x8180, NumQuestions: 1, NumAnswers: 2, NumAuthRRs: 0, NumAddRRs: 0}
	resp := message{hdr: h}

	var expected []byte
	expected = append(expected, be16(h.Id)...)
	expected = append(expected, be16(h.Flags)...)
	expected = append(expected, be16(h.NumQuestions)...)
	expected = append(expected, be16(h.NumAnswers)...)
	expected = append(expected, be16(h.NumAuthRRs)...)
	expected = append(expected, be16(h.NumAddRRs)...)

	var buf bytes.Buffer
	if err := resp.fillHeaders(&buf); err != nil {
		t.Fatalf("fillHeaders() error = %v", err)
	}
	if !bytes.Equal(buf.Bytes(), expected) {
		t.Errorf("fillHeaders() = %v, want %v", buf.Bytes(), expected)
	}
}

func TestFillQuestion(t *testing.T) {
	t.Run("one question", func(t *testing.T) {
		resp := message{hdr: headers{NumQuestions: 1}, qn: msgTestQn}

		var expected []byte
		expected = append(expected, encodeName(msgTestQn.Name)...)
		expected = append(expected, be16(msgTestQn.QType)...)
		expected = append(expected, be16(msgTestQn.Class)...)

		var buf bytes.Buffer
		if err := resp.fillQuestion(&buf); err != nil {
			t.Fatalf("fillQuestion() error = %v", err)
		}
		if !bytes.Equal(buf.Bytes(), expected) {
			t.Errorf("fillQuestion() = %v, want %v", buf.Bytes(), expected)
		}
	})

	t.Run("no question", func(t *testing.T) {
		resp := message{hdr: headers{NumQuestions: 0}}

		var buf bytes.Buffer
		if err := resp.fillQuestion(&buf); err != nil {
			t.Fatalf("fillQuestion() error = %v", err)
		}
		if buf.Len() != 0 {
			t.Errorf("fillQuestion() wrote %d bytes, want 0", buf.Len())
		}
	})
}

func TestFillAnswer(t *testing.T) {
	resp := message{ans: []record{aRec, cNameRec}}

	var buf bytes.Buffer
	if err := resp.fillAnswer(&buf); err != nil {
		t.Fatalf("fillAnswer() error = %v", err)
	}

	raw := buf.Bytes()
	bufr := bytes.NewReader(raw)

	var got message
	if err := got.parseAnswer(bufr, raw); err != nil {
		t.Fatalf("parseAnswer() error = %v", err)
	}
	if err := got.parseAnswer(bufr, raw); err != nil {
		t.Fatalf("parseAnswer() error = %v", err)
	}

	if len(got.ans) != 2 || got.ans[0] != aRec || got.ans[1] != cNameRec {
		t.Errorf("round trip ans = %+v, want [%+v %+v]", got.ans, aRec, cNameRec)
	}
}

// TestFillBuffer round-trips a full response through fillBuffer and back
// through newMessage to confirm encode/decode stay symmetric end-to-end.
func TestFillBuffer(t *testing.T) {
	resp := message{
		hdr: headers{Id: 0x9999, NumQuestions: 1, NumAnswers: 1},
		flg: flags{QR: 1, RA: 1, AA: 1, RD: 1, RCode: 0},
		qn:  msgTestQn,
		ans: []record{aRec},
	}
	resp.makeFlags()

	raw, err := resp.fillBuffer()
	if err != nil {
		t.Fatalf("fillBuffer() error = %v", err)
	}

	got, err := newMessage(raw)
	if err != nil {
		t.Fatalf("newMessage() on filled buffer error = %v", err)
	}

	if got.hdr.Id != resp.hdr.Id || got.hdr.Flags != resp.hdr.Flags {
		t.Errorf("round trip hdr = %+v, want %+v", got.hdr, resp.hdr)
	}
	if got.qn != resp.qn {
		t.Errorf("round trip qn = %+v, want %+v", got.qn, resp.qn)
	}
	if len(got.ans) != 1 || got.ans[0] != aRec {
		t.Errorf("round trip ans = %+v, want [%+v]", got.ans, aRec)
	}
}

// TestSend exercises send() over a real loopback UDP socket, confirming the
// bytes that arrive decode back into the original message.
func TestSend(t *testing.T) {
	serverConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to open server socket: %v", err)
	}
	defer serverConn.Close()

	clientConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to open client socket: %v", err)
	}
	defer clientConn.Close()

	resp := message{
		hdr: headers{Id: 0x4242, NumQuestions: 1, NumAnswers: 1},
		flg: flags{QR: 1, RA: 1, RCode: 0},
		qn:  msgTestQn,
		ans: []record{aRec},
	}
	resp.makeFlags()

	if err := resp.send(clientConn, serverConn.LocalAddr()); err != nil {
		t.Fatalf("send() error = %v", err)
	}

	if err := serverConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline() error = %v", err)
	}

	buf := make([]byte, 512)
	n, _, err := serverConn.ReadFrom(buf)
	if err != nil {
		t.Fatalf("ReadFrom() error = %v", err)
	}

	got, err := newMessage(buf[:n])
	if err != nil {
		t.Fatalf("newMessage() on received bytes error = %v", err)
	}

	if got.hdr.Id != resp.hdr.Id {
		t.Errorf("received Id = 0x%04x, want 0x%04x", got.hdr.Id, resp.hdr.Id)
	}
	if got.qn != resp.qn {
		t.Errorf("received qn = %+v, want %+v", got.qn, resp.qn)
	}
	if len(got.ans) != 1 || got.ans[0] != aRec {
		t.Errorf("received ans = %+v, want [%+v]", got.ans, aRec)
	}
}

func TestMessageString(t *testing.T) {
	t.Run("standard query", func(t *testing.T) {
		m := message{
			hdr: headers{Id: 0xABCD, NumQuestions: 1},
			flg: flags{QR: 0, OpCode: 0, RD: 1},
			qn:  question{Name: "example.com", QType: 1, Class: 1},
		}
		m.makeFlags()

		expected := fmt.Sprintf("ID:    0x%04x\n", m.hdr.Id)
		expected += fmt.Sprintf("Flags: 0x%04x\n", m.hdr.Flags)
		expected += "- Standard Query\n"
		expected += "- Recursion Requested\n"
		expected += fmt.Sprintf("# Questions: %v\n", m.hdr.NumQuestions)
		expected += fmt.Sprintf("# Answers:   %v\n", m.hdr.NumAnswers)
		expected += fmt.Sprintf("# Auth RRs:  %v\n", m.hdr.NumAuthRRs)
		expected += fmt.Sprintf("# Addl RRs:  %v\n", m.hdr.NumAddRRs)
		expected += "Questions:\n"
		expected += fmt.Sprintf("- %v, %v, %v\n", m.qn.Name, "A", "IN")

		if got := m.String(); got != expected {
			t.Errorf("String() = %q, want %q", got, expected)
		}
	})

	t.Run("standard response with answer", func(t *testing.T) {
		m := message{
			hdr: headers{Id: 0xABCD, NumQuestions: 1, NumAnswers: 1},
			flg: flags{QR: 1, RCode: 0, RD: 1, RA: 1, AA: 1},
			qn:  question{Name: "example.com", QType: 1, Class: 1},
			ans: []record{aRec},
		}
		m.makeFlags()

		expected := fmt.Sprintf("ID:    0x%04x\n", m.hdr.Id)
		expected += fmt.Sprintf("Flags: 0x%04x\n", m.hdr.Flags)
		expected += "- Standard Response\n"
		expected += "- Recursion Requested\n"
		expected += "- Recursion Available\n"
		expected += "- Authoritative Answer\n"
		expected += fmt.Sprintf("# Questions: %v\n", m.hdr.NumQuestions)
		expected += fmt.Sprintf("# Answers:   %v\n", m.hdr.NumAnswers)
		expected += fmt.Sprintf("# Auth RRs:  %v\n", m.hdr.NumAuthRRs)
		expected += fmt.Sprintf("# Addl RRs:  %v\n", m.hdr.NumAddRRs)
		expected += "Questions:\n"
		expected += fmt.Sprintf("- %v, %v, %v\n", m.qn.Name, "A", "IN")
		expected += "Answers:\n"
		expected += fmt.Sprintf("- %v\n", aRec)

		if got := m.String(); got != expected {
			t.Errorf("String() = %q, want %q", got, expected)
		}
	})

	t.Run("nxdomain response, no question echoed", func(t *testing.T) {
		m := message{
			hdr: headers{Id: 0xABCD},
			flg: flags{QR: 1, RCode: 3},
		}
		m.makeFlags()

		expected := fmt.Sprintf("ID:    0x%04x\n", m.hdr.Id)
		expected += fmt.Sprintf("Flags: 0x%04x\n", m.hdr.Flags)
		expected += "- Response NXDomain\n"
		expected += fmt.Sprintf("# Questions: %v\n", m.hdr.NumQuestions)
		expected += fmt.Sprintf("# Answers:   %v\n", m.hdr.NumAnswers)
		expected += fmt.Sprintf("# Auth RRs:  %v\n", m.hdr.NumAuthRRs)
		expected += fmt.Sprintf("# Addl RRs:  %v\n", m.hdr.NumAddRRs)

		if got := m.String(); got != expected {
			t.Errorf("String() = %q, want %q", got, expected)
		}
	})

	t.Run("unexpected qr/opcode", func(t *testing.T) {
		m := message{
			hdr: headers{Id: 0xABCD},
			flg: flags{QR: 0, OpCode: 1},
		}
		m.makeFlags()

		expected := fmt.Sprintf("ID:    0x%04x\n", m.hdr.Id)
		expected += fmt.Sprintf("Flags: 0x%04x\n", m.hdr.Flags)
		expected += "- Unexpected QR/opcode\n"
		expected += fmt.Sprintf("# Questions: %v\n", m.hdr.NumQuestions)
		expected += fmt.Sprintf("# Answers:   %v\n", m.hdr.NumAnswers)
		expected += fmt.Sprintf("# Auth RRs:  %v\n", m.hdr.NumAuthRRs)
		expected += fmt.Sprintf("# Addl RRs:  %v\n", m.hdr.NumAddRRs)

		if got := m.String(); got != expected {
			t.Errorf("String() = %q, want %q", got, expected)
		}
	})
}
