package peer

import (
	"bytes"
	"testing"
)

func makeWork(index, size int) PieceWork {
	return PieceWork{Index: index, Size: size}
}

func TestNewPiece(t *testing.T) {
	p := newPiece(makeWork(5, 3*kBlockSize))

	if p.index != 5 {
		t.Errorf("index = %d, want 5", p.index)
	}
	if len(p.blocks) != 3 {
		t.Errorf("blocks len = %d, want 3", len(p.blocks))
	}
	if p.remaining != 3 {
		t.Errorf("remaining = %d, want 3", p.remaining)
	}
}

func TestNewPieceShortFinalBlock(t *testing.T) {
	p := newPiece(makeWork(0, kBlockSize+1000))

	if len(p.blocks) != 2 {
		t.Errorf("blocks len = %d, want 2", len(p.blocks))
	}
	if p.remaining != 2 {
		t.Errorf("remaining = %d, want 2", p.remaining)
	}
}

func TestSaveBlockStores(t *testing.T) {
	p := newPiece(makeWork(0, 2*kBlockSize))
	block := bytes.Repeat([]byte{0xAB}, kBlockSize)

	p.saveBlock(kBlockSize, block)

	if !bytes.Equal(p.blocks[1], block) {
		t.Error("block not stored at its index")
	}
	if p.blocks[0] != nil {
		t.Error("untouched slot should stay nil")
	}
	if p.remaining != 1 {
		t.Errorf("remaining = %d, want 1", p.remaining)
	}
}

func TestSaveBlockDuplicateIgnored(t *testing.T) {
	p := newPiece(makeWork(0, 2*kBlockSize))

	p.saveBlock(0, bytes.Repeat([]byte{0x01}, kBlockSize))
	p.saveBlock(0, bytes.Repeat([]byte{0x02}, kBlockSize)) // dup, must be dropped

	if p.remaining != 1 {
		t.Errorf("remaining = %d after duplicate, want 1", p.remaining)
	}
	if p.blocks[0][0] != 0x01 {
		t.Errorf("duplicate overwrote stored block: got %#x, want 0x01", p.blocks[0][0])
	}
}

// Regression for the index/length guard: saving past the last block must be a no-op.
func TestSaveBlockOutOfRangeDropped(t *testing.T) {
	p := newPiece(makeWork(0, 2*kBlockSize))
	before := p.remaining

	p.saveBlock(uint32(len(p.blocks)*kBlockSize), []byte{1}) // one past the end

	if p.remaining != before {
		t.Errorf("remaining changed on out-of-range begin: %d, want %d", p.remaining, before)
	}
}

func TestPieceComplete(t *testing.T) {
	p := newPiece(makeWork(0, 3*kBlockSize))
	block := bytes.Repeat([]byte{1}, kBlockSize)

	if p.complete() {
		t.Error("empty piece should not be complete")
	}

	p.saveBlock(0, block)
	p.saveBlock(kBlockSize, block)
	if p.complete() {
		t.Error("partial piece should not be complete")
	}

	p.saveBlock(2*kBlockSize, block)
	if !p.complete() {
		t.Error("fully received piece should be complete")
	}
}

func TestAssembleOrdersBlocks(t *testing.T) {
	size := 2*kBlockSize + 500
	p := newPiece(makeWork(0, size))

	b0 := bytes.Repeat([]byte{0xA0}, kBlockSize)
	b1 := bytes.Repeat([]byte{0xB1}, kBlockSize)
	b2 := bytes.Repeat([]byte{0xC2}, 500)

	// save out of order to prove assemble reorders by index
	p.saveBlock(2*kBlockSize, b2)
	p.saveBlock(0, b0)
	p.saveBlock(kBlockSize, b1)

	want := append(append(append([]byte{}, b0...), b1...), b2...)
	got := p.assemble()

	if len(got) != size {
		t.Errorf("assembled len = %d, want %d", len(got), size)
	}
	if !bytes.Equal(got, want) {
		t.Error("assembled bytes not in block-index order")
	}
}

// Regression: short final block must not index received[] out of range.
func TestShortFinalBlock(t *testing.T) {
	size := kBlockSize + 1000
	p := newPiece(makeWork(0, size))

	p.saveBlock(0, bytes.Repeat([]byte{1}, kBlockSize))
	p.saveBlock(kBlockSize, bytes.Repeat([]byte{2}, 1000))

	if !p.complete() {
		t.Error("piece with a short final block should complete")
	}
}
