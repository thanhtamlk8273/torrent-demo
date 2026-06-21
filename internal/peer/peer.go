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
	kMessageChannelSize int = 64
	kMaxParallelChunks      = 8
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

	// Maybe should be a seperate interface
	GetLocalBitfield() Bitfield
	GetBlock(index, begin, length int) ([]byte, error)
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
	BlockSent(addr string, size int)
}

type PeerConnection struct {
	conn *net.TCPConn

	lastAliveTime time.Time
	amChoking     bool
	amInterested  bool
	isChoking     bool
	isInterested  bool

	infoHash [20]byte
	peerId   [20]byte

	localBitfield Bitfield
	bitfield      Bitfield

	currentPiece     *Piece
	nextIdx          uint32
	nbParallelChunks int

	scheduler PieceScheduler
	reporter  Reporter // debug-only: nil-safe, no-op unless SetReporter is called
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
		conn:             conn.(*net.TCPConn),
		lastAliveTime:    time.Now(),
		amChoking:        true,
		amInterested:     false,
		isChoking:        true,
		isInterested:     false,
		infoHash:         infoHash,
		peerId:           peerId,
		bitfield:         Bitfield{},
		currentPiece:     nil,
		nextIdx:          0,
		nbParallelChunks: 0,
		scheduler:        scheduler,
	}, nil
}

func (p *PeerConnection) connect() error {
	if err := sendHandshake(p.conn, p.infoHash, p.peerId); err != nil {
		return err
	}
	if err := receiveHandshake(p.conn, p.infoHash); err != nil {
		return err
	}
	if p.localBitfield = p.scheduler.GetLocalBitfield(); p.localBitfield.Any() {
		sendBitfield(p.conn, p.localBitfield)
	}
	if err := sendUnchoke(p.conn); err == nil {
		p.amChoking = false
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

	go p.keepAliveLoop(ctx)

	p.amChoking = true
	p.amInterested = true
	sendInterested(p.conn)

	advanceTicker := time.NewTicker(250 * time.Millisecond)
	recvMsg := p.createReadChannel(ctx, cancel)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg := <-recvMsg:
			p.processMessage(msg)
			p.advance()
		case <-advanceTicker.C:
			p.advance()
		}
	}
}

func (p *PeerConnection) createReadChannel(ctx context.Context, cancel context.CancelFunc) <-chan message.Message {
	conn := p.conn
	recvMsgChan := make(chan message.Message)
	go func() {
		// p.read() below blocks on the socket and doesn't observe ctx
		// cancellation directly; closing the conn unblocks it immediately.
		<-ctx.Done()
		conn.Close()
	}()

	go func() {
		for {
			msg, err := message.Read(conn)
			if err != nil {
				cancel()
				return
			} else {
				select {
				case recvMsgChan <- msg:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return recvMsgChan
}

func (p *PeerConnection) processMessage(msg message.Message) {
	p.lastAliveTime = time.Now()
	switch m := msg.(type) {
	case *message.KeepAlive:
		// liveness tracked above, nothing else to do
	case *message.Interested:
		fmt.Printf("Received *message.Interested")
		p.isInterested = true
		if err := sendUnchoke(p.conn); err == nil {
			p.amChoking = false
		}
	case *message.NotInterested:
		fmt.Printf("Received *message.NotInterested")
		p.isInterested = false
		p.amChoking = true
	case *message.Choke:
		p.isChoking = true
	case *message.Unchoke:
		p.isChoking = false
	case *message.Bitfield:
		// The wire bytes can't tell us the piece count; it comes from the
		// torrent metadata, carried locally by localBitfield.
		p.bitfield = *NewBitfield(m.Field, p.localBitfield.Size())
	case *message.Have:
		p.bitfield.Set(int(m.PieceIndex))
	case *message.Request:
		fmt.Printf("Received *message.Request %d, %d, %d", m.Begin, m.Index, m.Length)
		if !p.amChoking {
			requestedBlock, err := p.scheduler.GetBlock(int(m.Index), int(m.Begin), int(m.Length))
			if err == nil && len(requestedBlock) == int(m.Length) {
				if sendErr := sendPiece(p.conn, m.Index, m.Begin, requestedBlock); sendErr == nil && p.reporter != nil {
					p.reporter.BlockSent(p.conn.RemoteAddr().String(), len(requestedBlock)) // debug-only
				}
			}
		}
	case *message.Piece:
		if p.currentPiece != nil {
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
					if p.reporter != nil { // debug-only
						p.reporter.PieceFailed(p.conn.RemoteAddr().String(), fmt.Sprintf("piece %d hash mismatch", p.currentPiece.index))
					}
					p.scheduler.UnclaimPiece(p.currentPiece.index)
				} else {
					p.scheduler.CompletePiece(p.currentPiece.index, data)
					if p.reporter != nil { // debug-only
						p.reporter.PieceCompleted(p.conn.RemoteAddr().String(), p.currentPiece.index)
					}
				}
				p.currentPiece = nil
				p.nextIdx = 0
			}
			if p.nbParallelChunks > 0 {
				p.nbParallelChunks -= 1
			}
		}
	}
}

func (p *PeerConnection) advance() {
	if p.currentPiece == nil {
		if w, ok := p.scheduler.GetWork(p.bitfield); ok {
			if p.currentPiece != nil {
				panic("p.currentPiece should be NULL when new work comes")
			}
			p.currentPiece = newPiece(w)
			if p.reporter != nil { // debug-only
				p.reporter.PieceStarted(p.conn.RemoteAddr().String(), w.Index, len(p.currentPiece.blocks))
			}
		}

		localBitfield := p.scheduler.GetLocalBitfield()
		for idx := range localBitfield.Size() {
			if localBitfield.Has(idx) && !p.localBitfield.Has(idx) {
				sendHave(p.conn, uint32(idx))
			}
		}
		p.localBitfield = localBitfield
	}
	if !p.isChoking {
		for p.nbParallelChunks < kMaxParallelChunks {
			if ok := p.requestNextChunk(); !ok {
				break
			}
		}
	}
}

func (p *PeerConnection) keepAliveLoop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sendKeepAlive(p.conn)
		}
	}
}

func (p *PeerConnection) requestNextChunk() bool {
	if p.currentPiece == nil || p.nbParallelChunks >= kMaxParallelChunks {
		return false
	}
	if p.nextIdx*kBlockSize >= uint32(p.currentPiece.pieceSize) {
		return false
	}
	chunkSize := uint32(kBlockSize)
	if (p.nextIdx+1)*kBlockSize > uint32(p.currentPiece.pieceSize) {
		chunkSize = uint32(p.currentPiece.pieceSize) - p.nextIdx*kBlockSize
	}
	sendRequest(p.conn, uint32(p.currentPiece.index), p.nextIdx*kBlockSize, chunkSize)
	p.nbParallelChunks += 1
	p.nextIdx += 1
	return true
}

