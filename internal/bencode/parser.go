package bencode

type PacketParser struct {
	data   []byte
	cursor int
}

func (p *PacketParser) Peek() *byte {
	if p.cursor >= len(p.data) {
		return nil
	}
	return &p.data[p.cursor]
}

func (p *PacketParser) NextChunk(size int) []byte {
	if p.cursor+size > len(p.data) {
		return nil
	}
	chunk := p.data[p.cursor : p.cursor+size]
	p.cursor += size
	return chunk
}

func (p *PacketParser) ReadUntil(ch byte) []byte {
	startIdx := p.cursor
	for p.cursor < len(p.data) && p.data[p.cursor] != ch {
		p.cursor += 1
	}
	if startIdx == p.cursor {
		return nil
	}
	chunk := p.data[startIdx:p.cursor]
	return chunk
}
