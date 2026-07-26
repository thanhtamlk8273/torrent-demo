package peer

import (
	"context"
	"crypto/sha1"
	"fmt"
	"net"
	"testing"
	"time"
	"torrent-demo/internal/message"
)

// mockScheduler signals completion/unclaim via channels so tests can block
// instead of polling, which avoids data races on the scheduler's own fields.
type mockScheduler struct {
	work        PieceWork
	hasWork     bool
	local       Bitfield
	completedCh chan int
	unclaimedCh chan int
}

func newMock(work PieceWork, hasWork bool) *mockScheduler {
	return &mockScheduler{
		work:    work,
		hasWork: hasWork,
		// 8-piece domain with nothing owned: Any() is false (so connect()
		// won't send a bitfield) but Size() gives remote bitfields a count.
		local:       *NewBitfield(make([]byte, 1), 8),
		completedCh: make(chan int, 1),
		unclaimedCh: make(chan int, 1),
	}
}

func (m *mockScheduler) GetWork(_ Bitfield) (PieceWork, bool) { return m.work, m.hasWork }
func (m *mockScheduler) CompletePiece(index int, _ []byte) error {
	m.completedCh <- index
	return nil
}
func (m *mockScheduler) UnclaimPiece(index int)     { m.unclaimedCh <- index }
func (m *mockScheduler) GetLocalBitfield() Bitfield { return m.local }
func (m *mockScheduler) GetBlock(index, begin, length int) ([]byte, error) {
	return nil, fmt.Errorf("GetBlock not implemented in mock")
}

// newTestPeer starts a loopback listener and returns a PeerConnection wired to
// dial it, plus a channel that yields the server-accepted remote conn once
// Run's connect() dials in. seam #1 in peer.go wires p.conn into
// leecher.conn/seeder.conn after the dial, so tests never inject conn directly.
func newTestPeer(t *testing.T, infoHash [20]byte, sched *mockScheduler) (*PeerConnection, <-chan net.Conn) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	remoteCh := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		ln.Close()
		if err != nil {
			return
		}
		t.Cleanup(func() { c.Close() })
		remoteCh <- c
	}()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	p := &PeerConnection{
		ctx:        ctx,
		cancelFunc: cancel,
		ipPort:     ln.Addr().String(),
		infoHash:   infoHash,
		leecher: Leecher{
			isChoking:    true,
			bitfieldSize: sched.GetLocalBitfield().n,
			scheduler:    sched,
		},
		seeder: Seeder{
			amChoking:     true,
			localBitfield: sched.GetLocalBitfield(),
			scheduler:     sched,
		},
	}
	return p, remoteCh
}

func waitPeerStopped(t *testing.T, p *PeerConnection) {
	t.Helper()
	select {
	case <-p.ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for peer to stop")
	}
}

func TestPeerBitfieldStatsAreSnapshots(t *testing.T) {
	p := &PeerConnection{
		leecher: Leecher{bitfield: *NewBitfield(nil, 8)},
	}
	p.updateStats()

	p.leecher.bitfield.Set(1)
	snapshot := p.GetBitField()
	if snapshot.Has(1) {
		t.Fatal("published stats changed when the leecher bitfield changed")
	}

	snapshot.Set(2)
	published := p.GetBitField()
	if published.Has(2) {
		t.Fatal("mutating GetBitField result changed the published stats")
	}
}

// waitStats polls the peer's published PeerStats snapshot until pred matches,
// the race-free seam for observing Run's fire-and-forget state changes.
func waitStats(t *testing.T, p *PeerConnection, pred func(PeerStats) bool) PeerStats {
	t.Helper()
	deadline := time.After(2 * time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			if s, ok := p.getStats(); ok && pred(s) {
				return s
			}
		case <-deadline:
			t.Fatal("timeout waiting for stats condition")
			return PeerStats{}
		}
	}
}

