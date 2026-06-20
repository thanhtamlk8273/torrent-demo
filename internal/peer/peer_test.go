package peer

import (
	"context"
	"crypto/sha1"
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
	completedCh chan int
	unclaimedCh chan int
}

func newMock(work PieceWork, hasWork bool) *mockScheduler {
	return &mockScheduler{
		work:        work,
		hasWork:     hasWork,
		completedCh: make(chan int, 1),
		unclaimedCh: make(chan int, 1),
	}
}

func (m *mockScheduler) GetWork(_ Bitfield) (PieceWork, bool) { return m.work, m.hasWork }
func (m *mockScheduler) CompletePiece(index int, _ []byte) error {
	m.completedCh <- index
	return nil
}
func (m *mockScheduler) UnclaimPiece(index int) { m.unclaimedCh <- index }

// newTestPeer creates a loopback TCP connection and returns a configured
// PeerConnection plus the remote net.Conn for the test to write messages into.
func newTestPeer(t *testing.T, infoHash [20]byte, sched *mockScheduler) (*PeerConnection, net.Conn) {
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
		remoteCh <- c
	}()

	clientConn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	remote := <-remoteCh
	t.Cleanup(func() { remote.Close(); clientConn.Close() })

	p := &PeerConnection{
		conn:      clientConn.(*net.TCPConn),
		sendCh:    make(chan message.Message, 16),
		infoHash:  infoHash,
		scheduler: sched,
	}
	return p, remote
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

func TestHandshakeInfoHashMismatch(t *testing.T) {
	var infoHash [20]byte
	infoHash[0] = 0xAA

	p, remote := newTestPeer(t, infoHash, newMock(PieceWork{}, false))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- p.Run(ctx) }()

	go func() {
		var hs message.Handshake
		hs.ReadFrom(remote)
		var wrong [20]byte
		wrong[0] = 0xBB
		reply := message.NewHandshake(wrong, [20]byte{})
		reply.WriteTo(remote)
	}()

	select {
	case err := <-errCh:
		if err == nil {
			t.Error("expected error on info_hash mismatch, got nil")
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for handshake error")
	}
}

// runAndWait starts Run in a goroutine and returns a channel that receives
// the error when Run exits. Cancelling ctx or closing the conn triggers exit.
func runAndWait(ctx context.Context, p *PeerConnection) <-chan error {
	ch := make(chan error, 1)
	go func() { ch <- p.Run(ctx) }()
	return ch
}

// State-change tests close the remote conn after sending the message,
// then wait for Run to exit. Reading p.* AFTER Run exits is race-free
// because the goroutine is gone — no concurrent writes.

func TestRunChoke(t *testing.T) {
	var infoHash [20]byte
	work, _ := singleBlockWork()
	p, remote := newTestPeer(t, infoHash, newMock(work, true))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := runAndWait(ctx, p)

	go func() {
		doHandshake(t, remote, infoHash)
		choke := message.NewChoke()
		choke.WriteTo(remote)
		remote.Close() // EOF causes Run to exit after processing Choke
	}()

	select {
	case <-errCh:
		if !p.isChoking {
			t.Error("expected isChoking = true after Choke")
		}
	case <-time.After(time.Second):
		t.Error("timeout")
	}
}

func TestRunUnChoke(t *testing.T) {
	var infoHash [20]byte
	work, _ := singleBlockWork()
	p, remote := newTestPeer(t, infoHash, newMock(work, true))
	p.isChoking = true

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := runAndWait(ctx, p)

	go func() {
		doHandshake(t, remote, infoHash)
		unchoke := message.NewUnChoke()
		unchoke.WriteTo(remote)
		remote.Close()
	}()

	select {
	case <-errCh:
		if p.isChoking {
			t.Error("expected isChoking = false after UnChoke")
		}
	case <-time.After(time.Second):
		t.Error("timeout")
	}
}

func TestRunBitfield(t *testing.T) {
	var infoHash [20]byte
	work, _ := singleBlockWork()
	p, remote := newTestPeer(t, infoHash, newMock(work, true))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := runAndWait(ctx, p)

	go func() {
		doHandshake(t, remote, infoHash)
		bf := message.NewBitfield([]byte{0xFF})
		bf.WriteTo(remote)
		remote.Close()
	}()

	select {
	case <-errCh:
		if len(p.bitfield) == 0 || p.bitfield[0] != 0xFF {
			t.Errorf("bitField = %v, want [0xFF]", p.bitfield)
		}
	case <-time.After(time.Second):
		t.Error("timeout")
	}
}

func TestRunHave(t *testing.T) {
	var infoHash [20]byte
	work, _ := singleBlockWork()
	p, remote := newTestPeer(t, infoHash, newMock(work, true))
	p.bitfield = make(Bitfield, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := runAndWait(ctx, p)

	go func() {
		doHandshake(t, remote, infoHash)
		have := message.NewHave(3)
		have.WriteTo(remote)
		remote.Close()
	}()

	select {
	case <-errCh:
		if !p.bitfield.Has(3) {
			t.Error("expected bit 3 set after Have(3)")
		}
	case <-time.After(time.Second):
		t.Error("timeout")
	}
}

func TestRunPieceCompleteHashMatch(t *testing.T) {
	var infoHash [20]byte
	work, blockData := singleBlockWork()
	sched := newMock(work, true)
	p, remote := newTestPeer(t, infoHash, sched)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	go func() {
		doHandshake(t, remote, infoHash)
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
	case <-time.After(time.Second):
		t.Error("timeout waiting for CompletePiece")
	}
}

func TestRunPieceCompleteHashMismatch(t *testing.T) {
	var infoHash [20]byte
	work, _ := singleBlockWork()
	sched := newMock(work, true)
	p, remote := newTestPeer(t, infoHash, sched)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)

	go func() {
		doHandshake(t, remote, infoHash)
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
	p, remote := newTestPeer(t, infoHash, sched)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := runAndWait(ctx, p)

	go func() {
		doHandshake(t, remote, infoHash)
		// Send piece for index 99 — peer holds index 0, must ignore
		piece := message.NewPiece(99, 0, blockData)
		piece.WriteTo(remote)
		remote.Close() // exit cleanly after the wrong-index piece
	}()

	select {
	case <-errCh:
		// Run exited — safe to read state, no concurrent goroutine
		select {
		case <-sched.completedCh:
			t.Error("CompletePiece should not be called for wrong index")
		case idx := <-sched.unclaimedCh:
			if idx != 0 {
				t.Errorf("UnclaimPiece index = %d, want 0", idx)
			}
		default:
			t.Error("UnclaimPiece should be called for in-flight work when the peer exits")
		}
	case <-time.After(time.Second):
		t.Error("timeout")
	}
}
