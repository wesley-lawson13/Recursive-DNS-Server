package main

import (
	"log"
	"net"
	"os"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("usage: %v zone_file\n", os.Args[0])
	}
	zone, zerr := newZone(os.Args[1])
	if zerr != nil {
		log.Fatalln(zerr)
	}

	var nserr error
	NEXTSERVER, nserr = net.ResolveUDPAddr("udp", "127.0.0.53:53")
	if nserr != nil {
		log.Fatalln("error resolving next server address:", nserr)
	}

	pending := &pendingQueryMap{
		items: make(map[uint16]queryInfo),
	}

	localCache := &cache{
		items: make(map[cacheKey][]record),
	}

	socket, serr := net.ListenPacket("udp", "localhost:53")
	if serr != nil {
		log.Fatalln(serr)
	}
	defer socket.Close()

	log.Println("Listening on UDP port 53...")

	const MAX_DNS_LEN = 512 // RFC 1035 2.3.4: maximum UDP DNS message size

	for {
		var buf [MAX_DNS_LEN]byte

		n, client, err := socket.ReadFrom(buf[:])
		if err != nil {
			log.Println(err)
			continue
		}

		go handleMessage(zone, socket, localCache, pending, client, buf[:n])
	}
}
