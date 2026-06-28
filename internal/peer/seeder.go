package peer

import (
	"net"
	"sync/atomic"
	"torrent-demo/internal/message"
)

type Seeder struct {
	conn         *net.TCPConn
	amChoking    bool
	isInterested bool

	localBitfield Bitfield

	// uploadedBytes accumulates sent block bytes (atomic); the parent connection
	// reads it via bytesUploaded to derive upload speed.
	uploadedBytes int64

	scheduler PieceScheduler
	reporter  Reporter
}

func (s *Seeder) init() {
	// For uploading
	if s.localBitfield = s.scheduler.GetLocalBitfield(); s.localBitfield.Any() {
		sendBitfield(s.conn, s.localBitfield)
	}
	if err := sendUnchoke(s.conn); err == nil {
		s.amChoking = false
	}
}

func (l *Seeder) processMessage(msg message.Message) {
	switch m := msg.(type) {
	case *message.KeepAlive:
		// liveness tracked above, nothing else to do
	case *message.Interested:
		l.isInterested = true
		if err := sendUnchoke(l.conn); err == nil {
			l.amChoking = false
		}
	case *message.NotInterested:
		l.isInterested = false
	case *message.Request:
		if !l.amChoking {
			requestedBlock, err := l.scheduler.GetBlock(int(m.Index), int(m.Begin), int(m.Length))
			if err == nil && len(requestedBlock) == int(m.Length) {
				if sendErr := sendPiece(l.conn, m.Index, m.Begin, requestedBlock); sendErr == nil {
					atomic.AddInt64(&l.uploadedBytes, int64(len(requestedBlock)))
					if l.reporter != nil { // debug-only
						l.reporter.BlockSent(l.conn.RemoteAddr().String(), len(requestedBlock))
					}
				}
			}
		}
	}
}

func (l *Seeder) advance() {
	localBitfield := l.scheduler.GetLocalBitfield()
	for idx := range localBitfield.Size() {
		if localBitfield.Has(idx) && !l.localBitfield.Has(idx) {
			sendHave(l.conn, uint32(idx))
		}
	}
	l.localBitfield = localBitfield
}

// bytesUploaded returns the cumulative sent block bytes. Safe to call from
// another goroutine.
func (l *Seeder) bytesUploaded() int64 {
	return atomic.LoadInt64(&l.uploadedBytes)
}

func (l *Seeder) shutdown() {
}
