package peer

import "testing"

func TestSize(t *testing.T) {
	b := Bitfield([]byte{0x00, 0x00})
	if b.Size() != 16 {
		t.Errorf("expected 16, got %d", b.Size())
	}
}

func TestExtend(t *testing.T) {
	var b Bitfield

	b.Extend(0)
	if len(b) != 1 {
		t.Errorf("expected 1 byte, got %d", len(b))
	}

	b.Extend(7)
	if len(b) != 1 {
		t.Errorf("expected 1 byte after extending to index 7, got %d", len(b))
	}

	b.Extend(8)
	if len(b) != 2 {
		t.Errorf("expected 2 bytes after extending to index 8, got %d", len(b))
	}

	// Extend to already covered index should be no-op
	prev := len(b)
	b.Extend(8)
	if len(b) != prev {
		t.Errorf("expected no-op extend, got %d", len(b))
	}
}

func TestSetAndHas(t *testing.T) {
	var b Bitfield

	for _, index := range []int{0, 1, 7, 8, 15, 16, 63} {
		b.Set(index)
		if !b.Has(index) {
			t.Errorf("expected bit %d to be set", index)
		}
	}
}

func TestSetDoesNotAffectNeighbors(t *testing.T) {
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

func TestUnset(t *testing.T) {
	var b Bitfield
	b.Set(3)
	b.Unset(3)
	if b.Has(3) {
		t.Error("expected bit 3 to be unset")
	}

	// Unset on out-of-range index should be no-op
	b.Unset(999)
}

func TestComplete(t *testing.T) {
	var b Bitfield
	total := 10

	for i := range total {
		if b.Complete(total) {
			t.Errorf("should not be complete after setting %d pieces", i)
		}
		b.Set(i)
	}

	if !b.Complete(total) {
		t.Error("expected bitfield to be complete")
	}
}

func TestHasOnEmptyBitfield(t *testing.T) {
	var b Bitfield
	b.Extend(7)
	for i := range 8 {
		if b.Has(i) {
			t.Errorf("expected bit %d to be unset on zero bitfield", i)
		}
	}
}
