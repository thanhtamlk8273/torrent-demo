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

func TestBitfieldConstructorOwnsInput(t *testing.T) {
	raw := []byte{0x80}
	b := NewBitfield(raw, 8)

	raw[0] = 0
	if !b.Has(0) {
		t.Fatal("mutating constructor input changed the bitfield")
	}
}

func TestBitfieldBytesReturnsSnapshot(t *testing.T) {
	b := NewBitfield([]byte{0x80}, 8)

	bytes := b.Bytes()
	bytes[0] = 0
	if !b.Has(0) {
		t.Fatal("mutating Bytes result changed the bitfield")
	}
}

func TestBitfieldCloneIsIndependent(t *testing.T) {
	original := NewBitfield(nil, 8)
	clone := original.Clone()

	clone.Set(3)
	if original.Has(3) {
		t.Fatal("mutating clone changed the original bitfield")
	}

	original.Set(5)
	if clone.Has(5) {
		t.Fatal("mutating original changed the cloned bitfield")
	}
}

func TestBitfieldConstructorAllocatesExactStorage(t *testing.T) {
	for _, tc := range []struct {
		bits int
		want int
	}{
		{bits: 0, want: 0},
		{bits: 1, want: 1},
		{bits: 7, want: 1},
		{bits: 8, want: 1},
		{bits: 9, want: 2},
		{bits: 16, want: 2},
	} {
		b := NewBitfield(nil, tc.bits)
		if got := len(b.bits); got != tc.want {
			t.Errorf("NewBitfield(nil, %d) allocated %d bytes, want %d", tc.bits, got, tc.want)
		}
	}
}

func TestBitfieldSetAndHas(t *testing.T) {
	b := NewBitfield(nil, 64)

	for _, index := range []int{0, 1, 7, 8, 15, 16, 63} {
		b.Set(index)
		if !b.Has(index) {
			t.Errorf("expected bit %d to be set", index)
		}
	}
}

func TestBitfieldSetDoesNotAffectNeighbors(t *testing.T) {
	b := NewBitfield(nil, 8)
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
	b := NewBitfield(nil, 4)
	b.Set(3)
	b.Unset(3)
	if b.Has(3) {
		t.Error("expected bit 3 to be unset")
	}

	// Unset on out-of-range index should be no-op
	b.Unset(999)
}

func TestBitfieldComplete(t *testing.T) {
	b := NewBitfield(nil, 10)

	// Set a single high bit, leaving gaps below it.
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
	b := NewBitfield(nil, 8)
	for i := range 8 {
		if b.Has(i) {
			t.Errorf("expected bit %d to be unset on zero bitfield", i)
		}
	}
}
