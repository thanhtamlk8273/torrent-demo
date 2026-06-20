package piecemanager

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"torrent-demo/internal/metainfo"
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

func TestSafeJoinValid(t *testing.T) {
	base := t.TempDir()
	got, err := safeJoin(base, "sub", "file.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(base, "sub", "file.txt")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSafeJoinRejectsDotDotSegment(t *testing.T) {
	base := t.TempDir()
	if _, err := safeJoin(base, "..", "evil.txt"); err == nil {
		t.Fatal("expected error for \"..\" segment, got nil")
	}
}

func TestSafeJoinRejectsEmptySegment(t *testing.T) {
	base := t.TempDir()
	if _, err := safeJoin(base, ""); err == nil {
		t.Fatal("expected error for empty segment, got nil")
	}
}

func TestSafeJoinRejectsAbsoluteSegment(t *testing.T) {
	base := t.TempDir()
	if _, err := safeJoin(base, "/etc/passwd"); err == nil {
		t.Fatal("expected error for absolute segment, got nil")
	}
}

func TestSafeJoinRejectsEmbeddedTraversal(t *testing.T) {
	base := t.TempDir()
	// A single segment that itself encodes a traversal (e.g. crafted from
	// untrusted metainfo) isn't caught by the exact ".." check, but must
	// still be rejected once resolved against base.
	if _, err := safeJoin(base, "../../../etc/passwd"); err == nil {
		t.Fatal("expected error for embedded traversal, got nil")
	}
}

func TestOpenFileLayoutCreatesNestedDirs(t *testing.T) {
	base := t.TempDir()
	fl, err := openFileLayout(base, []string{"sub", "dir", "file.bin"}, 5, 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer fl.file.Close()

	wantPath := filepath.Join(base, "sub", "dir", "file.bin")
	if fl.path != wantPath {
		t.Errorf("path: got %q, want %q", fl.path, wantPath)
	}
	if fl.torrentOffset != 5 {
		t.Errorf("torrentOffset: got %d, want 5", fl.torrentOffset)
	}
	if fl.size != 20 {
		t.Errorf("size: got %d, want 20", fl.size)
	}

	info, err := os.Stat(wantPath)
	if err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}
	if info.Size() != 20 {
		t.Errorf("file size: got %d, want 20", info.Size())
	}
}

func TestOpenFileLayoutRejectsTraversal(t *testing.T) {
	base := t.TempDir()
	if _, err := openFileLayout(base, []string{"..", "evil.bin"}, 0, 10); err == nil {
		t.Fatal("expected error for path escaping base directory, got nil")
	}
}

func TestNewTorrentLayoutSingleFile(t *testing.T) {
	location := t.TempDir()
	info := metainfo.SingleFileInfo{
		InfoCommon: metainfo.InfoCommon{PieceLength: 10},
		Name:       "movie.mp4",
		Length:     25,
	}

	layout, err := NewTorrentLayout(location, info)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if layout.torrentSize != 25 {
		t.Errorf("torrentSize: got %d, want 25", layout.torrentSize)
	}
	if layout.piecesCount != 3 {
		t.Errorf("piecesCount: got %d, want 3 (ceil(25/10))", layout.piecesCount)
	}
	if len(layout.files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(layout.files))
	}
	if layout.files[0].path != filepath.Join(location, "movie.mp4") {
		t.Errorf("file path: got %q", layout.files[0].path)
	}
}

func TestNewTorrentLayoutMultiFile(t *testing.T) {
	location := t.TempDir()
	info := metainfo.MultiFileInfo{
		InfoCommon: metainfo.InfoCommon{PieceLength: 10},
		Name:       "pack",
		Files: []metainfo.FileEntry{
			{Length: 15, Path: []string{"a.txt"}},
			{Length: 15, Path: []string{"sub", "b.txt"}},
		},
	}

	layout, err := NewTorrentLayout(location, info)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if layout.torrentSize != 30 {
		t.Errorf("torrentSize: got %d, want 30", layout.torrentSize)
	}
	if layout.piecesCount != 3 {
		t.Errorf("piecesCount: got %d, want 3", layout.piecesCount)
	}
	if len(layout.files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(layout.files))
	}
	if layout.files[0].torrentOffset != 0 || layout.files[1].torrentOffset != 15 {
		t.Errorf("unexpected offsets: %d, %d", layout.files[0].torrentOffset, layout.files[1].torrentOffset)
	}
	if _, err := os.Stat(filepath.Join(location, "sub", "b.txt")); err != nil {
		t.Errorf("expected nested file to exist: %v", err)
	}
}

func TestNewTorrentLayoutRejectsPathTraversal(t *testing.T) {
	location := t.TempDir()
	info := metainfo.SingleFileInfo{
		InfoCommon: metainfo.InfoCommon{PieceLength: 10},
		Name:       "../evil.txt",
		Length:     10,
	}

	if _, err := NewTorrentLayout(location, info); err == nil {
		t.Fatal("expected error for path traversal in Name, got nil")
	}
}
