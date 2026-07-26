package peermgr

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
	"torrent-demo/internal/peer"
	"torrent-demo/internal/tracker_client"
)

const (
	kMaxParallelPeers  = 100
	kMaxNbFailure      = 8
	kRetryAgainAfter   = 1 * time.Minute
	kRevisitTime       = 2 * time.Minute
	kBitfieldStaleTime = 5 * time.Minute
)

type Status string

const (
	StatusCandidate = "Candidate"
	StatusActive    = "Active"
	StatusDead      = "Dead"
)

type peerProfile struct {
	endpoint          tracker_client.Peer
	failures          int
	lastBitfield      peer.Bitfield
	bitFieldStaleTime time.Time
}

type waitingPeer struct {
	profile    peerProfile
	eligibleAt time.Time
}

type activePeer struct {
	profile     peerProfile
	conn        *peer.PeerConnection
	revisitTime time.Time

	lastDownloadedBytes uint64
	lastUploadedBytes   uint64
}

type PeerManager struct {
	infoHash [20]byte
	peerID   [20]byte

	waiting map[string]waitingPeer
	active  map[string]activePeer

	scheduler peer.PieceScheduler
	book      peer.PieceBook
	reporter  peer.Reporter
}

func (pm *PeerManager) logf(format string, args ...any) {
	log.Printf("[PeerManager] "+format, args...)
}

func (pm *PeerManager) SetReporter(r peer.Reporter) { pm.reporter = r }

func NewPeerManager(infoHash [20]byte, peerId [20]byte, scheduler peer.PieceScheduler, book peer.PieceBook) *PeerManager {
	return &PeerManager{
		infoHash:  infoHash,
		peerID:    peerId,
		waiting:   make(map[string]waitingPeer),
		active:    make(map[string]activePeer),
		scheduler: scheduler,
		book:      book,
	}
}

func (pm *PeerManager) AddPeer(p tracker_client.Peer) {
	tag := fmt.Sprintf("tcp://%s:%d", p.Ip, p.Port)
	if _, ok := pm.waiting[tag]; ok {
		pm.logf("stage=peer-discovery-ignored peer=%s reason=already-waiting", tag)
		return
	}
	if _, ok := pm.active[tag]; ok {
		pm.logf("stage=peer-discovery-ignored peer=%s reason=already-active", tag)
		return
	}
	localBitfield := pm.scheduler.GetLocalBitfield()
	mp := waitingPeer{
		profile: peerProfile{
			endpoint:          p,
			failures:          0,
			lastBitfield:      *peer.NewBitfield([]byte{}, localBitfield.Size()),
			bitFieldStaleTime: time.Now(),
		},
	}
	pm.waiting[tag] = mp
	fmt.Printf("[PeerManager] AddPeer %s (total=%d)\n", tag, len(pm.waiting)+len(pm.active))
}

func (pm *PeerManager) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	pm.logf("stage=starting waiting=%d active=%d capacity=%d", len(pm.waiting), len(pm.active), kMaxParallelPeers)

	// Add first batch of active peers randomly
	if ok := pm.activeFirstBatch(ctx); !ok {
		pm.logf("stage=initial-activation-failed waiting=%d active=%d", len(pm.waiting), len(pm.active))
		return fmt.Errorf("Unable to start first batch of peers")
	}
	pm.logf("stage=running waiting=%d active=%d", len(pm.waiting), len(pm.active))

	// Event loop
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			pm.logf("stage=stopping reason=%v waiting=%d active=%d", ctx.Err(), len(pm.waiting), len(pm.active))
			wg.Wait()
			return ctx.Err()
		case <-ticker.C:
			pm.evictStallPeers()
			pm.fillActiveSlots(ctx)
		}
	}
}

// Waiting queue methods
func (pm *PeerManager) activeFirstBatch(ctx context.Context) bool {
	// Add first batch of active peers randomly
	waitingPeers := make([]string, 0, len(pm.waiting))
	for k := range pm.waiting {
		waitingPeers = append(waitingPeers, k)
	}
	peerIdx := 0
	for len(pm.active) < kMaxParallelPeers && peerIdx < len(waitingPeers) {
		tag := waitingPeers[peerIdx]
		pm.logf("stage=initial-peer-selected peer=%s waiting=%d active=%d", tag, len(pm.waiting), len(pm.active))
		if _, ok := pm.waiting[tag]; !ok {
			pm.logf("stage=initial-peer-skipped peer=%s", tag)
			continue
		}
		pm.activate(ctx, tag)
		peerIdx++
	}
	return len(pm.active) > 0
}