// doHandshake simulates the remote peer's handshake response.
func doHandshake(t *testing.T, remote net.Conn, infoHash [20]byte) {
	t.Helper()
	var hs message.Handshake
	if _, err := hs.ReadFrom(remote); err != nil {
		t.Error("remote: failed to read handshake:", err)
		return
	}
	reply := message.NewHandshake(infoHash, [20]byte{})
	if _, err := reply.WriteTo(remote); err != nil {
		t.Error("remote: failed to write handshake:", err)
	}
}

// singleBlockWork returns PieceWork and the matching block data for a
// one-block piece so tests can produce a hash-valid complete piece easily.
func singleBlockWork() (PieceWork, []byte) {
	data := make([]byte, kBlockSize)
	hash := sha1.Sum(data)
	return PieceWork{Index: 0, Size: kBlockSize, Hash: hash}, data
}

// unchokeAndAwaitRequest drives the peer through the real state machine before
// a Piece can be delivered: a freshly handshaken peer is choked and will not
// claim work or send Request messages until it is unchoked. We send UnChoke,
// then read remote-bound messages until the peer's block Request arrives. Only
// then has the peer set up its in-flight piece and is ready to accept the
// matching Piece. Returns the observed Request so callers can assert on it.
func unchokeAndAwaitRequest(t *testing.T, remote net.Conn) message.Request {
	t.Helper()
	unchoke := message.NewUnchoke()
	if _, err := unchoke.WriteTo(remote); err != nil {
		t.Error("remote: failed to write unchoke:", err)
		return message.Request{}
	}
	for {
		msg, err := message.Read(remote)
		if err != nil {
			t.Error("remote: failed to read message while awaiting request:", err)
			return message.Request{}
		}
		if req, ok := msg.(*message.Request); ok {
			return *req
		}
		// Skip the peer's Interested (and any other pre-request) messages.
	}
}

func TestHandshakeInfoHashMismatch(t *testing.T) {
	var infoHash [20]byte
	infoHash[0] = 0xAA

	p, remoteCh := newTestPeer(t, infoHash, newMock(PieceWork{}, false))

	p.Run()

	go func() {
		remote := <-remoteCh
		var hs message.Handshake
		hs.ReadFrom(remote)
		var wrong [20]byte
		wrong[0] = 0xBB
		reply := message.NewHandshake(wrong, [20]byte{})
		reply.WriteTo(remote)
	}()

	waitPeerStopped(t, p)
}

