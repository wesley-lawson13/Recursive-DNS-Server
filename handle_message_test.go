package main

import (
	"net"
	"testing"
	"time"
)

// --- HELPER ---

func openLoopbackConn(t *testing.T) net.PacketConn {
	t.Helper()

	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to open loopback socket: %v", err)
	}
	return conn
}

// --- HELPER ---

func buildQueryBuf(t *testing.T, id uint16, qn question) []byte {
	t.Helper()

	m := message{
		hdr: headers{Id: id, NumQuestions: 1},
		flg: flags{QR: 0, RD: 1},
		qn:  qn,
	}
	m.makeFlags()

	buf, err := m.fillBuffer()
	if err != nil {
		t.Fatalf("failed to build query buffer: %v", err)
	}
	return buf
}

// --- HELPER ---

func buildReplyBuf(t *testing.T, id uint16, qn question, ans []record) []byte {
	t.Helper()

	m := message{
		hdr: headers{Id: id, NumQuestions: 1, NumAnswers: uint16(len(ans))},
		flg: flags{QR: 1, RA: 1, RCode: 0},
		qn:  qn,
		ans: ans,
	}
	m.makeFlags()

	buf, err := m.fillBuffer()
	if err != nil {
		t.Fatalf("failed to build reply buffer: %v", err)
	}
	return buf
}

func TestHandleQuery_ZoneHit(t *testing.T) {
	zn := createZone(t)
	ch := initCache(t)
	pending := initPendingMap(t)
	var client net.Addr = &genericClient

	query := message{hdr: headers{Id: 0x1111, NumQuestions: 1}, flg: flags{RD: 1}, qn: testQn}

	resp, addr := handleQuery(zn, client, query, ch, pending)

	if addr != client {
		t.Errorf("addr = %v, want %v", addr, client)
	}
	if resp.flg.AA != 1 {
		t.Errorf("flg.AA = %v, want authoritative answer", resp.flg.AA)
	}
	if len(resp.ans) != 2 {
		t.Errorf("ans len = %d, want 2", len(resp.ans))
	}
	if len(pending.items) != 0 {
		t.Errorf("pending.items = %d, want 0 for a zone hit", len(pending.items))
	}
}

func TestHandleQuery_CacheHit(t *testing.T) {
	zn := createZone(t)
	ch := initCache(t)
	pending := initPendingMap(t)
	var client net.Addr = &genericClient

	// seed the cache for a name not present in the zone
	ch.addRecords(message{ans: []record{aRec}}, message{qn: missingQn})

	query := message{hdr: headers{Id: 0x2222, NumQuestions: 1}, flg: flags{RD: 1}, qn: missingQn}

	resp, addr := handleQuery(zn, client, query, ch, pending)

	if addr != client {
		t.Errorf("addr = %v, want %v", addr, client)
	}
	if resp.flg.AA != 0 {
		t.Errorf("flg.AA = %v, want non-authoritative (cached) answer", resp.flg.AA)
	}
	if len(resp.ans) != 1 || resp.ans[0].Data != aRec.Data {
		t.Errorf("ans = %+v, want [%+v]", resp.ans, aRec)
	}
	if len(pending.items) != 0 {
		t.Errorf("pending.items = %d, want 0 for a cache hit", len(pending.items))
	}
}

func TestHandleQuery_ForwardsOnMiss(t *testing.T) {
	zn := createZone(t)
	ch := initCache(t)
	pending := initPendingMap(t)
	var client net.Addr = &genericClient

	nextServer := &net.UDPAddr{IP: net.ParseIP("8.8.8.8"), Port: 53}
	origNext := NEXTSERVER
	NEXTSERVER = nextServer
	defer func() { NEXTSERVER = origNext }()

	query := message{hdr: headers{Id: 0x3333, NumQuestions: 1}, flg: flags{RD: 1}, qn: missingQn}

	fwd, addr := handleQuery(zn, client, query, ch, pending)

	if addr != net.Addr(nextServer) {
		t.Errorf("addr = %v, want NEXTSERVER %v", addr, nextServer)
	}
	if fwd.hdr.Id == query.hdr.Id {
		t.Errorf("forwarded query Id = 0x%04x, want it rewritten away from the original", fwd.hdr.Id)
	}

	info, ok := pending.pop(fwd.hdr.Id)
	if !ok {
		t.Fatalf("pending map missing entry for forwarded id 0x%04x", fwd.hdr.Id)
	}
	if info.query.hdr.Id != query.hdr.Id || info.query.qn != query.qn {
		t.Errorf("stored pending query = %+v, want original query %+v", info.query, query)
	}
	if info.client != client {
		t.Errorf("stored pending client = %v, want %v", info.client, client)
	}
}

