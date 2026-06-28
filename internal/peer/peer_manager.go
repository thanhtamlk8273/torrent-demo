package peer

import (
	"context"
	"fmt"
	"sync"
	"time"
	"torrent-demo/internal/tracker_client"
)

const kMaxParallelPeers = 8
const kMaxNbFailure = 8
const kRetryAgainAfter = 1 * time.Minute
const kGracePeriod = 1 * time.Minute

type PieceBook interface {
	GetNextPieces(availability []int) []int
}

type EventKind int

const (
	BitfieldEvent EventKind = iota
	HaveEvent
	ConnectionClosed
)

type PeerEvent struct {
	tag     string
	kind    EventKind
	payload any
}

type PeerStatus string

const (
	StatusIdle       PeerStatus = "Idle"
	StatusConnecting PeerStatus = "Connecting"
	StatusActive     PeerStatus = "Active"
	StatusFailed     PeerStatus = "Failed"
	StatusLater      PeerStatus = "Later"
)

type ManagedPeer struct {
	peer             tracker_client.Peer
	conn             *PeerConnection
	status           PeerStatus
	nextRetry        time.Time
	afterGracePeriod time.Time
	nbFailure        int
	bitfield         Bitfield
}

type PeerManager struct {
	infoHash       [20]byte
	peerID         [20]byte
	managedPeers   map[string]*ManagedPeer
	nbRunningPeers int

	peerCh    chan PeerEvent
	scheduler PieceScheduler
	book      PieceBook
	reporter  Reporter
}

func (pm *PeerManager) SetReporter(r Reporter) { pm.reporter = r }

func NewPeerManager(infoHash [20]byte, peerId [20]byte, scheduler PieceScheduler, book PieceBook) *PeerManager {
	return &PeerManager{
		infoHash:     infoHash,
		peerID:       peerId,
		managedPeers: make(map[string]*ManagedPeer),
		peerCh:       make(chan PeerEvent, 100),
		scheduler:    scheduler,
		book:         book,
	}
}

func (pm *PeerManager) AddPeer(peer tracker_client.Peer) {
	tag := fmt.Sprintf("tcp://%s:%d", peer.Ip, peer.Port)
	mp := ManagedPeer{
		peer:             peer,
		conn:             nil,
		status:           StatusIdle,
		nextRetry:        time.Now(),
		afterGracePeriod: time.Now(),
		nbFailure:        0,
	}
	pm.managedPeers[tag] = &mp
}

func (pm *PeerManager) Run(ctx context.Context) {
	var wg sync.WaitGroup
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case e := <-pm.peerCh:
			pm.handleEvent(e)
		case <-ticker.C:
			for tag, mp := range pm.managedPeers {
				switch {
				case canStart(mp):
					if pc, err := pm.startPeerConnection(tag, mp); err == nil {
						mp.conn = pc
						mp.status = StatusConnecting
						mp.nbFailure = 0
					} else {
						mp.status = StatusFailed
						mp.nbFailure += 1
						mp.nextRetry = time.Now().Add(kRetryAgainAfter)
					}
				case isSlow(mp) || isStuckInConnecting(mp):
					resetManagedPeer(mp)
				case shouldDelete(mp):
					delete(pm.managedPeers, tag)
				}
			}

			// WORKING ON
			pool := pm.getPool()
			availability := make([]int, len(pool))
			for idx, peers := range pool {
				availability[idx] = len(peers)
			}
			for _, pieceIdx := range pm.book.GetNextPieces(availability) {
				for _, mp := range pool[pieceIdx] {
					if isReady(mp) {
						wg.Go(func() {
							defer wg.Done()
							mp.conn.Run(ctx)
						})
						mp.status = StatusActive
						break
					}
				}
				pm.nbRunningPeers += 1
				if pm.nbRunningPeers >= kMaxParallelPeers {
					break
				}
			}
		}
	}
}

func shouldDelete(mp *ManagedPeer) bool {
	status := mp.status
	return status == StatusFailed && mp.nbFailure > kMaxNbFailure
}

func canStart(mp *ManagedPeer) bool {
	status := mp.status
	return status == StatusIdle || (((status == StatusFailed && !shouldDelete(mp)) || status == StatusLater) && time.Now().After(mp.nextRetry))
}

func isStuckInConnecting(mp *ManagedPeer) bool {
	return mp.status == StatusConnecting && time.Now().After(mp.afterGracePeriod)
}

func isReady(mp *ManagedPeer) bool {
	return mp.status == StatusConnecting && mp.conn.connected
}

func isSlow(mp *ManagedPeer) bool {
	return mp.status == StatusActive && time.Now().After(mp.afterGracePeriod) && mp.conn.getDownloadSpeed() == 0 && mp.conn.getUploadSpeed() == 0
}

func isActive(mp *ManagedPeer) bool {
	return mp.conn != nil && mp.status == StatusActive
}

func (pm *PeerManager) startPeerConnection(tag string, mp *ManagedPeer) (*PeerConnection, error) {
	if !canStart(mp) {
		return nil, fmt.Errorf("%s:%d eligible to start", mp.peer.Ip, mp.peer.Port)
	}
	pc, err := NewPeerConnection(tag, mp.peer.Ip, mp.peer.Port, pm.infoHash, pm.peerID, pm.peerCh, pm.scheduler)
	if err != nil {
		return nil, fmt.Errorf("%s:%d unable to create client", mp.peer.Ip, mp.peer.Port)
	}
	err = pc.Connect()
	if err != nil {
		return nil, fmt.Errorf("%s:%d unable to connect to", mp.peer.Ip, mp.peer.Port)
	}
	if pm.reporter != nil {
		pc.SetReporter(pm.reporter)
	}
	return &pc, nil
}

func (pm *PeerManager) handleEvent(event PeerEvent) {
	mPeer, ok := pm.managedPeers[event.tag]
	if !ok {
		return
	}
	switch event.kind {
	case ConnectionClosed:
		resetManagedPeer(mPeer)
		pm.nbRunningPeers -= 1
	case BitfieldEvent:
		mPeer.bitfield = event.payload.(Bitfield)
	case HaveEvent:
		if event.payload.(int) >= mPeer.bitfield.Size() {
			return
		}
		mPeer.bitfield.Set(event.payload.(int))
	}
}

func (pm *PeerManager) getPool() [][]*ManagedPeer {
	var pool [][]*ManagedPeer
	for _, mPeer := range pm.managedPeers {
		if !isReady(mPeer) && !isActive(mPeer) {
			continue
		}
		if len(pool) < mPeer.bitfield.Size() {
			pool = make([][]*ManagedPeer, mPeer.bitfield.Size())
		}
		for idx := range mPeer.bitfield.Size() {
			if mPeer.bitfield.Has(idx) {
				pool[idx] = append(pool[idx], mPeer)
			}
		}
	}
	return pool
}

func resetManagedPeer(mp *ManagedPeer) {
	mp.conn = nil
	mp.status = StatusLater
	mp.nextRetry = time.Now().Add(kRetryAgainAfter)
	mp.afterGracePeriod = time.Now().Add(kGracePeriod)
	mp.nbFailure = 0
	mp.bitfield = Bitfield{}
}