func TestRunChoke(t *testing.T) {
	var infoHash [20]byte
	work, _ := singleBlockWork()
	p, remoteCh := newTestPeer(t, infoHash, newMock(work, true))

	p.Run()

	result := make(chan error, 1)
	go func() {
		remote := <-remoteCh
		doHandshake(t, remote, infoHash)
		choke := message.NewChoke()
		if _, err := choke.WriteTo(remote); err != nil {
			result <- err
			return
		}
		if err := remote.SetReadDeadline(time.Now().Add(400 * time.Millisecond)); err != nil {
			result <- err
			return
		}
		for {
			msg, err := message.Read(remote)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					result <- nil
					return
				}
				result <- err
				return
			}
			if _, ok := msg.(*message.Request); ok {
				result <- fmt.Errorf("received block request while peer was choked")
				return
			}
		}
	}()

	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestRunUnChoke(t *testing.T) {
	var infoHash [20]byte
	work, _ := singleBlockWork()
	p, remoteCh := newTestPeer(t, infoHash, newMock(work, true))
	p.leecher.isChoking = true

	p.Run()

	requested := make(chan message.Request, 1)
	go func() {
		remote := <-remoteCh
		doHandshake(t, remote, infoHash)
		requested <- unchokeAndAwaitRequest(t, remote)
	}()

	select {
	case req := <-requested:
		if int(req.Index) != work.Index {
			t.Errorf("requested index = %d, want %d", req.Index, work.Index)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for request after unchoke")
	}
}

func TestRunBitfield(t *testing.T) {
	var infoHash [20]byte
	work, _ := singleBlockWork()
	p, remoteCh := newTestPeer(t, infoHash, newMock(work, true))

	p.Run()

	go func() {
		remote := <-remoteCh
		doHandshake(t, remote, infoHash)
		bf := message.NewBitfield([]byte{0xFF})
		bf.WriteTo(remote)
	}()

	s := waitStats(t, p, func(s PeerStats) bool {
		b := s.Bitfield.Bytes()
		return len(b) > 0 && b[0] == 0xFF
	})
	if b := s.Bitfield.Bytes(); len(b) == 0 || b[0] != 0xFF {
		t.Errorf("bitField = %v, want [0xFF]", b)
	}
}

func TestRunHave(t *testing.T) {
	var infoHash [20]byte
	work, _ := singleBlockWork()
	p, remoteCh := newTestPeer(t, infoHash, newMock(work, true))
	p.leecher.bitfield = *NewBitfield(make([]byte, 1), 8)

	p.Run()

	go func() {
		remote := <-remoteCh
		doHandshake(t, remote, infoHash)
		have := message.NewHave(3)
		have.WriteTo(remote)
	}()

	waitStats(t, p, func(s PeerStats) bool { return s.Bitfield.Has(3) })
}

func TestRunPieceCompleteHashMatch(t *testing.T) {
	var infoHash [20]byte
	work, blockData := singleBlockWork()
	sched := newMock(work, true)
	p, remoteCh := newTestPeer(t, infoHash, sched)

	p.Run()

	go func() {
		remote := <-remoteCh
		doHandshake(t, remote, infoHash)
		req := unchokeAndAwaitRequest(t, remote)
		if req.Index != 0 {
			t.Errorf("requested index = %d, want 0", req.Index)
		}
		piece := message.NewPiece(0, 0, blockData)
		piece.WriteTo(remote)
	}()

	select {
	case idx := <-sched.completedCh:
		if idx != 0 {
			t.Errorf("CompletePiece index = %d, want 0", idx)
		}
	case <-sched.unclaimedCh:
		t.Error("UnclaimPiece should not be called on hash match")
	case <-time.After(1 * time.Second):
		t.Error("timeout waiting for CompletePiece")
	}
}

func TestRunPieceCompleteHashMismatch(t *testing.T) {
	var infoHash [20]byte
	work, _ := singleBlockWork()
	sched := newMock(work, true)
	p, remoteCh := newTestPeer(t, infoHash, sched)

	p.Run()

	go func() {
		remote := <-remoteCh
		doHandshake(t, remote, infoHash)
		unchokeAndAwaitRequest(t, remote)
		badData := make([]byte, kBlockSize)
		badData[0] = 0xFF // won't match the all-zero hash in work
		piece := message.NewPiece(0, 0, badData)
		piece.WriteTo(remote)
	}()

	select {
	case <-sched.completedCh:
		t.Error("CompletePiece should not be called on hash mismatch")
	case idx := <-sched.unclaimedCh:
		if idx != 0 {
			t.Errorf("UnclaimPiece index = %d, want 0", idx)
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for UnclaimPiece")
	}
}

func TestRunPieceWrongIndexIgnored(t *testing.T) {
	var infoHash [20]byte
	work, blockData := singleBlockWork() // claims piece 0
	sched := newMock(work, true)
	p, remoteCh := newTestPeer(t, infoHash, sched)

	p.Run()

	go func() {
		remote := <-remoteCh
		doHandshake(t, remote, infoHash)
		// Send piece for index 99 — peer holds index 0, must ignore
		piece := message.NewPiece(99, 0, blockData)
		piece.WriteTo(remote)
		remote.Close() // exit cleanly after the wrong-index piece; leecher.shutdown() unclaims piece 0
	}()

	select {
	case <-sched.completedCh:
		t.Error("CompletePiece should not be called for wrong index")
	case idx := <-sched.unclaimedCh:
		if idx != 0 {
			t.Errorf("UnclaimPiece index = %d, want 0", idx)
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for UnclaimPiece")
	}
}
