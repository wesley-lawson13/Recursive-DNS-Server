# DNS Server

A recursive DNS forwarder implemented in Go from scratch, communicating over raw UDP sockets and parsing the DNS wire format (RFC 1035) manually — no DNS libraries used. Initially developed during my Boston College Computer Networks course, and extended to fix transaction ID collision, add safety measures for concurrent connection, and clean up files from the original submission.

## How It Works

The server handles three resolution paths in priority order:

1. **Authoritative zone** — answers from a local zone file are returned immediately with the `AA` (Authoritative Answer) flag set.
2. **TTL-aware cache** — responses from upstream are stored and served from memory until their TTL expires, with the `AA` flag cleared to signal non-authoritative origin.
3. **Recursive forwarding** — queries not resolved locally are forwarded to an upstream resolver; when the reply arrives, it is cached and routed back to the original client by matching the DNS transaction ID.

## Implementation Details

- **Binary protocol parsing** — DNS messages are parsed field-by-field using `encoding/binary` with big-endian byte order. Name label compression (RFC 1035 §4.1.4) is handled via pointer offsets into the raw packet buffer.
- **UDP socket I/O** — uses `net.PacketConn` directly; each incoming packet is dispatched to a goroutine.
- **Concurrent-safe cache** — the response cache uses `sync.RWMutex` to allow parallel reads while serializing writes.
- **Concurrent-safe pending query table** — in-flight forwarded queries are tracked in a `sync.Mutex`-guarded map keyed by DNS transaction ID.
- **TTL expiration** — `cache.update()` is called on each incoming request and evicts or adjusts records whose TTL has elapsed since they were cached.
- **Record types** — A (IPv4) and CNAME records are supported; the wire encoding for each is handled separately since A records serialize as 4 raw octets and CNAME records serialize as DNS name labels.

## Zone File Format

Each line defines one resource record:

```
<name>  <ttl>  <class>  <type>  <data>
```

Example (`csci3363.zone`):

```
test1.csci3363.net  300 IN  A       1.2.3.4
test1.csci3363.net  300 IN  A       1.2.3.5
test2.csci3363.net  300 IN  CNAME   test1.csci3363.net
```

## Usage

```
go build -o dns-server .
sudo ./dns-server <zone_file>
```

Requires root (or `CAP_NET_BIND_SERVICE`) to bind port 53. The upstream resolver defaults to `127.0.0.53:53`.

In another terminal window, test with `dig`:

```
dig @localhost test1.csci3363.net A
dig @localhost google.com A
```

## Testing

The suite covers **86.5%** of statements (`go test ./... -cover`), across 50 tests spanning every file: unit tests for message parsing/serialization (`message_test.go`), record encoding (`record_test.go`), zone file loading (`zone_test.go`), the TTL cache (`cache_test.go`), and the pending-query table (`pending_test.go`, including a concurrent store/pop test run with `-race`), plus the end-to-end integration tests described below.

### End-to-end integration tests

`handle_message_test.go` includes two full-stack tests that exercise `handleMessage` exactly as `main.go` would call it: real loopback UDP sockets stand in for the client and upstream resolver, a real wire-format buffer is built and parsed (no mocked message objects), and the test asserts on bytes actually received off the socket.

- **`TestHandleMessage_QueryFlow`** — a client socket sends a wire-format query for a zone-authoritative name to the server's `handleMessage`. Covers: buffer parsing, zone lookup, response encoding, and delivery back to the querying `net.Addr` — asserting the reply carries the original transaction ID, the `AA` flag, and the expected answer count.
- **`TestHandleMessage_ReplyFlow`** — a pending query is seeded (as if already forwarded upstream), then an upstream reply arrives at `handleMessage` over a loopback socket. Covers: transaction-ID lookup in the pending table, ID rewrite back to the original client's ID, delivery to the *original client's* socket (not the upstream one), and that the answer is written into the cache as a side effect.

Together these two catch regressions that unit tests on individual functions can't: wire-format round-tripping through real sockets, correct routing of bytes to the correct `net.Addr`, and the interaction between `handleMessage`, the zone, the cache, and the pending-query table.

The remaining `TestHandleMessage_*` and `TestHandleQuery_*` / `TestHandleReply_*` tests in the same file are narrower unit/socket tests (malformed input, unknown reply IDs, cache eviction on request) that isolate individual branches of `handleMessage`, `handleQuery`, and `handleReply` without exercising the full send/receive path.

### Running the tests

```
go test ./...              # run the full suite
go test ./... -v           # verbose, per-test output
go test ./... -cover       # with coverage summary
go test ./... -race        # with the race detector (covers concurrent cache/pending access)
```

To see coverage by function:

```
go test ./... -coverprofile=cover.out
go tool cover -func=cover.out
```
