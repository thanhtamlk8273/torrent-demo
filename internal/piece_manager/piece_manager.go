package piecemanager

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"slices"
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
	mu            sync.RWMutex
	location      string
	pieceStates   []PieceState
	metaInfo      metainfo.MetaInfo
	layout        *TorrentLayout
	localBitfield peer.Bitfield
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
	pm.localBitfield = field

	return pm, nil
}

func (pm *PieceManager) GetLocalBitfield() peer.Bitfield {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.localBitfield.Clone()
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
	pm.localBitfield.Set(index)
	return nil
}

func (pm *PieceManager) UnclaimPiece(index int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if pm.pieceStates[index] == Claimed {
		pm.pieceStates[index] = Unclaimed
	}
}

func (pm *PieceManager) GetBlock(index, begin, length int) ([]byte, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if pm.pieceStates[index] != Done {
		return []byte{}, fmt.Errorf("Do not have requested piece %d", index)
	}
	piece, err := pm.layout.readPiece(index)
	if err != nil {
		return []byte{}, fmt.Errorf("Do not have requested piece %d", index)
	}
	if begin < 0 || begin+length > len(piece) {
		return []byte{}, fmt.Errorf("GetBlock: out of range index=%d begin=%d length=%d", index, begin, length)
	}
	return piece[begin : begin+length], nil
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
	pieceCount := int(pm.layout.piecesCount)
	field := peer.NewBitfield(nil, pieceCount)
	pieces := pm.metaInfo.Info.Common().Pieces
	for idx := range pm.layout.piecesCount {
		pieceData, err := pm.layout.readPiece(int(idx))
		if err != nil {
			return peer.Bitfield{}, fmt.Errorf("verifyTorrent: readPiece bad idx %d", idx)
		}
		expected, ok := getHash(pieces, idx)
		if !ok {
			return peer.Bitfield{}, fmt.Errorf("verifyTorrent: unable to find expectedHash %d", idx)
		}
		hash := sha1.Sum(pieceData)
		if !bytes.Equal(hash[:], expected[:]) {
			continue
		}
		field.Set(int(idx))
	}
	return *field, nil
}

func (pm *PieceManager) GetNextPieces(availability []int) []int {
	ranked := []struct {
		idx   int
		count int
	}{}
	for idx, count := range availability {
		if pm.pieceStates[idx] == Unclaimed {
			ranked = append(ranked, struct {
				idx   int
				count int
			}{idx, count})
		}
	}
	slices.SortFunc(ranked, func(a, b struct{ idx, count int }) int {
		return a.count - b.count // ascending: rarest first
	})
	indices := make([]int, 0)
	for _, r := range ranked {
		if r.count > 0 {
			indices = append(indices, r.idx)
		}
	}
	return indices
}

func getHash(pieces []byte, idx int) ([20]byte, bool) {
	if idx*20+20 > len(pieces) {
		return [20]byte{}, false
	}
	return [20]byte(pieces[idx*20 : idx*20+20]), true
}