func TestHandleReply_KnownID(t *testing.T) {
	ch := initCache(t)
	pending := initPendingMap(t)
	var origClient net.Addr = &genericClient
	var server net.Addr = &net.UDPAddr{IP: net.ParseIP("9.9.9.9"), Port: 53}

	origQuery := message{hdr: headers{Id: 0xABCD, NumQuestions: 1}, qn: testQn}
	id := pending.store(queryInfo{query: origQuery, client: origClient})

	reply := message{
		hdr: headers{Id: id, NumQuestions: 1, NumAnswers: 1},
		flg: flags{QR: 1, RCode: 0},
		qn:  testQn,
		ans: []record{aRec},
	}

	got, addr := handleReply(server, reply, ch, pending)

	if addr != origClient {
		t.Errorf("addr = %v, want original client %v", addr, origClient)
	}
	if got.hdr.Id != origQuery.hdr.Id {
		t.Errorf("reply Id = 0x%04x, want restored original Id 0x%04x", got.hdr.Id, origQuery.hdr.Id)
	}

	cached, ok := ch.returnRecords(testQn)
	if !ok || len(cached) != 1 || cached[0].Data != aRec.Data {
		t.Errorf("cache after reply = %v, %v, want a hit with [%+v]", cached, ok, aRec)
	}

	if _, stillPending := pending.pop(id); stillPending {
		t.Errorf("pending entry for id 0x%04x should have been consumed", id)
	}
}

func TestHandleReply_UnknownID(t *testing.T) {
	ch := initCache(t)
	pending := initPendingMap(t)
	var server net.Addr = &genericClient

	reply := message{hdr: headers{Id: 0x9999, NumQuestions: 1}, flg: flags{QR: 1}, qn: testQn}

	got, addr := handleReply(server, reply, ch, pending)

	if addr != nil {
		t.Errorf("addr = %v, want nil for an unknown reply ID", addr)
	}
	if got.hdr.Id != reply.hdr.Id {
		t.Errorf("reply Id = 0x%04x, want unchanged 0x%04x", got.hdr.Id, reply.hdr.Id)
	}
	if _, ok := ch.returnRecords(testQn); ok {
		t.Errorf("cache should remain empty when the reply ID is unknown")
	}
}

func TestHandleMessage_MalformedBuffer(t *testing.T) {
	zn := createZone(t)
	ch := initCache(t)
	pending := initPendingMap(t)

	socket := openLoopbackConn(t)
	defer socket.Close()
	clientConn := openLoopbackConn(t)
	defer clientConn.Close()

	handleMessage(zn, socket, ch, pending, clientConn.LocalAddr(), []byte{0x00, 0x01})

	if err := clientConn.SetReadDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
		t.Fatalf("SetReadDeadline() error = %v", err)
	}
	buf := make([]byte, 512)
	if _, _, err := clientConn.ReadFrom(buf); err == nil {
		t.Error("expected no response for a malformed message, got one")
	}
}

func TestHandleMessage_ReplyUnknownID(t *testing.T) {
	ch := initCache(t)
	pending := initPendingMap(t)

	socket := openLoopbackConn(t)
	defer socket.Close()

	buf := buildReplyBuf(t, 0x4242, testQn, []record{aRec})

	// nothing is stored under this id, so handleMessage's addr == nil branch
	// should skip sending anything and leave the cache untouched
	handleMessage(zone{}, socket, ch, pending, socket.LocalAddr(), buf)

	if _, ok := ch.returnRecords(testQn); ok {
		t.Error("cache should remain empty for a reply with an unknown ID")
	}
}

