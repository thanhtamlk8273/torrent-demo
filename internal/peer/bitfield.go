package peer

type Bitfield []byte

func (b *Bitfield) Size() int {
	return len(*b) * 8
}

func (b *Bitfield) Extend(index int) {
	if index < b.Size() {
		return
	}
	newSize := index/8 + 1
	*b = append(*b, make([]byte, newSize-len(*b))...)
}

func (b *Bitfield) Has(index int) bool {
	if index >= b.Size() {
		return false
	}
	return (*b)[index/8]>>(7-index%8)&1 == 1
}

func (b *Bitfield) Set(index int) {
	b.Extend(index)
	(*b)[index/8] |= 1 << (7 - index%8)
}

func (b *Bitfield) Unset(index int) {
	if index >= b.Size() {
		return
	}
	(*b)[index/8] &^= 1 << (7 - index%8)
}

func (b *Bitfield) Complete(total int) bool {
	for i := range total {
		if !b.Has(i) {
			return false
		}
	}
	return true
}
