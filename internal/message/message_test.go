package message

import (
	"bytes"
	"io"
	"testing"
)

// --- Handshake ---

func TestHandshake_RoundTrip(t *testing.T) {
	infoHash := [20]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}
	peerId := [20]byte{20, 19, 18, 17, 16, 15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1}
	original := NewHandshake(infoHash, peerId)

	var buf bytes.Buffer
	n, err := original.WriteTo(&buf)
	if err != nil {
		t.Fatalf("WriteTo error: %v", err)
	}
	if n != 68 {
		t.Errorf("expected 68 bytes written, got %d", n)
	}

	var decoded Handshake
	_, err = decoded.ReadFrom(&buf)
	if err != nil {
		t.Fatalf("ReadFrom error: %v", err)
	}

	if decoded.Pstrlen != original.Pstrlen {
		t.Errorf("Pstrlen: got %d, want %d", decoded.Pstrlen, original.Pstrlen)
	}
	if string(decoded.Pstr) != string(original.Pstr) {
		t.Errorf("Pstr: got %s, want %s", decoded.Pstr, original.Pstr)
	}
	if decoded.InfoHash != original.InfoHash {
		t.Errorf("InfoHash mismatch")
	}
	if decoded.PeerId != original.PeerId {
		t.Errorf("PeerId mismatch")
	}
}

// --- KeepAlive ---

func TestKeepAlive_RoundTrip(t *testing.T) {
	var buf bytes.Buffer
	ka := NewKeepAlive()
	n, err := ka.WriteTo(&buf)
	if err != nil {
		t.Fatalf("WriteTo error: %v", err)
	}
	if n != 4 {
		t.Errorf("expected 4 bytes written, got %d", n)
	}
	if buf.Bytes()[0] != 0 || buf.Bytes()[1] != 0 || buf.Bytes()[2] != 0 || buf.Bytes()[3] != 0 {
		t.Errorf("expected 4 zero bytes, got %v", buf.Bytes())
	}

	var decoded KeepAlive
	_, err = decoded.ReadFrom(&buf)
	if err != nil {
		t.Fatalf("ReadFrom error: %v", err)
	}
}

func TestKeepAlive_ReadFrom_BadLength(t *testing.T) {
	buf := bytes.NewBuffer([]byte{0, 0, 0, 1}) // length=1, not 0
	var ka KeepAlive
	_, err := ka.ReadFrom(buf)
	if err == nil {
		t.Error("expected error for bad length, got nil")
	}
}

// --- Simple messages (Choke, UnChoke, Interested, NotInterested) ---

type simpleMsg interface {
	WriteTo(w interface{ Write([]byte) (int, error) }) (int64, error)
	ReadFrom(r interface{ Read([]byte) (int, error) }) (int64, error)
}

func testSimpleRoundTrip(t *testing.T, name string, msg interface {
	WriteTo(w io.Writer) (int64, error)
	ReadFrom(r io.Reader) (int64, error)
}) {
	t.Helper()
	var buf bytes.Buffer
	n, err := msg.WriteTo(&buf)
	if err != nil {
		t.Fatalf("%s WriteTo error: %v", name, err)
	}
	if n != 5 {
		t.Errorf("%s: expected 5 bytes written, got %d", name, n)
	}
	_, err = msg.ReadFrom(&buf)
	if err != nil {
		t.Fatalf("%s ReadFrom error: %v", name, err)
	}
}

func TestChoke_RoundTrip(t *testing.T)   { c := NewChoke(); testSimpleRoundTrip(t, "Choke", &c) }
func TestUnChoke_RoundTrip(t *testing.T) { u := NewUnchoke(); testSimpleRoundTrip(t, "UnChoke", &u) }
func TestInterested_RoundTrip(t *testing.T) {
	i := NewInterested()
	testSimpleRoundTrip(t, "Interested", &i)
}
func TestNotInterested_RoundTrip(t *testing.T) {
	ni := NewNotInterested()
	testSimpleRoundTrip(t, "NotInterested", &ni)
}

func TestChoke_ReadFrom_BadLength(t *testing.T) {
	buf := bytes.NewBuffer([]byte{0, 0, 0, 2, 0}) // length=2, not 1
	var c Choke
	_, err := c.ReadFrom(buf)
	if err == nil {
		t.Error("expected error for bad length, got nil")
	}
}

func TestChoke_ReadFrom_BadID(t *testing.T) {
	buf := bytes.NewBuffer([]byte{0, 0, 0, 1, 99}) // id=99
	var c Choke
	_, err := c.ReadFrom(buf)
	if err == nil {
		t.Error("expected error for bad id, got nil")
	}
}

// --- Have ---

func TestHave_RoundTrip(t *testing.T) {
	original := NewHave(42)
	var buf bytes.Buffer
	n, err := original.WriteTo(&buf)
	if err != nil {
		t.Fatalf("WriteTo error: %v", err)
	}
	if n != 9 {
		t.Errorf("expected 9 bytes written, got %d", n)
	}

	var decoded Have
	_, err = decoded.ReadFrom(&buf)
	if err != nil {
		t.Fatalf("ReadFrom error: %v", err)
	}
	if decoded.PieceIndex != 42 {
		t.Errorf("PieceIndex: got %d, want 42", decoded.PieceIndex)
	}
}

