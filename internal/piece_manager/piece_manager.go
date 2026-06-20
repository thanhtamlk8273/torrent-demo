package piecemanager

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"sync"
	"torrent-demo/internal/metainfo"
	"torrent-demo/internal/peer"
)

type PieceState uint8

const (
	Unclaimed PieceState = iota
	Claimed
	Done
)

type PieceManager struct {
	mu          sync.RWMutex
	location    string
	pieceStates []PieceState
	metaInfo    metainfo.MetaInfo
	layout      *TorrentLayout
}

func NewPieceManager(location string, mInfo metainfo.MetaInfo) (*PieceManager, error) {
	layout, err := NewTorrentLayout(location, mInfo.Info.Info)
	if err != nil {
		return nil, err
	}
	pm := &PieceManager{
		mu:          sync.RWMutex{},
		location:    location,
		pieceStates: make([]PieceState, layout.piecesCount),
		metaInfo:    mInfo,
		layout:      layout,
	}

	field, err := pm.verifyTorrent()
	if err != nil {
		return nil, err
	}
	for idx := range pm.pieceStates {
		if field.Has(idx) {
			pm.pieceStates[idx] = Done
		}
	}

	return pm, nil
}

func (pm *PieceManager) GetWork(field peer.Bitfield) (peer.PieceWork, bool) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pieceCount := pm.layout.piecesCount
	for idx := range pieceCount {
		if pm.pieceStates[idx] == Unclaimed && field.Has(idx) {
			expected, ok := getHash(pm.metaInfo.Info.Common().Pieces, idx)
			if !ok {
				panic("GetWork: unable to find hash")
			}
			pieceSize := pm.layout.pieceSizeAt(idx)
			pm.pieceStates[idx] = Claimed
			return peer.PieceWork{
				Index: idx,
				Size:  int(pieceSize),
				Hash:  expected,
			}, true
		}
	}
	return peer.PieceWork{}, false
}

func (pm *PieceManager) CompletePiece(index int, data []byte) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if err := pm.layout.writePiece(index, data); err != nil {
		// A failed write must never count as Done. Revert a claimed piece so
		// it can be re-fetched; leave any other state untouched.
		if pm.pieceStates[index] == Claimed {
			pm.pieceStates[index] = Unclaimed
		}
		return err
	}
	pm.pieceStates[index] = Done
	return nil
}

func (pm *PieceManager) UnclaimPiece(index int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if pm.pieceStates[index] == Claimed {
		pm.pieceStates[index] = Unclaimed
	}
}

func (pm *PieceManager) Progress() (done, total int) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	for _, s := range pm.pieceStates {
		if s == Done {
			done++
		}
	}
	return done, len(pm.pieceStates)
}

func (pm *PieceManager) verifyTorrent() (peer.Bitfield, error) {
	field := peer.Bitfield{}
	pieces := pm.metaInfo.Info.Common().Pieces
	for idx := range pm.layout.piecesCount {
		pieceData, err := pm.layout.readPiece(int(idx))
		if err != nil {
			return nil, fmt.Errorf("verifyTorrent: readPiece bad idx %d", idx)
		}
		expected, ok := getHash(pieces, idx)
		if !ok {
			return nil, fmt.Errorf("verifyTorrent: unable to find expectedHash %d", idx)
		}
		hash := sha1.Sum(pieceData)
		if !bytes.Equal(hash[:], expected[:]) {
			continue
		}
		field.Set(int(idx))
	}
	return field, nil
}

func getHash(pieces []byte, idx int) ([20]byte, bool) {
	if idx*20+20 > len(pieces) {
		return [20]byte{}, false
	}
	return [20]byte(pieces[idx*20 : idx*20+20]), true
}
