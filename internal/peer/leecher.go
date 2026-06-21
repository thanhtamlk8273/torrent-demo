package peer

import (
	"crypto/sha1"
	"fmt"
	"net"
	"torrent-demo/internal/message"
)

type Leecher struct {
	conn *net.TCPConn

	isChoking    bool
	amInterested bool

	bitfieldSize int
	bitfield     Bitfield

	currentPiece     *Piece
	nextIdx          uint32
	nbParallelChunks int

	scheduler PieceScheduler
	reporter  Reporter
}

func (l *Leecher) init() {
	l.amInterested = true
	sendInterested(l.conn)
}

func (l *Leecher) advance() {
	if l.currentPiece == nil {
		if w, ok := l.scheduler.GetWork(l.bitfield); ok {
			if l.currentPiece != nil {
				panic("p.currentPiece should be NULL when new work comes")
			}
			l.currentPiece = newPiece(w)
			if l.reporter != nil { // debug-only
				l.reporter.PieceStarted(l.conn.RemoteAddr().String(), w.Index, len(l.currentPiece.blocks))
			}
		}
	}
	if !l.isChoking {
		for l.nbParallelChunks < kMaxParallelChunks {
			if ok := l.requestNextChunk(); !ok {
				break
			}
		}
	}
}

func (l *Leecher) processMessage(msg message.Message) {
	switch m := msg.(type) {
	case *message.KeepAlive:
		// liveness tracked above, nothing else to do
	case *message.Choke:
		l.isChoking = true
	case *message.Unchoke:
		l.isChoking = false
	case *message.Bitfield:
		l.bitfield = *NewBitfield(m.Field, l.bitfieldSize)
	case *message.Have:
		l.bitfield.Set(int(m.PieceIndex))
	case *message.Piece:
		if l.currentPiece != nil {
			if m.Index != uint32(l.currentPiece.index) {
				// stale block for a piece we've moved on from; dropped silently
			} else if l.currentPiece.saveBlock(m.Begin, m.Block) {
				if l.reporter != nil { // debug-only
					l.reporter.BlockSaved(l.conn.RemoteAddr().String(), int(m.Begin)/kBlockSize, len(m.Block))
				}
			}
			if l.currentPiece.complete() {
				data := l.currentPiece.assemble()
				hash := sha1.Sum(data)
				if hash != l.currentPiece.expectedHash {
					if l.reporter != nil { // debug-only
						l.reporter.PieceFailed(l.conn.RemoteAddr().String(), fmt.Sprintf("piece %d hash mismatch", l.currentPiece.index))
					}
					l.scheduler.UnclaimPiece(l.currentPiece.index)
				} else {
					l.scheduler.CompletePiece(l.currentPiece.index, data)
					if l.reporter != nil { // debug-only
						l.reporter.PieceCompleted(l.conn.RemoteAddr().String(), l.currentPiece.index)
					}
				}
				l.currentPiece = nil
				l.nextIdx = 0
			}
			if l.nbParallelChunks > 0 {
				l.nbParallelChunks -= 1
			}
		}
	}
}

func (l *Leecher) requestNextChunk() bool {
	if l.currentPiece == nil || l.nbParallelChunks >= kMaxParallelChunks {
		return false
	}
	if l.nextIdx*kBlockSize >= uint32(l.currentPiece.pieceSize) {
		return false
	}
	chunkSize := uint32(kBlockSize)
	if (l.nextIdx+1)*kBlockSize > uint32(l.currentPiece.pieceSize) {
		chunkSize = uint32(l.currentPiece.pieceSize) - l.nextIdx*kBlockSize
	}
	sendRequest(l.conn, uint32(l.currentPiece.index), l.nextIdx*kBlockSize, chunkSize)
	l.nbParallelChunks += 1
	l.nextIdx += 1
	return true
}

func (l *Leecher) shutdown() {
	if l.currentPiece != nil {
		if l.reporter != nil { // debug-only
			l.reporter.PieceFailed(l.conn.RemoteAddr().String(), fmt.Sprintf("piece %d incomplete", l.currentPiece.index))
		}
		l.scheduler.UnclaimPiece(l.currentPiece.index)
		l.currentPiece = nil
	}
}