func TestHandleMessage_EvictsExpiredCacheEntries(t *testing.T) {
	zn := createZone(t)
	ch := initCache(t)
	pending := initPendingMap(t)

	staleKey := makeKey(mailQn)
	ch.items[staleKey] = []record{{Data: "stale", TTL: 10, AddedAt: time.Now().Add(-20 * time.Second)}}

	socket := openLoopbackConn(t)
	defer socket.Close()
	clientConn := openLoopbackConn(t)
	defer clientConn.Close()

	buf := buildQueryBuf(t, 0x5555, testQn)

	handleMessage(zn, socket, ch, pending, clientConn.LocalAddr(), buf)

	if _, ok := ch.items[staleKey]; ok {
		t.Error("expected the expired cache entry to be evicted during handleMessage")
	}
}

// --- Integration Tests ---

// end-to-end integration test for incoming queries that hit the zone.
func TestHandleMessage_QueryFlow(t *testing.T) {
	zn := createZone(t)
	ch := initCache(t)
	pending := initPendingMap(t)

	socket := openLoopbackConn(t)
	defer socket.Close()
	clientConn := openLoopbackConn(t)
	defer clientConn.Close()

	buf := buildQueryBuf(t, 0x1234, testQn)

	handleMessage(zn, socket, ch, pending, clientConn.LocalAddr(), buf)

	if err := clientConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline() error = %v", err)
	}
	respBuf := make([]byte, 512)
	n, _, err := clientConn.ReadFrom(respBuf)
	if err != nil {
		t.Fatalf("expected a response, got error: %v", err)
	}

	resp, err := newMessage(respBuf[:n])
	if err != nil {
		t.Fatalf("newMessage() on received bytes error = %v", err)
	}
	if resp.hdr.Id != 0x1234 {
		t.Errorf("resp Id = 0x%04x, want 0x1234", resp.hdr.Id)
	}
	if resp.flg.AA != 1 {
		t.Errorf("resp flg.AA = %v, want authoritative answer", resp.flg.AA)
	}
	if len(resp.ans) != 2 {
		t.Errorf("resp ans len = %d, want 2", len(resp.ans))
	}
}

// end-to-end integration test for replies, ensuring that it reaches the original 
// client and gets cached.
func TestHandleMessage_ReplyFlow(t *testing.T) {
	ch := initCache(t)
	pending := initPendingMap(t)

	socket := openLoopbackConn(t)
	defer socket.Close()
	origClientConn := openLoopbackConn(t)
	defer origClientConn.Close()
	upstream := openLoopbackConn(t)
	defer upstream.Close()

	origQuery := message{hdr: headers{Id: 0xABCD, NumQuestions: 1}, qn: testQn}
	id := pending.store(queryInfo{query: origQuery, client: origClientConn.LocalAddr()})

	buf := buildReplyBuf(t, id, testQn, []record{aRec})

	handleMessage(zone{}, socket, ch, pending, upstream.LocalAddr(), buf)

	if err := origClientConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline() error = %v", err)
	}
	respBuf := make([]byte, 512)
	n, _, err := origClientConn.ReadFrom(respBuf)
	if err != nil {
		t.Fatalf("expected the reply to be forwarded to the original client, got error: %v", err)
	}

	resp, err := newMessage(respBuf[:n])
	if err != nil {
		t.Fatalf("newMessage() on received bytes error = %v", err)
	}
	if resp.hdr.Id != origQuery.hdr.Id {
		t.Errorf("resp Id = 0x%04x, want restored original Id 0x%04x", resp.hdr.Id, origQuery.hdr.Id)
	}
	if len(resp.ans) != 1 || resp.ans[0].Data != aRec.Data {
		t.Errorf("resp ans = %+v, want [%+v]", resp.ans, aRec)
	}

	if cached, ok := ch.returnRecords(testQn); !ok || len(cached) != 1 {
		t.Errorf("expected the reply to populate the cache, got %v, %v", cached, ok)
	}
}