func (pm *PeerManager) activate(ctx context.Context, tag string) bool {
	waitingPeer := pm.waiting[tag]
	endpoint := waitingPeer.profile.endpoint
	pm.logf("stage=activating peer=%s failures=%d", tag, waitingPeer.profile.failures)
	pc := peer.NewPeerConnection(ctx, tag, endpoint.Ip, endpoint.Port, pm.infoHash, pm.peerID, pm.scheduler)
	if pc == nil {
		pm.logf("stage=activation-failed peer=%s", tag)
		pm.markFailed(tag)
		return false
	}
	if pm.reporter != nil {
		pc.SetReporter(pm.reporter)
	}
	pc.Run()
	pm.active[tag] = activePeer{
		profile:     waitingPeer.profile,
		revisitTime: time.Now().Add(kRevisitTime),
		conn:        pc,
	}
	delete(pm.waiting, tag)
	pm.logf("stage=activated peer=%s waiting=%d active=%d revisit-at=%s", tag, len(pm.waiting), len(pm.active), pm.active[tag].revisitTime.Format(time.RFC3339))
	return true
}

func (pm *PeerManager) markFailed(tag string) {
	waitingPeer := pm.waiting[tag]
	waitingPeer.profile.failures++
	// if waitingPeer.profile.failures > kMaxNbFailure {
	// 	pm.logf("stage=peer-dropped peer=%s failures=%d", tag, waitingPeer.profile.failures)
	// 	delete(pm.waiting, tag)
	// 	return
	// }
	waitingPeer.eligibleAt = time.Now().Add(kRetryAgainAfter)
	pm.waiting[tag] = waitingPeer
	pm.logf("stage=peer-backed-off peer=%s failures=%d eligible-at=%s", tag, waitingPeer.profile.failures, waitingPeer.eligibleAt.Format(time.RFC3339))
}

func (pm *PeerManager) fillActiveSlots(ctx context.Context) {
	now := time.Now()
	bitfield := pm.scheduler.GetLocalBitfield()
	pm.logf("stage=slot-candidate-scan waiting=%d active=%d", len(pm.waiting), len(pm.active))
	if len(pm.active) >= kMaxParallelPeers {
		return
	}
	// Ask piece manager opinion
	tags := pm.selectCandidates(bitfield, now)
	pm.logf("fillActiveSlots %d", len(tags))
	for _, tag := range tags {
		pm.logf("stage=slot-candidate-selected peer=%s waiting=%d active=%d", tag, len(pm.waiting), len(pm.active))
		if !pm.activate(ctx, tag) {
			pm.logf("stage=slot-candidate-selected-failed peer=%s waiting=%d active=%d", tag, len(pm.waiting), len(pm.active))
		}
	}
	// Fill the rest if possible
	if len(pm.active) >= kMaxParallelPeers {
		return
	}
	for tag, waitingPeer := range pm.waiting {
		if now.After(waitingPeer.profile.bitFieldStaleTime) && now.After(waitingPeer.eligibleAt) {
			if !pm.activate(ctx, tag) {
				pm.logf("stage=slot-candidate-selected-failed peer=%s waiting=%d active=%d", tag, len(pm.waiting), len(pm.active))
			}
		}
	}
}

func (pm *PeerManager) selectCandidates(bitfield peer.Bitfield, now time.Time) []string {
	if len(pm.waiting) == 0 {
		return []string{}
	}
	availability := pm.pieceAvailability(bitfield, now)
	pieces := pm.book.GetNextPieces(availability)
	// pm.logf("stage=piece-selection availability=%v pieces=%v", availability, pieces)
	var tags []string
	for _, piece := range pieces {
		for tag, waitingPeer := range pm.waiting {
			if waitingPeer.profile.lastBitfield.Has(piece) && now.After(waitingPeer.eligibleAt) {
				pm.logf("stage=candidate-matched peer=%s piece=%d", tag, piece)
				tags = append(tags, tag)
			}
		}
	}
	return tags
}

