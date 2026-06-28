package peer

import (
	"context"
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
	tag string

	ipPort    string
	conn      *net.TCPConn
	connected bool

	lastAliveTime time.Time
	infoHash      [20]byte
	peerId        [20]byte

	leecher Leecher
	seeder  Seeder

	// Snapshot of each child's cumulative byte counter captured on the last
	// speed sample, so a rate can be derived from the delta.
	lastDownBytes  int64
	lastUpBytes    int64
	lastDownSample time.Time
	lastUpSample   time.Time

	managerChan chan<- PeerEvent
	reporter    Reporter
}

// SetReporter attaches a debug-only observer for this connection's lifecycle
// signals. Not required for normal operation.
func (p *PeerConnection) SetReporter(r Reporter) {
	p.reporter = r
	p.seeder.reporter = r
	p.leecher.reporter = r
}

func NewPeerConnection(tag string, ip string, port int, infoHash, peerId [20]byte, managerChan chan<- PeerEvent, scheduler PieceScheduler) (PeerConnection, error) {
	return PeerConnection{
		tag:           tag,
		ipPort:        net.JoinHostPort(ip, strconv.Itoa(port)),
		conn:          nil,
		lastAliveTime: time.Now(),
		infoHash:      infoHash,
		peerId:        peerId,
		managerChan:   managerChan,
		leecher: Leecher{
			tag:              tag,
			conn:             nil,
			isChoking:        true,
			amInterested:     false,
			bitfieldSize:     scheduler.GetLocalBitfield().n,
			bitfield:         Bitfield{},
			currentPiece:     nil,
			nextIdx:          0,
			nbParallelChunks: 0,
			managerChan:      managerChan,
			scheduler:        scheduler,
		},
		seeder: Seeder{
			conn:          nil,
			amChoking:     true,
			isInterested:  false,
			localBitfield: scheduler.GetLocalBitfield(),
			scheduler:     scheduler,
		},
	}, nil
}

func (p *PeerConnection) Connect() error {
	dialer := net.Dialer{Timeout: kDialTimeout}
	conn, err := dialer.Dial("tcp", p.ipPort)
	if err != nil {
		return err
	}
	p.conn = conn.(*net.TCPConn)
	if err := p.conn.SetDeadline(time.Now().Add(kHandshakeTimeout)); err != nil {
		return err
	}
	if err := sendHandshake(p.conn, p.infoHash, p.peerId); err != nil {
		return err
	}
	if err := receiveHandshake(p.conn, p.infoHash); err != nil {
		return err
	}
	p.connected = true
	return nil
}

func (p *PeerConnection) Run(ctx context.Context) (retErr error) {
	if !p.connected {
		return fmt.Errorf("PeerConnection::Run: Not connected yet")
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer func() {
		p.seeder.shutdown()
		p.leecher.shutdown()
	}()

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
			p.sendClosedEvent()
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

func (p *PeerConnection) sendClosedEvent() {
	p.managerChan <- PeerEvent{
		tag:     p.tag,
		kind:    ConnectionClosed,
		payload: nil,
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

// getDownloadSpeed returns bytes/sec measured over the interval since the last
// call: it advances the sampling state, so it must only be called from a single
// goroutine (the PeerManager). The first call sets the baseline and returns 0.
func (p *PeerConnection) getDownloadSpeed() int {
	if !p.connected {
		return 0
	}
	now := time.Now()
	cur := p.leecher.bytesDownloaded()
	if p.lastDownSample.IsZero() {
		p.lastDownSample = now
		p.lastDownBytes = cur
		return 0
	}
	elapsed := now.Sub(p.lastDownSample).Seconds()
	delta := cur - p.lastDownBytes
	p.lastDownSample = now
	p.lastDownBytes = cur
	if elapsed <= 0 {
		return 0
	}
	return int(float64(delta) / elapsed)
}

func (p *PeerConnection) getUploadSpeed() int {
	if !p.connected {
		return 0
	}
	now := time.Now()
	cur := p.seeder.bytesUploaded()
	if p.lastUpSample.IsZero() {
		p.lastUpSample = now
		p.lastUpBytes = cur
		return 0
	}
	elapsed := now.Sub(p.lastUpSample).Seconds()
	delta := cur - p.lastUpBytes
	p.lastUpSample = now
	p.lastUpBytes = cur
	if elapsed <= 0 {
		return 0
	}
	return int(float64(delta) / elapsed)
}