func TestHave_ReadFrom_BadLength(t *testing.T) {
	buf := bytes.NewBuffer([]byte{0, 0, 0, 4, 4, 0, 0, 0, 42}) // length=4, not 5
	var h Have
	_, err := h.ReadFrom(buf)
	if err == nil {
		t.Error("expected error for bad length, got nil")
	}
}

// --- Bitfield ---

func TestBitfield_RoundTrip(t *testing.T) {
	field := []byte{0b11001010, 0b01110001, 0b10000000}
	original := NewBitfield(field)
	var buf bytes.Buffer
	_, err := original.WriteTo(&buf)
	if err != nil {
		t.Fatalf("WriteTo error: %v", err)
	}

	var decoded Bitfield
	_, err = decoded.ReadFrom(&buf)
	if err != nil {
		t.Fatalf("ReadFrom error: %v", err)
	}
	if !bytes.Equal(decoded.Field, field) {
		t.Errorf("Field: got %v, want %v", decoded.Field, field)
	}
}

func TestBitfield_ReadFrom_BadID(t *testing.T) {
	buf := bytes.NewBuffer([]byte{0, 0, 0, 2, 99, 0xff}) // id=99
	var b Bitfield
	_, err := b.ReadFrom(buf)
	if err == nil {
		t.Error("expected error for bad id, got nil")
	}
}

// --- Request ---

func TestRequest_RoundTrip(t *testing.T) {
	original := NewRequest(1, 0, 16384)
	var buf bytes.Buffer
	n, err := original.WriteTo(&buf)
	if err != nil {
		t.Fatalf("WriteTo error: %v", err)
	}
	if n != 17 {
		t.Errorf("expected 17 bytes written, got %d", n)
	}

	var decoded Request
	_, err = decoded.ReadFrom(&buf)
	if err != nil {
		t.Fatalf("ReadFrom error: %v", err)
	}
	if decoded.Index != 1 || decoded.Begin != 0 || decoded.Length != 16384 {
		t.Errorf("fields mismatch: got {%d %d %d}, want {1 0 16384}", decoded.Index, decoded.Begin, decoded.Length)
	}
}

func TestRequest_ReadFrom_BadLength(t *testing.T) {
	buf := bytes.NewBuffer([]byte{0, 0, 0, 12, 6, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 64, 0}) // length=12
	var rq Request
	_, err := rq.ReadFrom(buf)
	if err == nil {
		t.Error("expected error for bad length, got nil")
	}
}

// --- Piece ---

func TestPiece_RoundTrip(t *testing.T) {
	block := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	original := NewPiece(3, 512, block)
	var buf bytes.Buffer
	n, err := original.WriteTo(&buf)
	if err != nil {
		t.Fatalf("WriteTo error: %v", err)
	}
	if n != int64(13+len(block)) {
		t.Errorf("expected %d bytes written, got %d", 13+len(block), n)
	}

	var decoded Piece
	_, err = decoded.ReadFrom(&buf)
	if err != nil {
		t.Fatalf("ReadFrom error: %v", err)
	}
	if decoded.Index != 3 || decoded.Begin != 512 {
		t.Errorf("fields mismatch: got {%d %d}, want {3 512}", decoded.Index, decoded.Begin)
	}
	if !bytes.Equal(decoded.Block, block) {
		t.Errorf("Block: got %v, want %v", decoded.Block, block)
	}
}

func TestPiece_ReadFrom_BadLength(t *testing.T) {
	buf := bytes.NewBuffer([]byte{0, 0, 0, 8, 7, 0, 0, 0, 1, 0, 0, 0, 0}) // length=8 < 9
	var p Piece
	_, err := p.ReadFrom(buf)
	if err == nil {
		t.Error("expected error for bad length, got nil")
	}
}

// --- Cancel ---

func TestCancel_RoundTrip(t *testing.T) {
	original := NewCancel(1, 0, 16384)
	var buf bytes.Buffer
	n, err := original.WriteTo(&buf)
	if err != nil {
		t.Fatalf("WriteTo error: %v", err)
	}
	if n != 17 {
		t.Errorf("expected 17 bytes written, got %d", n)
	}

	var decoded Cancel
	_, err = decoded.ReadFrom(&buf)
	if err != nil {
		t.Fatalf("ReadFrom error: %v", err)
	}
	if decoded.Index != 1 || decoded.Begin != 0 || decoded.Length != 16384 {
		t.Errorf("fields mismatch: got {%d %d %d}, want {1 0 16384}", decoded.Index, decoded.Begin, decoded.Length)
	}
}

func TestCancel_ReadFrom_BadID(t *testing.T) {
	buf := bytes.NewBuffer([]byte{0, 0, 0, 13, 99, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 64, 0}) // id=99
	var c Cancel
	_, err := c.ReadFrom(buf)
	if err == nil {
		t.Error("expected error for bad id, got nil")
	}
}
