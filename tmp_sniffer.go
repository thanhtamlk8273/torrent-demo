package main

import (
	"fmt"
	"net"
	"time"

	"torrent-demo/internal/metainfo"
	"torrent-demo/internal/message"
)

func main() {
	meta, err := metainfo.NewMetaInfo("sn.torrent")
	if err != nil {
		fmt.Println("Error parsing metainfo:", err)
		return
	}

	ln, err := net.Listen("tcp", ":6881")
	if err != nil {
		fmt.Println("Error listening on :6881 -", err)
		return
	}
	fmt.Println("Sniffer listening on port 6881...")

	for {
		conn, err := ln.Accept()
		if err != nil {
			continue
		}
		fmt.Printf("Incoming connection from %s\n", conn.RemoteAddr())
		go handleConn(conn, meta.InfoHash)
	}
}

func handleConn(conn net.Conn, infoHash [20]byte) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	
	buf := make([]byte, 68)
	if _, err := conn.Read(buf); err != nil {
		fmt.Println("Error reading handshake:", err)
		return
	}
	
	var myPeerID [20]byte
	copy(myPeerID[:], "-SN0001-123456789012")
	
	handshake := make([]byte, 68)
	handshake[0] = 19
	copy(handshake[1:20], "BitTorrent protocol")
	copy(handshake[28:48], infoHash[:])
	copy(handshake[48:68], myPeerID[:])
	
	if _, err := conn.Write(handshake); err != nil {
		fmt.Println("Error writing handshake:", err)
		return
	}
	
	// Send Unchoke
	conn.Write([]byte{0, 0, 0, 1, 1})
	
	for {
		conn.SetDeadline(time.Now().Add(60 * time.Second))
		msg, err := message.Read(conn)
		if err != nil {
			return
		}
		if msg == nil {
			continue
		}
		
		switch m := msg.(type) {
		case *message.Request:
			fmt.Printf("[DETECTED] Remote %s sent a request for piece! Piece=%d Begin=%d Length=%d\n", conn.RemoteAddr(), m.Index, m.Begin, m.Length)
		default:
			// ignore
		}
	}
}