func (pm *PeerManager) pieceAvailability(bitfield peer.Bitfield, now time.Time) []int {
	count := make([]int, bitfield.Size())
	for i := range bitfield.Size() {
		for _, waitingPeer := range pm.waiting {
			if waitingPeer.profile.lastBitfield.Has(i) && now.After(waitingPeer.eligibleAt) {
				count[i] += 1
			}
		}
		for _, activePeer := range pm.active {
			activeBitfield := activePeer.conn.GetBitField()
			if activeBitfield.Has(i) {
				count[i] += 1
			}
		}
	}
	return count
}

// Active queue methods
func (pm *PeerManager) evictStallPeers() {
	pm.logf("stage=eviction-target-scan waiting=%d active=%d", len(pm.waiting), len(pm.active))
	for _, target := range pm.identifyEvictTargets() {
		pm.logf("stage=eviction-target-selected peer=%s waiting=%d active=%d", target, len(pm.waiting), len(pm.active))
		pm.evict(target)
	}
}

func (pm *PeerManager) identifyEvictTargets() []string {
	var targets []string
	var uploadOnly []string
	for tag, activePeer := range pm.active {
		conn := activePeer.conn
		if conn == nil {
			pm.logf("stage=eviction-candidate peer=%s reason=nil-connection", tag)
			targets = append(targets, tag)
		}
		if time.Now().Before(activePeer.revisitTime) {
			continue
		}

		downloadChange := conn.GetDownloadedBytes() - activePeer.lastDownloadedBytes
		uploadChange := conn.GetUploadedBytes() - activePeer.lastUploadedBytes

		activePeer.lastDownloadedBytes = activePeer.conn.GetDownloadedBytes()
		activePeer.lastUploadedBytes = activePeer.conn.GetUploadedBytes()
		pm.active[tag] = activePeer

		// pm.logf("stage=eviction-candidate peer=%s downloaded-bytes=%d uploaded-bytes=%d", tag, conn.GetDownloadedBytes(), conn.GetUploadedBytes())
		if downloadChange == 0 && uploadChange == 0 {
			pm.logf("stage=eviction-candidate peer=%s reason=no-traffic", tag)
			targets = append(targets, tag)
		} else if downloadChange == 0 {
			if uploadChange == 0 {
				localBitfield := pm.scheduler.GetLocalBitfield()
				peerBitfield := conn.GetBitField()
				for i := range peerBitfield.Size() {
					if localBitfield.Has(i) && peerBitfield.Has(i) {
						continue
					}
				}
				pm.logf("stage=eviction-candidate peer=%s reason=no-download", tag)
				targets = append(targets, tag)
			} else {
				uploadOnly = append(uploadOnly, tag)
			}
		}
	}
	// if len(pm.active) > 0 && float64(len(uploadOnly))/float64(len(pm.active)) > 0.2 {
	// 	pm.logf("stage=eviction-candidate peer=%s reason=upload-only-budget upload-only=%d active=%d", uploadOnly[0], len(uploadOnly), len(pm.active))
	// 	targets = append(targets, uploadOnly[0])
	// }
	for _, target := range targets {
		activePeer := pm.active[target]
		activePeer.revisitTime = time.Now().Add(kRevisitTime)
		pm.active[target] = activePeer
	}
	return targets
}

func (pm *PeerManager) evict(tag string) {
	activePeer, ok := pm.active[tag]
	if !ok {
		pm.logf("stage=eviction-skipped peer=%s reason=not-active", tag)
		return
	}
	pm.logf("stage=evicting peer=%s downloaded-bytes=%d uploaded-bytes=%d", tag, activePeer.conn.GetDownloadedBytes(), activePeer.conn.GetUploadedBytes())
	activePeer.conn.Shutdown()
	activePeer.profile.failures++ // We need more nuance than this but for now this is good enoguh
	activePeer.profile.lastBitfield = activePeer.conn.GetBitField()
	activePeer.profile.bitFieldStaleTime = time.Now().Add(kBitfieldStaleTime)
	waitingPeer := waitingPeer{
		profile:    activePeer.profile,
		eligibleAt: time.Now().Add(kRetryAgainAfter),
	}
	pm.waiting[tag] = waitingPeer
	delete(pm.active, tag)
	pm.logf("stage=evicted peer=%s waiting=%d active=%d eligible-at=%s", tag, len(pm.waiting), len(pm.active), waitingPeer.eligibleAt.Format(time.RFC3339))
}
