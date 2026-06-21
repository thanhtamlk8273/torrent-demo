package peer

import (
	"bytes"
	"testing"
)

func TestBitfieldSize(t *testing.T) {
	b := NewBitfield([]byte{0x00, 0x00}, 16)
	if b.Size() != 16 {
		t.Errorf("expected 16, got %d", b.Size())
	}
}

func TestBitfieldNewAndBytes(t *testing.T) {
	raw := []byte{0b10100000}
	b := NewBitfield(raw, 3)

	if b.Size() != 3 {
		t.Errorf("expected size 3, got %d", b.Size())
	}
	if !b.Has(0) || b.Has(1) || !b.Has(2) {
		t.Errorf("unexpected bits: Has(0)=%v Has(1)=%v Has(2)=%v", b.Has(0), b.Has(1), b.Has(2))
	}
	// Index past n is out of the valid range even though the byte has spare bits.
	if b.Has(3) {
		t.Error("expected index 3 to be out of range")
	}
	if !bytes.Equal(b.Bytes(), raw) {
		t.Errorf("Bytes() = %v, want %v", b.Bytes(), raw)
	}
}

func TestBitfieldExtend(t *testing.T) {
	var b Bitfield

	b.Extend(0)
	if len(b.bits) != 1 {
		t.Errorf("expected 1 byte, got %d", len(b.bits))
	}

	b.Extend(7)
	if len(b.bits) != 1 {
		t.Errorf("expected 1 byte after extending to index 7, got %d", len(b.bits))
	}

	b.Extend(8)
	if len(b.bits) != 2 {
		t.Errorf("expected 2 bytes after extending to index 8, got %d", len(b.bits))
	}

	// Extend to already covered index should be no-op
	prev := len(b.bits)
	b.Extend(8)
	if len(b.bits) != prev {
		t.Errorf("expected no-op extend, got %d", len(b.bits))
	}
}

func TestBitfieldSetAndHas(t *testing.T) {
	var b Bitfield

	for _, index := range []int{0, 1, 7, 8, 15, 16, 63} {
		b.Set(index)
		if !b.Has(index) {
			t.Errorf("expected bit %d to be set", index)
		}
	}
}

func TestBitfieldSetDoesNotAffectNeighbors(t *testing.T) {
	var b Bitfield
	b.Set(4)

	for i := range b.Size() {
		if i == 4 {
			continue
		}
		if b.Has(i) {
			t.Errorf("expected bit %d to be unset", i)
		}
	}
}

func TestBitfieldUnset(t *testing.T) {
	var b Bitfield
	b.Set(3)
	b.Unset(3)
	if b.Has(3) {
		t.Error("expected bit 3 to be unset")
	}

	// Unset on out-of-range index should be no-op
	b.Unset(999)
}

func TestBitfieldComplete(t *testing.T) {
	var b Bitfield

	// Grow the domain to 10 with a single high bit, leaving gaps below it.
	b.Set(9)
	if b.Complete() {
		t.Error("should not be complete with bits 0-8 unset")
	}

	for i := range 10 {
		b.Set(i)
	}
	if !b.Complete() {
		t.Error("expected bitfield to be complete")
	}
}

func TestBitfieldHasOnEmptyBitfield(t *testing.T) {
	var b Bitfield
	b.Extend(7)
	for i := range 8 {
		if b.Has(i) {
			t.Errorf("expected bit %d to be unset on zero bitfield", i)
		}
	}
}
