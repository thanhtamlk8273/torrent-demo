package peermgr

import (
	"fmt"
	"sync"
	"testing"
	"torrent-demo/internal/peer"
	"torrent-demo/internal/tracker_client"
)

// fakeScheduler/book let us construct a PeerManager without a live torrent or
// network. Only GetLocalBitfield is consulted by AddPeer/Snapshot.
type fakeScheduler struct{}

func (fakeScheduler) GetWork(peer.Bitfield) (peer.PieceWork, bool) { return peer.PieceWork{}, false }
func (fakeScheduler) CompletePiece(int, []byte) error              { return nil }
func (fakeScheduler) UnclaimPiece(int)                             {}
func (fakeScheduler) GetLocalBitfield() peer.Bitfield              { return *peer.NewBitfield(nil, 10) }
func (fakeScheduler) GetBlock(int, int, int) ([]byte, error)       { return nil, nil }

type fakeBook struct{}

func (fakeBook) SubmitAvailability([]int) []int { return nil }

// TestConcurrentAddPeerAndSnapshot exercises the two externally-called entry
// points from concurrent goroutines, exactly the shape that used to race
// (AddPeer from Session's refresh loop vs Snapshot from the GUI/progress
// watcher). Run with -race this is the concrete check that the mutex is held
// at the entry points and nowhere else.
func TestConcurrentAddPeerAndSnapshot(t *testing.T) {
	pm := NewPeerManager([20]byte{}, [20]byte{}, fakeScheduler{}, fakeBook{})

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			pm.AddPeer(tracker_client.Peer{
				Ip:   fmt.Sprintf("10.0.%d.%d", i/250, i%250),
				Port: 6881 + i%500,
			})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			_ = pm.Snapshot()
		}
	}()
	wg.Wait()
}

// TestSnapshotFields sanity-checks the snapshot contents for both a waiting
// and an active-shaped view. The active path needs a live connection, so only
// the waiting path is asserted here.
func TestSnapshotWaitingFields(t *testing.T) {
	pm := NewPeerManager([20]byte{}, [20]byte{}, fakeScheduler{}, fakeBook{})
	pm.AddPeer(tracker_client.Peer{Ip: "1.2.3.4", Port: 7000})

	snap := pm.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snap))
	}
	s := snap[0]
	if s.Active {
		t.Fatalf("expected waiting peer, got active=%v", s.Active)
	}
	if s.Address != "1.2.3.4:7000" {
		t.Fatalf("unexpected address %q", s.Address)
	}
	if s.BitfieldFraction != 0 {
		t.Fatalf("expected empty bitfield fraction 0, got %v", s.BitfieldFraction)
	}
}

func TestBitfieldFraction(t *testing.T) {
	bf := peer.NewBitfield([]byte{0b11000000}, 8)
	if got := bitfieldFraction(*bf); got != 0.25 {
		t.Fatalf("expected 0.25, got %v", got)
	}
	if got := bitfieldFraction(*peer.NewBitfield(nil, 0)); got != 0 {
		t.Fatalf("expected 0 for empty bitfield, got %v", got)
	}
}
