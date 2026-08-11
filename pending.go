package main

import (
	"net"
	"sync"
)

type queryInfo struct {
	query  message
	client net.Addr
}

type pendingQueryMap struct {
	mu     sync.Mutex
	items  map[uint16]queryInfo
	nextID uint16
}

// store assigns a unique forwarded ID to the query and returns it.
// The caller must rewrite query.hdr.Id with the returned ID before sending upstream.
func (pq *pendingQueryMap) store(info queryInfo) uint16 {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	id := pq.nextID
	pq.nextID++
	pq.items[id] = info
	return id
}

func (pq *pendingQueryMap) pop(id uint16) (queryInfo, bool) {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	info, ok := pq.items[id]
	if ok {
		delete(pq.items, id)
	}
	return info, ok
}
