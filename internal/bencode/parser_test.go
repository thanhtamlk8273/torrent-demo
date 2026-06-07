package bencode

import (
	"testing"
)

func newParser(s string) *PacketParser {
	return &PacketParser{data: []byte(s), cursor: 0}
}

func bytePtr(b byte) *byte { return &b }

// --- PacketParser.Peek ---

func TestPeek(t *testing.T) {
	tests := []struct {
		name   string
		data   string
		cursor int
		want   *byte
	}{
		{"empty data returns nil", "", 0, nil},
		{"cursor past end returns nil", "abc", 3, nil},
		{"returns byte at cursor without advancing", "abc", 0, bytePtr('a')},
		{"returns byte mid-data", "abc", 1, bytePtr('b')},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &PacketParser{data: []byte(tt.data), cursor: tt.cursor}
			got := p.Peek()
			if tt.want == nil {
				if got != nil {
					t.Errorf("expected nil, got %v", *got)
				}
				return
			}
			if got == nil || *got != *tt.want {
				t.Errorf("got %v, want %v", got, *tt.want)
			}
			if p.cursor != tt.cursor {
				t.Errorf("Peek must not advance cursor: got %d, want %d", p.cursor, tt.cursor)
			}
		})
	}
}

// --- PacketParser.NextChunk ---

func TestNextChunk(t *testing.T) {
	tests := []struct {
		name       string
		data       string
		cursor     int
		size       int
		wantChunk  string
		wantNil    bool
		wantCursor int
	}{
		{"reads exact size", "hello", 0, 5, "hello", false, 5},
		{"reads partial", "hello", 0, 3, "hel", false, 3},
		{"reads from mid", "hello", 2, 2, "ll", false, 4},
		{"returns nil when too short", "hi", 0, 5, "", true, 0},
		{"returns nil at exact end", "hi", 2, 1, "", true, 2},
		{"zero size returns empty slice", "hi", 0, 0, "", false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &PacketParser{data: []byte(tt.data), cursor: tt.cursor}
			got := p.NextChunk(tt.size)
			if tt.wantNil {
				if got != nil {
					t.Errorf("expected nil, got %q", got)
				}
				return
			}
			if got == nil {
				t.Fatal("unexpected nil")
			}
			if string(got) != tt.wantChunk {
				t.Errorf("chunk: got %q, want %q", got, tt.wantChunk)
			}
			if p.cursor != tt.wantCursor {
				t.Errorf("cursor: got %d, want %d", p.cursor, tt.wantCursor)
			}
		})
	}
}

// --- PacketParser.ReadUntil ---

func TestReadUntil(t *testing.T) {
	tests := []struct {
		name       string
		data       string
		ch         byte
		wantChunk  string
		wantNil    bool
		wantCursor int
	}{
		{"reads single char before delimiter", "3:abc", ':', "3", false, 1},
		{"reads multi-char before delimiter", "10:abc", ':', "10", false, 2},
		{"returns nil when delimiter is first char", ":abc", ':', "", true, 0},
		{"reads until end-marker e", "123e", 'e', "123", false, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newParser(tt.data)
			got := p.ReadUntil(tt.ch)
			if tt.wantNil {
				if got != nil {
					t.Errorf("expected nil, got %q", got)
				}
				return
			}
			if got == nil {
				t.Fatal("unexpected nil")
			}
			if string(got) != tt.wantChunk {
				t.Errorf("chunk: got %q, want %q", got, tt.wantChunk)
			}
			if p.cursor != tt.wantCursor {
				t.Errorf("cursor: got %d, want %d", p.cursor, tt.wantCursor)
			}
		})
	}
}
