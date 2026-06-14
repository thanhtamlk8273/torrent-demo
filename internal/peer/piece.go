package peer

const kBlockSize = 16 * 1024

type Block []byte

type Piece struct {
	index        int
	pieceSize    int
	expectedHash [20]byte
	blocks       []Block
	remaining    int
}

func newPiece(w PieceWork) *Piece {
	blockCount := (w.Size + kBlockSize - 1) / kBlockSize
	return &Piece{
		index:        w.Index,
		expectedHash: w.Hash,
		pieceSize:    w.Size,
		blocks:       make([]Block, blockCount),
		remaining:    blockCount,
	}
}

func (p *Piece) saveBlock(begin uint32, block []byte) {
	blockIndex := int(begin) / kBlockSize
	if blockIndex < 0 || blockIndex >= len(p.blocks) {
		return
	}
	if len(p.blocks[blockIndex]) > 0 {
		return
	}
	p.blocks[blockIndex] = block
	p.remaining = p.remaining - 1
}

func (p *Piece) complete() bool {
	return p.remaining == 0
}

func (p *Piece) assemble() []byte {
	data := make([]byte, 0, p.pieceSize)
	for _, b := range p.blocks {
		data = append(data, b...)
	}
	return data
}
