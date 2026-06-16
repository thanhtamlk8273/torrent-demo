package piecemanager

import (
	"bytes"
	"os"
	"testing"
)

func makeTempFile(t *testing.T, size int64) *os.File {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "layout-*")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestWritePieceOutOfRange(t *testing.T) {
	layout := &TorrentLayout{piecesCount: 3, pieceSize: 10}
	if err := layout.writePiece(3, make([]byte, 10)); err == nil {
		t.Fatal("expected error for out-of-range index, got nil")
	}
}

func TestWritePieceNoFileFound(t *testing.T) {
	f := makeTempFile(t, 10)
	layout := &TorrentLayout{
		piecesCount: 5,
		pieceSize:   10,
		files:       []FileLayout{{torrentOffset: 0, size: 10, file: f}},
	}
	// piece 3: pieceOffset=30, outside file [0, 10)
	if err := layout.writePiece(3, make([]byte, 10)); err == nil {
		t.Fatal("expected error for piece with no matching file, got nil")
	}
}

func TestWritePieceSingleFile(t *testing.T) {
	f := makeTempFile(t, 20)
	layout := &TorrentLayout{
		piecesCount: 2,
		pieceSize:   10,
		files:       []FileLayout{{torrentOffset: 0, size: 20, file: f}},
	}
	data := bytes.Repeat([]byte{0xAB}, 10)
	if err := layout.writePiece(1, data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := make([]byte, 10)
	if _, err := f.ReadAt(got, 10); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("got %x, want %x", got, data)
	}
}

// Piece straddles two files: first 5 bytes go to f1, remaining 5 bytes go to f2.
func TestWritePieceSpansFiles(t *testing.T) {
	f1 := makeTempFile(t, 15) // torrent bytes 0-14
	f2 := makeTempFile(t, 15) // torrent bytes 15-29
	layout := &TorrentLayout{
		piecesCount: 3,
		pieceSize:   10,
		files: []FileLayout{
			{torrentOffset: 0, size: 15, file: f1},
			{torrentOffset: 15, size: 15, file: f2},
		},
	}
	// piece 1: pieceOffset=10, 5 bytes fit in f1 (offset 10-14), 5 bytes spill to f2 (offset 0-4)
	data := bytes.Repeat([]byte{0xCD}, 10)
	if err := layout.writePiece(1, data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	gotF1 := make([]byte, 5)
	if _, err := f1.ReadAt(gotF1, 10); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotF1, data[:5]) {
		t.Errorf("f1 tail: got %x, want %x", gotF1, data[:5])
	}

	gotF2 := make([]byte, 5)
	if _, err := f2.ReadAt(gotF2, 0); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotF2, data[5:]) {
		t.Errorf("f2 head: got %x, want %x", gotF2, data[5:])
	}
}
