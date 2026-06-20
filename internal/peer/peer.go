package peer

import (
	"context"
	"crypto/sha1"
	"fmt"
	"net"
	"strconv"
	"time"
	"torrent-demo/internal/message"
)

const (
	kMessageChannelSize int = 16
	kDialTimeout            = 5 * time.Second
	kHandshakeTimeout       = 10 * time.Second
)

type PieceWork struct {
	Index int
	Size  int
	Hash  [20]byte
}

type PieceScheduler interface {
	GetWork(bf Bitfield) (PieceWork, bool)
	CompletePiece(index int, data []byte) error
	UnclaimPiece(index int)
}

// Reporter receives debug-only lifecycle signals for one peer connection so
// an external observer (e.g. a debug dashboard) can track progress without
// peer.go depending on that observer's implementation. Safe to leave unset.
type Reporter interface {
	PeerConnected(addr string)
	PieceStarted(addr string, index, blockCount int)
	BlockSaved(addr string, blockIndex, size int)
	PieceCompleted(addr string, index int)
	PieceFailed(addr, reason string)
	PeerDisconnected(addr string, err error)
}

type State int

const (
	Free State = iota
	Downloading
)

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

	bitfield Bitfield

	currentPiece *Piece
	nextIdx      uint32
	state        State

	scheduler PieceScheduler

	reporter Reporter // debug-only: nil-safe, no-op unless SetReporter is called
}

// SetReporter attaches a debug-only observer for this connection's lifecycle
// signals. Not required for normal operation.
func (p *PeerConnection) SetReporter(r Reporter) {
	p.reporter = r
}

func NewPeerConnection(ip string, port int, infoHash, peerId [20]byte, scheduler PieceScheduler) (PeerConnection, error) {
	dialer := net.Dialer{Timeout: kDialTimeout}
	conn, err := dialer.Dial("tcp", net.JoinHostPort(ip, strconv.Itoa(port)))
	if err != nil {
		return PeerConnection{}, err
	}
	return PeerConnection{
		conn:          conn.(*net.TCPConn),
		sendCh:        make(chan message.Message, kMessageChannelSize),
		lastAliveTime: time.Now(),
		amChoking:     true,
		amInterested:  false,
		isChoking:     true,
		isInterested:  false,
		infoHash:      infoHash,
		peerId:        peerId,
		bitfield:      Bitfield{},
		currentPiece:  nil,
		nextIdx:       0,
		state:         Free,
		scheduler:     scheduler,
	}, nil
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

func (p *PeerConnection) Run(ctx context.Context) (retErr error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer func() {
		if p.currentPiece != nil {
			if p.reporter != nil { // debug-only
				p.reporter.PieceFailed(p.conn.RemoteAddr().String(), fmt.Sprintf("piece %d incomplete: %v", p.currentPiece.index, retErr))
			}
			p.scheduler.UnclaimPiece(p.currentPiece.index)
			p.currentPiece = nil
		}
	}()

	if err := p.conn.SetDeadline(time.Now().Add(kHandshakeTimeout)); err != nil {
		return err
	}
	if err := p.connect(); err != nil {
		return err
	}

	if deadline, ok := ctx.Deadline(); ok {
		if err := p.conn.SetDeadline(deadline); err != nil {
			return err
		}
	} else {
		if err := p.conn.SetDeadline(time.Time{}); err != nil {
			return err
		}
	}

	addr := p.conn.RemoteAddr().String()
	if p.reporter != nil { // debug-only
		p.reporter.PeerConnected(addr)
		defer func() { p.reporter.PeerDisconnected(addr, retErr) }()
	}

	fmt.Printf("[%s] handshake complete\n", p.conn.RemoteAddr())

	go p.sendLoop(ctx, cancel)
	go p.keepAliveLoop(ctx)
	go func() {
		// p.read() below blocks on the socket and doesn't observe ctx
		// cancellation directly; closing the conn unblocks it immediately.
		<-ctx.Done()
		p.conn.Close()
	}()

	p.amChoking = true
	p.amInterested = true
	p.sendInterested()

	for {
		// Redundant but put here for early exit
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if p.currentPiece == nil {
			if w, ok := p.scheduler.GetWork(p.bitfield); !ok {
				// time.Sleep(5 * time.Second)
			} else {
				fmt.Printf("[%s] downloading piece %d (%d bytes)\n", p.conn.RemoteAddr(), w.Index, w.Size)
				p.currentPiece = newPiece(w)
				if p.reporter != nil { // debug-only
					p.reporter.PieceStarted(p.conn.RemoteAddr().String(), w.Index, len(p.currentPiece.blocks))
				}
			}
		}

		if p.state == Free && !p.isChoking {
			p.requestNextChunk()
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
			p.bitfield = m.Field
			fmt.Printf("[%s] bitfield received (%d bytes)\n", p.conn.RemoteAddr(), len(m.Field))
		case *message.Have:
			p.bitfield.Set(int(m.PieceIndex))
		case *message.Piece:
			if p.currentPiece == nil {
				continue
			}
			if m.Index != uint32(p.currentPiece.index) {
				// stale block for a piece we've moved on from; dropped silently
			} else if p.currentPiece.saveBlock(m.Begin, m.Block) {
				if p.reporter != nil { // debug-only
					p.reporter.BlockSaved(p.conn.RemoteAddr().String(), int(m.Begin)/kBlockSize, len(m.Block))
				}
			}
			if p.currentPiece.complete() {
				data := p.currentPiece.assemble()
				hash := sha1.Sum(data)
				if hash != p.currentPiece.expectedHash {
					fmt.Printf("[%s] piece %d verification FAILED (hash mismatch), dropping\n", p.conn.RemoteAddr(), p.currentPiece.index)
					if p.reporter != nil { // debug-only
						p.reporter.PieceFailed(p.conn.RemoteAddr().String(), fmt.Sprintf("piece %d hash mismatch", p.currentPiece.index))
					}
					p.scheduler.UnclaimPiece(p.currentPiece.index)
				} else {
					fmt.Printf("[%s] piece %d verified OK, completed\n", p.conn.RemoteAddr(), p.currentPiece.index)
					p.scheduler.CompletePiece(p.currentPiece.index, data)
					if p.reporter != nil { // debug-only
						p.reporter.PieceCompleted(p.conn.RemoteAddr().String(), p.currentPiece.index)
					}
				}
				p.currentPiece = nil
				p.nextIdx = 0
			}
			p.state = Free
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

func (p *PeerConnection) requestNextChunk() bool {
	if p.currentPiece == nil || p.state == Downloading {
		return false
	}
	if p.nextIdx*kBlockSize >= uint32(p.currentPiece.pieceSize) {
		return false
	}
	chunkSize := uint32(kBlockSize)
	if (p.nextIdx+1)*kBlockSize > uint32(p.currentPiece.pieceSize) {
		chunkSize = uint32(p.currentPiece.pieceSize) - p.nextIdx*kBlockSize
	}
	p.sendRequest(uint32(p.currentPiece.index), p.nextIdx*chunkSize, chunkSize)
	p.state = Downloading
	p.nextIdx += 1
	return true
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

func (p *PeerConnection) sendRequest(index, begin, length uint32) error {
	req := message.NewRequest(index, begin, length)
	_, err := req.WriteTo(p.conn)
	return err
}
