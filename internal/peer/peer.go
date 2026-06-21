package peer

import (
	"context"
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
	infoHash      [20]byte
	peerId        [20]byte

	leecher Leecher
	seeder  Seeder

	reporter Reporter
}

// SetReporter attaches a debug-only observer for this connection's lifecycle
// signals. Not required for normal operation.
func (p *PeerConnection) SetReporter(r Reporter) {
	p.reporter = r
	p.seeder.reporter = r
	p.leecher.reporter = r
}

func NewPeerConnection(ip string, port int, infoHash, peerId [20]byte, scheduler PieceScheduler) (PeerConnection, error) {
	dialer := net.Dialer{Timeout: kDialTimeout}
	conn, err := dialer.Dial("tcp", net.JoinHostPort(ip, strconv.Itoa(port)))
	if err != nil {
		return PeerConnection{}, err
	}
	return PeerConnection{
		conn:          conn.(*net.TCPConn),
		lastAliveTime: time.Now(),
		infoHash:      infoHash,
		peerId:        peerId,
		leecher: Leecher{
			conn:             conn.(*net.TCPConn),
			isChoking:        true,
			amInterested:     false,
			bitfieldSize:     scheduler.GetLocalBitfield().n,
			bitfield:         Bitfield{},
			currentPiece:     nil,
			nextIdx:          0,
			nbParallelChunks: 0,
			scheduler:        scheduler,
		},
		seeder: Seeder{
			conn:          conn.(*net.TCPConn),
			amChoking:     true,
			isInterested:  false,
			localBitfield: scheduler.GetLocalBitfield(),
			scheduler:     scheduler,
		},
	}, nil
}

func (p *PeerConnection) connect() error {
	if err := sendHandshake(p.conn, p.infoHash, p.peerId); err != nil {
		return err
	}
	if err := receiveHandshake(p.conn, p.infoHash); err != nil {
		return err
	}
	return nil
}

func (p *PeerConnection) Run(ctx context.Context) (retErr error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer func() {
		p.seeder.shutdown()
		p.leecher.shutdown()
	}()

	if err := p.conn.SetDeadline(time.Now().Add(kHandshakeTimeout)); err != nil {
		return err
	}
	if err := p.connect(); err != nil {
		return err
	}
	p.leecher.init()
	p.seeder.init()

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

	advanceTicker := time.NewTicker(250 * time.Millisecond)
	recvMsg := p.createReadChannel(ctx, cancel)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg := <-recvMsg:
			p.lastAliveTime = time.Now()
			p.seeder.processMessage(msg)
			p.seeder.advance()
			p.leecher.processMessage(msg)
			p.leecher.advance()
		case <-advanceTicker.C:
			p.seeder.advance()
			p.leecher.advance()
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
