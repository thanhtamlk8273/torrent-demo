package piecemanager

import (
	"testing"
	"torrent-demo/internal/metainfo"
	"torrent-demo/internal/peer"
)

// newTestPM builds a 3-piece (10+10+5 = 25 byte) single-file torrent in a temp
// dir. The piece hashes are deliberately non-zero so they never match the
// freshly-truncated (all-zero) file, leaving every piece Unclaimed after the
// constructor's verify pass — i.e. a clean "nothing downloaded yet" manager.
func newTestPM(t *testing.T) *PieceManager {
	t.Helper()
	pieces := make([]byte, 3*20)
	for i := range pieces {
		pieces[i] = byte(i%251 + 1) // non-zero, won't equal any zero-block hash
	}
	info := metainfo.SingleFileInfo{
		InfoCommon: metainfo.InfoCommon{PieceLength: 10, Pieces: pieces},
		Name:       "data.bin",
		Length:     25,
	}
	meta := metainfo.MetaInfo{Info: metainfo.InfoField{Info: info}}

	pm, err := NewPieceManager(t.TempDir(), meta)
	if err != nil {
		t.Fatalf("NewPieceManager: %v", err)
	}
	return pm
}

// fullBitfield returns a bitfield advertising all 3 pieces as available.
func fullBitfield() peer.Bitfield {
	bf := make(peer.Bitfield, 1)
	bf.Set(0)
	bf.Set(1)
	bf.Set(2)
	return bf
}

func TestGetWorkClaimsEachAvailablePieceOnce(t *testing.T) {
	pm := newTestPM(t)
	bf := fullBitfield()

	seen := map[int]bool{}
	for i := 0; i < 3; i++ {
		w, ok := pm.GetWork(bf)
		if !ok {
			t.Fatalf("GetWork call %d: expected work, got none", i)
		}
		if seen[w.Index] {
			t.Errorf("GetWork returned piece %d twice", w.Index)
		}
		seen[w.Index] = true
		if pm.pieceStates[w.Index] != Claimed {
			t.Errorf("piece %d state = %d, want Claimed", w.Index, pm.pieceStates[w.Index])
		}
	}

	// Everything is now Claimed, so there is no more work.
	if _, ok := pm.GetWork(bf); ok {
		t.Error("expected no work once all pieces are claimed")
	}
}

func TestGetWorkSkipsPiecesThePeerLacks(t *testing.T) {
	pm := newTestPM(t)
	bf := make(peer.Bitfield, 1) // peer advertises nothing

	if _, ok := pm.GetWork(bf); ok {
		t.Error("expected no work when the peer has no pieces")
	}
}

func TestGetWorkReturnsLowestIndexAvailable(t *testing.T) {
	pm := newTestPM(t)
	bf := make(peer.Bitfield, 1)
	bf.Set(2) // only piece 2 is available

	w, ok := pm.GetWork(bf)
	if !ok {
		t.Fatal("expected work for the one available piece")
	}
	if w.Index != 2 {
		t.Errorf("Index = %d, want 2", w.Index)
	}
	if w.Size != 5 {
		t.Errorf("Size = %d, want 5 (short final piece)", w.Size)
	}
}

func TestUnclaimPieceMakesItAvailableAgain(t *testing.T) {
	pm := newTestPM(t)
	bf := fullBitfield()

	w, _ := pm.GetWork(bf)
	pm.UnclaimPiece(w.Index)
	if pm.pieceStates[w.Index] != Unclaimed {
		t.Fatalf("piece %d state = %d, want Unclaimed after UnclaimPiece", w.Index, pm.pieceStates[w.Index])
	}

	w2, ok := pm.GetWork(bf)
	if !ok || w2.Index != w.Index {
		t.Errorf("expected the unclaimed piece %d to be re-claimable, got %d (ok=%v)", w.Index, w2.Index, ok)
	}
}

func TestUnclaimPieceLeavesDonePiecesAlone(t *testing.T) {
	pm := newTestPM(t)
	pm.pieceStates[1] = Done

	pm.UnclaimPiece(1)
	if pm.pieceStates[1] != Done {
		t.Errorf("UnclaimPiece must not revert a Done piece; state = %d", pm.pieceStates[1])
	}
}

func TestCompletePieceMarksDoneAndAdvancesProgress(t *testing.T) {
	pm := newTestPM(t)
	bf := fullBitfield()
	w, _ := pm.GetWork(bf)

	if err := pm.CompletePiece(w.Index, make([]byte, w.Size)); err != nil {
		t.Fatalf("CompletePiece: %v", err)
	}
	if pm.pieceStates[w.Index] != Done {
		t.Errorf("piece %d state = %d, want Done", w.Index, pm.pieceStates[w.Index])
	}

	done, total := pm.Progress()
	if done != 1 || total != 3 {
		t.Errorf("Progress = %d/%d, want 1/3", done, total)
	}
}

// A failed write reverts a claimed piece to Unclaimed so another peer can
// retry it, rather than leaking the slot.
func TestCompletePieceWriteErrorRevertsClaimedPiece(t *testing.T) {
	pm := newTestPM(t)
	bf := fullBitfield()
	w, _ := pm.GetWork(bf)

	// Far more data than any single file holds -> writePiece fails.
	if err := pm.CompletePiece(w.Index, make([]byte, 1000)); err == nil {
		t.Fatal("expected CompletePiece to fail on oversized data")
	}
	if pm.pieceStates[w.Index] != Unclaimed {
		t.Errorf("piece %d state = %d, want Unclaimed after failed write", w.Index, pm.pieceStates[w.Index])
	}
}

// Invariant: a CompletePiece that returns an error must never leave the piece
// counted as Done. Regression guard for the old err/state coupling, where a
// failed write on a non-Claimed piece still fell through to Done.
func TestCompletePieceWriteErrorNeverMarksDone(t *testing.T) {
	pm := newTestPM(t)

	// Piece 0 is Unclaimed (not claimed first). A failing write must not flip
	// it to Done.
	if err := pm.CompletePiece(0, make([]byte, 1000)); err == nil {
		t.Fatal("expected CompletePiece to fail on oversized data")
	}
	if pm.pieceStates[0] == Done {
		t.Error("failed write must not mark the piece Done")
	}
	if done, _ := pm.Progress(); done != 0 {
		t.Errorf("Progress done = %d, want 0 after a failed write", done)
	}
}

func TestGetHashBounds(t *testing.T) {
	pieces := make([]byte, 2*20) // exactly two hashes
	for i := range pieces {
		pieces[i] = byte(i)
	}

	if _, ok := getHash(pieces, 0); !ok {
		t.Error("getHash(0) should succeed")
	}
	if _, ok := getHash(pieces, 1); !ok {
		t.Error("getHash(1) should succeed")
	}
	if _, ok := getHash(pieces, 2); ok {
		t.Error("getHash(2) should fail: past the end of the hash list")
	}
}
