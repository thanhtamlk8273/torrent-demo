package peer

import "torrent-demo/internal/mathutil"

type Bitfield struct {
	bits []byte
	n    int
}

func NewBitfield(bits []byte, n int) *Bitfield {
	if n < 0 {
		panic("negative bitfield size")
	}
	size := mathutil.CeilDiv(n, 8)
	ownedBits := make([]byte, size)
	copy(ownedBits, bits)
	return &Bitfield{ownedBits, n}
}

// Clone returns an independent snapshot of the bitfield.
func (b Bitfield) Clone() Bitfield {
	clonedBits := make([]byte, len(b.bits))
	copy(clonedBits, b.bits)
	return Bitfield{bits: clonedBits, n: b.n}
}

// Bytes returns an independent byte snapshot for serialization.
func (b *Bitfield) Bytes() []byte {
	bytes := make([]byte, len(b.bits))
	copy(bytes, b.bits)
	return bytes
}

func (b *Bitfield) Size() int {
	return b.n
}

func (b *Bitfield) Has(index int) bool {
	if index >= b.Size() {
		return false
	}
	return b.bits[index/8]>>(7-index%8)&1 == 1
}

func (b *Bitfield) Set(index int) {
	b.bits[index/8] |= 1 << (7 - index%8)
}

func (b *Bitfield) Unset(index int) {
	if index >= b.Size() {
		return
	}
	b.bits[index/8] &^= 1 << (7 - index%8)
}

func (b *Bitfield) Complete() bool {
	for i := range b.Size() {
		if !b.Has(i) {
			return false
		}
	}
	return true
}

func (b *Bitfield) Any() bool {
	for i := range b.Size() {
		if b.Has(i) {
			return true
		}
	}
	return false
}

func (b *Bitfield) Equal(other Bitfield) bool {
	if b.Size() != other.Size() {
		return false
	}
	for idx := range b.Size() {
		if b.Has(idx) != other.Has(idx) {
			return false
		}
	}
	return true
}
