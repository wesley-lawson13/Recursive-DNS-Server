package main

import (
	"log"
	"net"
)

var NEXTSERVER net.Addr

func handleQuery(zn zone, client net.Addr, query message, localCache *cache, pending *pendingQueryMap) (message, net.Addr) {
	var resp message

	log.Printf("%v: query received\n%v\n", client, query)

	recs := zn.lookup(query.qn)

	if len(recs) != 0 {
		resp = newResponse(query, recs, true)
		log.Printf("%v: sending reply\n%v\n", client, resp)
		return resp, client
	}

	cacheRecs, found := localCache.returnRecords(query.qn)
	if found {
		resp = newResponse(query, cacheRecs, false)
		log.Printf("%v: sending cached reply\n%v\n", client, resp)
		return resp, client
	}

	log.Printf("%v: forwarding query to %v\n%v\n", client, NEXTSERVER, query)
	forwardedID := pending.store(queryInfo{query, client})
	query.hdr.Id = forwardedID

	return query, NEXTSERVER
}

func handleReply(server net.Addr, reply message, localCache *cache, pending *pendingQueryMap) (message, net.Addr) {
	log.Printf("%v: reply received\n%v\n", server, reply)

	info, ok := pending.pop(reply.hdr.Id)
	if !ok {
		log.Printf("%v: received reply for unknown query ID 0x%04x\n", server, reply.hdr.Id)
		return reply, nil
	}

	// restore the original client transaction ID we replaced when forwarding
	reply.hdr.Id = info.query.hdr.Id
	localCache.addRecords(reply, info.query)

	log.Printf("%v: forwarding reply to %v\n%v\n", server, info.client, reply)

	return reply, info.client
}

func handleMessage(zn zone, socket net.PacketConn, localCache *cache, pending *pendingQueryMap, client net.Addr, buf []byte) {
	// lazy TTL eviction: expire stale cache entries on each inbound message
	// rather than maintaining a background goroutine
	localCache.update()

	msg, err := newMessage(buf)
	if err != nil {
		log.Printf("%v: %v\n", client, err)
		return
	}

	var resp message
	var addr net.Addr

	if msg.flg.QR == 0 {
		resp, addr = handleQuery(zn, client, msg, localCache, pending)
	} else {
		resp, addr = handleReply(client, msg, localCache, pending)
	}

	if addr != nil {
		if err := resp.send(socket, addr); err != nil {
			log.Printf("%v: %v\n", client, err)
			return
		}
	}
}
