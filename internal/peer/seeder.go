package peer

import (
	"log"
	"net"
	"sync/atomic"
	"torrent-demo/internal/message"
)

type Seeder struct {
	conn *net.TCPConn

	amChoking    bool
	isInterested bool

	localBitfield Bitfield

	// uploadedBytes accumulates sent block bytes (atomic); the parent connection
	// reads it via bytesUploaded to derive upload speed.
	uploadedBytes uint64

	scheduler PieceScheduler
}

func (s *Seeder) logf(format string, args ...any) {
	remote := "unknown"
	if s.conn != nil && s.conn.RemoteAddr() != nil {
		remote = s.conn.RemoteAddr().String()
	}
	log.Printf("[Seeder %s] "+format, append([]any{remote}, args...)...)
}

func (s *Seeder) init() {
	// For uploading
	if s.localBitfield = s.scheduler.GetLocalBitfield(); s.localBitfield.Any() {
		if err := sendBitfield(s.conn, s.localBitfield); err != nil {
			s.logf("stage=bitfield-send-failed pieces=%d err=%v", s.localBitfield.Size(), err)
		} else {
			s.logf("stage=bitfield-sent pieces=%d", s.localBitfield.Size())
		}
	} else {
		s.logf("stage=bitfield-skipped reason=empty")
	}
	if err := sendUnchoke(s.conn); err != nil {
		s.logf("stage=unchoke-send-failed err=%v", err)
	} else {
		s.amChoking = false
		s.logf("stage=unchoke-sent")
	}
}

func (l *Seeder) processMessage(msg message.Message) {
	switch m := msg.(type) {
	case *message.KeepAlive:
		// liveness tracked above, nothing else to do
	case *message.Interested:
		l.isInterested = true
		l.logf("stage=interested-received")
		if err := sendUnchoke(l.conn); err != nil {
			l.logf("stage=unchoke-send-failed err=%v", err)
		} else {
			l.amChoking = false
			l.logf("stage=unchoke-sent")
		}
	case *message.NotInterested:
		l.isInterested = false
		l.logf("stage=not-interested-received")
	case *message.Request:
		l.logf("stage=request-received piece=%d begin=%d length=%d", m.Index, m.Begin, m.Length)
		if l.amChoking {
			l.logf("stage=request-rejected piece=%d begin=%d length=%d reason=choked", m.Index, m.Begin, m.Length)
			return
		}
		requestedBlock, err := l.scheduler.GetBlock(int(m.Index), int(m.Begin), int(m.Length))
		if err != nil {
			l.logf("stage=request-rejected piece=%d begin=%d length=%d reason=block-unavailable err=%v", m.Index, m.Begin, m.Length, err)
			return
		}
		if len(requestedBlock) != int(m.Length) {
			l.logf("stage=request-rejected piece=%d begin=%d length=%d reason=length-mismatch actual=%d", m.Index, m.Begin, m.Length, len(requestedBlock))
			return
		}
		if err := sendPiece(l.conn, m.Index, m.Begin, requestedBlock); err != nil {
			l.logf("stage=piece-send-failed piece=%d begin=%d length=%d err=%v", m.Index, m.Begin, m.Length, err)
			return
		}
		atomic.AddUint64(&l.uploadedBytes, uint64(len(requestedBlock)))
		l.logf("stage=piece-sent piece=%d begin=%d length=%d", m.Index, m.Begin, len(requestedBlock))
	}
}

func (l *Seeder) advance() {
	localBitfield := l.scheduler.GetLocalBitfield()
	for idx := range localBitfield.Size() {
		if localBitfield.Has(idx) && !l.localBitfield.Has(idx) {
			if err := sendHave(l.conn, uint32(idx)); err != nil {
				l.logf("stage=have-send-failed piece=%d err=%v", idx, err)
			}
		}
	}
	l.localBitfield = localBitfield
}

func (l *Seeder) shutdown() {
}
