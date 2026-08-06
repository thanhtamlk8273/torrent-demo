package peer

import (
	"context"
	"log"
	"net"
	"strconv"
	"sync/atomic"
	"time"
	"torrent-demo/internal/message"
)

const (
	kMessageChannelSize int = 64
	kMaxParallelChunks      = 64
	kDialTimeout            = 5 * time.Second
	kHandshakeTimeout       = 10 * time.Second
	kLeecherStartDelay      = 2 * time.Second
)

type PeerStats struct {
	Bitfield        Bitfield
	DownloadedBytes uint64
	UploadedBytes   uint64
}

type CommandKind int

const (
	LeechPeer CommandKind = iota
	StopLeechPeer
	BloockPeer
	UblockPeer
)

type PeerCommand struct {
	kind CommandKind
}

type PeerConnection struct {
	ctx        context.Context
	cancelFunc context.CancelFunc

	tag string

	ipPort string
	conn   *net.TCPConn

	infoHash [20]byte
	peerId   [20]byte

	leecher Leecher
	seeder  Seeder

	// Snapshot of each child's cumulative byte counter captured on the last
	// speed sample, so a rate can be derived from the delta.
	lastDownBytes int64
	lastUpBytes   int64

	commandChan chan PeerCommand

	leecherStartDelay time.Duration
	stats             atomic.Pointer[PeerStats]
}

func (p *PeerConnection) logf(format string, args ...any) {
	log.Printf("[Peer %s] "+format, append([]any{p.tag}, args...)...)
}

func NewPeerConnection(parentCtx context.Context, tag string, ip string, port int, infoHash, peerId [20]byte, scheduler PieceScheduler) *PeerConnection {
	ctx, cancel := context.WithCancel(parentCtx)
	return &PeerConnection{
		ctx:               ctx,
		cancelFunc:        cancel,
		tag:               tag,
		ipPort:            net.JoinHostPort(ip, strconv.Itoa(port)),
		conn:              nil,
		infoHash:          infoHash,
		peerId:            peerId,
		commandChan:       make(chan PeerCommand),
		leecherStartDelay: kLeecherStartDelay,
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
			scheduler:        scheduler,
		},
		seeder: Seeder{
			conn:          nil,
			amChoking:     true,
			isInterested:  false,
			localBitfield: scheduler.GetLocalBitfield(),
			scheduler:     scheduler,
		},
	}
}

func (p *PeerConnection) connect(ctx context.Context) error {
	p.logf("stage=dialing address=%s", p.ipPort)
	dialer := net.Dialer{Timeout: kDialTimeout}
	conn, err := dialer.Dial("tcp", p.ipPort)
	if err != nil {
		return err
	}
	p.conn = conn.(*net.TCPConn)
	p.leecher.conn = p.conn
	p.seeder.conn = p.conn
	p.logf("stage=tcp-connected remote=%s", p.conn.RemoteAddr())

	if err := p.conn.SetDeadline(time.Now().Add(kHandshakeTimeout)); err != nil {
		return err
	}
	p.logf("stage=handshake-sending")
	if err := sendHandshake(p.conn, p.infoHash, p.peerId); err != nil {
		return err
	}
	p.logf("stage=handshake-waiting")
	if err := receiveHandshake(p.conn, p.infoHash); err != nil {
		return err
	}
	p.logf("stage=handshake-complete")

	if deadline, ok := ctx.Deadline(); ok {
		if err := p.conn.SetDeadline(deadline); err != nil {
			return err
		}
	} else {
		if err := p.conn.SetDeadline(time.Time{}); err != nil {
			return err
		}
	}
	return nil
}

func (p *PeerConnection) Run() {
	go func() {
		defer p.cancelFunc()
		p.logf("stage=starting")
		if err := p.connect(p.ctx); err != nil {
			p.logf("stage=connection-failed err=%v", err)
			p.updateStats()
			return
		}

		defer p.logf("stage=stopped")

		p.seeder.init()
		time.Sleep(p.leecherStartDelay)
		p.leecher.init()
		defer func() {
			p.seeder.shutdown()
			p.leecher.shutdown()
		}()

		p.logf("stage=active")
		go p.keepAliveLoop(p.ctx)

		advanceTicker := time.NewTicker(250 * time.Millisecond)
		recvMsg := p.createReadChannel(p.ctx, p.cancelFunc)

		// lastAliveTime := time.Now()
		for {
			select {
			case <-p.ctx.Done():
				p.logf("stage=stopping reason=%v", p.ctx.Err())
				return
			case msg := <-recvMsg:
				// lastAliveTime = time.Now()
				p.seeder.processMessage(msg)
				p.seeder.advance()
				p.leecher.processMessage(msg)
				p.leecher.advance()
			case <-advanceTicker.C:
				p.seeder.advance()
				p.leecher.advance()
				p.updateStats()
			}
		}
	}()
}

func (pc *PeerConnection) Shutdown() {
	pc.logf("stage=shutdown-requested")
	pc.cancelFunc()
	if pc.conn != nil {
		pc.conn.Close()
	}
}

func (p *PeerConnection) getStats() (PeerStats, bool) {
	stats := p.stats.Load()
	if stats != nil {
		return *stats, true
	}
	return PeerStats{}, false
}

func (p *PeerConnection) updateStats() {
	newStats := PeerStats{
		Bitfield:        p.leecher.bitfield.Clone(),
		DownloadedBytes: p.leecher.downloadedBytes,
		UploadedBytes:   p.seeder.uploadedBytes,
	}
	p.stats.Store(&newStats)
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
				select {
				case <-ctx.Done():
					p.logf("stage=reader-stopped reason=%v", ctx.Err())
				default:
					p.logf("stage=read-failed err=%v", err)
				}
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
	ticker := time.NewTicker(1 * time.Minute)
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

func (p *PeerConnection) GetDownloadedBytes() uint64 {
	stats, ok := p.getStats()
	if !ok {
		return 0
	}
	return stats.DownloadedBytes
}

func (p *PeerConnection) GetUploadedBytes() uint64 {
	stats, ok := p.getStats()
	if !ok {
		return 0
	}
	return stats.UploadedBytes
}

func (p *PeerConnection) GetBitField() Bitfield {
	stats, ok := p.getStats()
	if !ok {
		return Bitfield{}
	}
	return stats.Bitfield.Clone()
}
