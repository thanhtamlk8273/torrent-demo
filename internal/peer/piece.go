package peer

import (
	"fmt"
	"torrent-demo/internal/mathutil"
)

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
	blockCount := mathutil.CeilDiv(w.Size, kBlockSize)
	return &Piece{
		index:        w.Index,
		expectedHash: w.Hash,
		pieceSize:    w.Size,
		blocks:       make([]Block, blockCount),
		remaining:    blockCount,
	}
}

func (p *Piece) saveBlock(begin uint32, block []byte) bool {
	blockIndex := int(begin) / kBlockSize
	if blockIndex < 0 || blockIndex >= len(p.blocks) {
		fmt.Printf("Bad idx for %d, %d, %d\n", p.index, begin, len(block))
		return false
	}
	if len(p.blocks[blockIndex]) > 0 {
		fmt.Printf("Duplication for %d, %d, %d, %d\n", p.index, begin, len(block), len(p.blocks[blockIndex]))
		return false
	}
	p.blocks[blockIndex] = block
	p.remaining = p.remaining - 1
	return true
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
