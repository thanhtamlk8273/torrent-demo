package peer

import (
	"net"
	"torrent-demo/internal/message"
)

type Seeder struct {
	conn         *net.TCPConn
	amChoking    bool
	isInterested bool

	localBitfield Bitfield

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
				if sendErr := sendPiece(l.conn, m.Index, m.Begin, requestedBlock); sendErr == nil && l.reporter != nil {
					l.reporter.BlockSent(l.conn.RemoteAddr().String(), len(requestedBlock)) // debug-only
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

func (l *Seeder) shutdown() {
}
