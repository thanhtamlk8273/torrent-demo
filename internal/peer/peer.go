package peer

import (
	"context"
	"crypto/sha1"
	"fmt"
	"net"
	"time"
	"torrent-demo/internal/message"
)

type PieceWork struct {
	Index int
	Size  int
	Hash  [20]byte
}

type PieceScheduler interface {
	GetWork(bitfield Bitfield) (PieceWork, bool)
	CompletePiece(index int, data []byte) error
	UnclaimPiece(index int)
}

type PeerConnection struct {
	conn   *net.TCPConn
	sendCh chan message.Message

	lastAliveTime time.Time
	amChoking     bool
	amInterested  bool
	isChoking     bool
	isInterested  bool

	infoHash [20]byte
	peerId   [20]byte

	bitField     Bitfield
	currentPiece *Piece

	scheduler PieceScheduler
}

func (p *PeerConnection) connect() error {
	if err := p.sendHandshake(); err != nil {
		return err
	}

	if err := p.receiveHandshake(); err != nil {
		return err
	}
	return nil
}

func (p *PeerConnection) Run(ctx context.Context) error {
	// Setup deadline
	if deadline, ok := ctx.Deadline(); ok {
		if err := p.conn.SetDeadline(deadline); err != nil {
			return err
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if err := p.connect(); err != nil {
		return err
	}

	go p.sendLoop(ctx, cancel)
	go p.keepAliveLoop(ctx)

	p.sendInterested()

	for {
		// Redundant but put here for early exit
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if p.currentPiece == nil {
			if w, ok := p.scheduler.GetWork(p.bitField); !ok {
				time.Sleep(5 * time.Second)
			} else {
				p.currentPiece = newPiece(w)
			}
		}

		msg, err := p.read()
		if err != nil {
			return err
		}

		p.lastAliveTime = time.Now()

		switch m := msg.(type) {
		case *message.KeepAlive:
			// liveness tracked above, nothing else to do
		case *message.Interested:
			p.isInterested = true
			//p.sendUnchoke()
		case *message.NotInterested:
			p.isInterested = false
			p.sendChoke()
		case *message.Choke:
			p.isChoking = true
		case *message.UnChoke:
			p.isChoking = false
		case *message.Bitfield:
			p.bitField = m.Field
		case *message.Have:
			p.bitField.Set(int(m.PieceIndex))
		case *message.Piece:
			if m.Index == uint32(p.currentPiece.index) {
				p.currentPiece.saveBlock(m.Begin, m.Block)
			}
			if p.currentPiece.complete() {
				data := p.currentPiece.assemble()
				hash := sha1.Sum(data)
				if hash != p.currentPiece.expectedHash {
					p.scheduler.UnclaimPiece(p.currentPiece.index)
				} else {
					p.scheduler.CompletePiece(p.currentPiece.index, data)
				}
				p.currentPiece = nil
			}
		}
	}
}

func (p *PeerConnection) sendLoop(ctx context.Context, cancel context.CancelFunc) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-p.sendCh:
			if _, err := msg.WriteTo(p.conn); err != nil {
				cancel()
				return
			}
		}
	}
}

func (p *PeerConnection) send(msg message.Message) {
	select {
	case p.sendCh <- msg:
	default:
	}
}

func (p *PeerConnection) read() (message.Message, error) {
	return message.Read(p.conn)
}

func (p *PeerConnection) keepAliveLoop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.sendKeepAlive()
		}
	}
}

func (p *PeerConnection) sendHandshake() error {
	handShake := message.NewHandshake(p.infoHash, p.peerId)
	_, err := handShake.WriteTo(p.conn)
	return err
}

func (p *PeerConnection) receiveHandshake() error {
	var handShake message.Handshake
	if _, err := handShake.ReadFrom(p.conn); err != nil {
		return err
	}
	if handShake.InfoHash != p.infoHash {
		return fmt.Errorf("info_hash mismatch")
	}
	return nil
}

func (p *PeerConnection) sendKeepAlive() error {
	keepAlive := message.NewKeepAlive()
	_, err := keepAlive.WriteTo(p.conn)
	return err
}

func (p *PeerConnection) sendInterested() error {
	interested := message.NewInterested()
	_, err := interested.WriteTo(p.conn)
	return err
}

func (p *PeerConnection) sendNotInterested() error {
	notInterested := message.NewNotInterested()
	_, err := notInterested.WriteTo(p.conn)
	return err
}

func (p *PeerConnection) sendChoke() error {
	choke := message.NewChoke()
	_, err := choke.WriteTo(p.conn)
	return err
}
