package peer

type Bitfield struct {
	bits []byte
	n    int
}

func NewBitfield(bits []byte, n int) *Bitfield {
	return &Bitfield{bits, n}
}

// Bytes returns the underlying bytes for serialization onto the wire.
func (b *Bitfield) Bytes() []byte {
	return b.bits
}

func (b *Bitfield) Size() int {
	return b.n
}

func (b *Bitfield) Extend(index int) {
	if index < b.Size() {
		return
	}
	newSize := index/8 + 1
	b.bits = append(b.bits, make([]byte, newSize-len(b.bits))...)
	b.n = index + 1
}

func (b *Bitfield) Has(index int) bool {
	if index >= b.Size() {
		return false
	}
	return b.bits[index/8]>>(7-index%8)&1 == 1
}

func (b *Bitfield) Set(index int) {
	b.Extend(index)
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
