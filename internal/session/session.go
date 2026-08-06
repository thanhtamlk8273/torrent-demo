package session

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"torrent-demo/internal/metainfo"
	peermgr "torrent-demo/internal/peer_manager"
	piecemanager "torrent-demo/internal/piece_manager"
	"torrent-demo/internal/tracker_client"
)

type Options struct {
	DownloadRoot string
	Port         int
}

const peerRefreshInterval = 3 * time.Minute

// sessionLogf tags a log line with the session id. Usable from
// readTorrentFile, which runs before the Session is constructed. Mirrors the
// logf pattern in internal/peer/peer.go.
func sessionLogf(id, format string, args ...any) {
	log.Printf("[session %s] "+format, append([]any{id}, args...)...)
}

func (s *Session) logf(format string, args ...any) {
	sessionLogf(s.id, format, args...)
}

// Status is the lifecycle state of a session.
type Status string

const (
	StatusPending   Status = "Pending"
	StatusRunning   Status = "Running"
	StatusStopped   Status = "Stopped"
	StatusCompleted Status = "Completed"
	StatusErrored   Status = "Errored"
)

// SessionSummary is a plain-data snapshot of a session, safe to call at any
// lifecycle stage. This is the shape the GUI renders.
type SessionSummary struct {
	ID           string
	Name         string
	Status       Status
	DonePieces   int
	TotalPieces  int
	TotalBytes   int64
	DownloadRate float64
	UploadRate   float64
	ActivePeers  int
	WaitingPeers int
	Error        string

	Seeds           int
	MaxConnections  int
	TotalDownloaded uint64
	TotalUploaded   uint64
	TrackerSeeds    int
	TrackerLeechers int
	SavePath        string
	InfoHash        string
	PieceLength     int
	AddedAt         int64
	StartedAt       int64
	CompletedAt     int64
	Comment         string
	CreatedBy       string
	CreationDate    int64
	Private         bool
}

type Session struct {
	id           string
	name         string
	totalBytes   int64
	downloadRoot string
	torrentPath  string
	port         int
	peerID       [20]byte
	meta         *metainfo.MetaInfo
	pieces       *piecemanager.PieceManager
	peers        *peermgr.PeerManager

	// Immutable: written in NewTorrentSession before the *Session is
	// published into SessionManager.sessions, so no lock is needed — exactly
	// like name / totalBytes.
	location     string
	infoHash     string
	pieceLength  int
	pieceCount   int
	comment      string
	createdBy    string
	creationDate int64
	private      bool
	addedAt      int64

	// statusMu guards the status/rate block below. Five plain scalar fields,
	// no nested calls — nothing like PeerManager's tangled map methods.
	statusMu     sync.RWMutex
	status       Status
	lastErr      error
	downloadRate float64
	uploadRate   float64
	activePeers  int
	waitingPeers int

	// Mutable lifetime stats, also guarded by statusMu: read by Summary()
	// from the ticker goroutine, written by progressWatcher / findPeers.
	seeds           int
	totalDownloaded uint64
	totalUploaded   uint64
	trackerSeeds    int
	trackerLeechers int
	startedAt       int64
	completedAt     int64

	cancel        context.CancelFunc
	stopOnce      sync.Once
	done          chan struct{}
	nextRefreshes map[string]time.Time
	announced     map[string]bool
}

// NewTorrentSession parses the torrent file eagerly so the session can appear
// in a list (name, size, piece count) before Start is ever called.
func NewTorrentSession(id, torrentPath string, options Options) (*Session, error) {
	var peerID [20]byte
	copy(peerID[:], "-TD0001-123456789012")

	meta, err := readTorrentFile(id, torrentPath)
	if err != nil {
		return nil, err
	}

	name := getTorrentName(&meta)
	common := infoCommon(&meta)
	return &Session{
		id:            id,
		name:          name,
		totalBytes:    computeTotalBytes(&meta),
		downloadRoot:  options.DownloadRoot,
		torrentPath:   torrentPath,
		location:      filepath.Join(options.DownloadRoot, name),
		infoHash:      hex.EncodeToString(meta.InfoHash[:]),
		pieceLength:   common.PieceLength,
		pieceCount:    len(common.Pieces) / 20,
		comment:       meta.Comment,
		createdBy:     meta.CreatedBy,
		creationDate:  int64(meta.CreationDate),
		private:       common.Private != 0,
		addedAt:       time.Now().Unix(),
		port:          options.Port,
		peerID:        peerID,
		meta:          &meta,
		status:        StatusPending,
		done:          make(chan struct{}),
		nextRefreshes: make(map[string]time.Time),
		announced:     make(map[string]bool),
	}, nil
}

func (s *Session) Start(ctx context.Context) error {
	for _, tracker := range getTrackers(s.meta) {
		s.nextRefreshes[tracker] = time.Now()
	}
	if len(s.nextRefreshes) == 0 {
		return fmt.Errorf("no trackers found")
	}

	// location is set in the constructor (immutable) so Summary() can show a
	// real save path while the torrent is still Pending, as qBittorrent does.
	if err := os.MkdirAll(s.location, 0755); err != nil {
		return fmt.Errorf("failed to create download dir: %w", err)
	}

	pieces, err := piecemanager.NewPieceManager(s.location, *s.meta)
	if err != nil {
		return fmt.Errorf("failed to init piece manager: %w", err)
	}
	s.pieces = pieces

	s.peers = peermgr.NewPeerManager(s.meta.InfoHash, s.peerID, s.pieces, s.pieces)

	// Populate the waiting room before PeerManager.Run, which errors out if it
	// can't activate a first batch from an empty waiting room.
	if err := s.refreshPeers(); err != nil {
		s.statusMu.Lock()
		s.status = StatusErrored
		s.lastErr = fmt.Errorf("failed to find peers: %w", err)
		s.statusMu.Unlock()
		return fmt.Errorf("failed to find peers: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	s.statusMu.Lock()
	s.status = StatusRunning
	s.startedAt = time.Now().Unix()
	s.statusMu.Unlock()

	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		s.peers.Run(ctx)
	}()
	go func() {
		defer wg.Done()
		s.refreshPeersLoop(ctx)
	}()
	go func() {
		defer wg.Done()
		s.progressWatcher(ctx)
	}()
	go func() {
		wg.Wait()
		close(s.done)
	}()

	return nil
}

// progressWatcher samples peer byte counters once a second, derives
// download/upload rates, counts active/waiting peers, and detects completion
// independently of any caller. Each session detects its own completion.
func (s *Session) progressWatcher(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var lastDown, lastUp uint64
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Lifetime byte totals are monotonic (evictions are retired, never
			// dropped), so rates derived from them never flicker to zero when
			// a peer is evicted mid-sample.
			down, up := s.peers.TotalTransferred()
			downRate := sampleRate(down, lastDown)
			upRate := sampleRate(up, lastUp)
			lastDown, lastUp = down, up

			active, waiting, seeds := 0, 0, 0
			for _, ps := range s.peers.Snapshot() {
				if ps.Active {
					active++
					// A peer whose bitfield is fully set is a seed. Waiting
					// peers are excluded deliberately — their lastBitfield is
					// stale. Peers that haven't sent a bitfield yet read 0 and
					// simply aren't counted.
					if ps.BitfieldFraction >= 1.0 {
						seeds++
					}
				} else {
					waiting++
				}
			}

			done, total := s.pieces.Progress()

			s.statusMu.Lock()
			s.downloadRate = downRate
			s.uploadRate = upRate
			s.activePeers = active
			s.waitingPeers = waiting
			s.seeds = seeds
			s.totalDownloaded = down
			s.totalUploaded = up
			s.statusMu.Unlock()

			// sampleRate clamps at zero if a total ever decreases between
			// samples.
			if total > 0 && done == total {
				s.statusMu.Lock()
				s.status = StatusCompleted
				s.completedAt = time.Now().Unix()
				s.statusMu.Unlock()
				s.Stop()
				return
			}
		}
	}
}

// sampleRate returns bytes/sec for a 1-second sample, clamped at zero when the
// counter decreased between samples (a peer disconnected).
func sampleRate(cur, prev uint64) float64 {
	if cur <= prev {
		return 0
	}
	return float64(cur - prev)
}

// Progress returns the number of verified pieces out of the total. Safe to
// call before Start returns 0, 0.
func (s *Session) Progress() (done, total int) {
	if s.pieces == nil {
		return 0, 0
	}
	return s.pieces.Progress()
}

// Stop cancels the session's background work. Safe to call multiple times.
func (s *Session) Stop() {
	s.stopOnce.Do(func() {
		s.statusMu.Lock()
		if s.status != StatusCompleted && s.status != StatusErrored {
			s.status = StatusStopped
		}
		s.statusMu.Unlock()
		if s.cancel != nil {
			s.cancel()
		}
	})
}

// Done returns a channel that closes once all background work has exited.
func (s *Session) Done() <-chan struct{} {
	return s.done
}

// Wait blocks until the session's background work has exited.
func (s *Session) Wait() {
	<-s.done
}

// ID returns the session's stable identifier.
func (s *Session) ID() string { return s.id }

// Status returns the session's lifecycle status.
func (s *Session) Status() Status {
	s.statusMu.RLock()
	defer s.statusMu.RUnlock()
	return s.status
}

// Peers returns a point-in-time view of the session's known peers, or nil
// before Start.
func (s *Session) Peers() []peermgr.PeerSnapshot {
	if s.peers == nil {
		return nil
	}
	return s.peers.Snapshot()
}

// Summary returns a plain-data snapshot of the session, safe to call at any
// lifecycle stage.
func (s *Session) Summary() SessionSummary {
	done, total := s.Progress()
	// Before Start the piece manager is nil and Progress returns 0, 0; fall
	// back to the parse-time piece count so a Pending torrent shows its real
	// piece total.
	if total == 0 {
		total = s.pieceCount
	}
	s.statusMu.RLock()
	defer s.statusMu.RUnlock()
	var errMsg string
	if s.lastErr != nil {
		errMsg = s.lastErr.Error()
	}
	return SessionSummary{
		ID:              s.id,
		Name:            s.name,
		Status:          s.status,
		DonePieces:      done,
		TotalPieces:     total,
		TotalBytes:      s.totalBytes,
		DownloadRate:    s.downloadRate,
		UploadRate:      s.uploadRate,
		ActivePeers:     s.activePeers,
		WaitingPeers:    s.waitingPeers,
		Error:           errMsg,
		Seeds:           s.seeds,
		MaxConnections:  peermgr.MaxParallelPeers,
		TotalDownloaded: s.totalDownloaded,
		TotalUploaded:   s.totalUploaded,
		TrackerSeeds:    s.trackerSeeds,
		TrackerLeechers: s.trackerLeechers,
		SavePath:        s.location,
		InfoHash:        s.infoHash,
		PieceLength:     s.pieceLength,
		AddedAt:         s.addedAt,
		StartedAt:       s.startedAt,
		CompletedAt:     s.completedAt,
		Comment:         s.comment,
		CreatedBy:       s.createdBy,
		CreationDate:    s.creationDate,
		Private:         s.private,
	}
}

func (s *Session) refreshPeersLoop(ctx context.Context) {
	ticker := time.NewTicker(peerRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.refreshPeers(); err != nil {
				s.logf("findPeers error: %v", err)
			}
		}
	}
}

func (s *Session) refreshPeers() error {
	peers, err := s.findPeers()
	if err != nil {
		return err
	}
	for _, p := range peers {
		s.peers.AddPeer(p)
	}
	return nil
}

func getTorrentName(meta *metainfo.MetaInfo) string {
	var name string
	switch info := meta.Info.Info.(type) {
	case metainfo.SingleFileInfo:
		name = info.Name
	case metainfo.MultiFileInfo:
		name = info.Name
	default:
		return ""
	}
	return name
}

// infoCommon reaches the shared info-dict fields (piece length, pieces,
// private) regardless of single- vs multi-file mode. The default branch
// covers both a nil-able unknown Info and keeps this nil-safe in spirit.
func infoCommon(meta *metainfo.MetaInfo) metainfo.InfoCommon {
	switch info := meta.Info.Info.(type) {
	case metainfo.SingleFileInfo:
		return info.Common()
	case metainfo.MultiFileInfo:
		return info.Common()
	default:
		return metainfo.InfoCommon{}
	}
}

// computeTotalBytes mirrors the type-switch in readTorrentFile to derive the
// torrent's total size once, at parse time.
func computeTotalBytes(meta *metainfo.MetaInfo) int64 {
	switch info := meta.Info.Info.(type) {
	case metainfo.SingleFileInfo:
		return int64(info.Length)
	case metainfo.MultiFileInfo:
		var total int64
		for _, f := range info.Files {
			total += int64(f.Length)
		}
		return total
	default:
		return 0
	}
}

func readTorrentFile(id, file string) (metainfo.MetaInfo, error) {
	// Read metainfo from torrent file
	meta, err := metainfo.NewMetaInfo(file)
	if err != nil {
		return metainfo.MetaInfo{}, fmt.Errorf("failed to parse torrent: %w", err)
	}

	sessionLogf(id, "Announce: %s", meta.Announce)
	if len(meta.AnnounceList) > 0 {
		sessionLogf(id, "Announce-list:")
		for i, tier := range meta.AnnounceList {
			sessionLogf(id, "  Tier %d: %s", i+1, strings.Join(tier, ", "))
		}
	}

	switch info := meta.Info.Info.(type) {
	case metainfo.SingleFileInfo:
		sessionLogf(id, "Mode:         single-file")
		sessionLogf(id, "Name:         %s", info.Name)
		sessionLogf(id, "Length:       %d bytes", info.Length)
		sessionLogf(id, "Piece length: %d bytes", info.PieceLength)
		sessionLogf(id, "Pieces:       %d", len(info.Pieces)/20)
	case metainfo.MultiFileInfo:
		sessionLogf(id, "Mode:         multi-file")
		sessionLogf(id, "Name:         %s", info.Name)
		sessionLogf(id, "Piece length: %d bytes", info.PieceLength)
		sessionLogf(id, "Files:        %d", len(info.Files))
		for _, f := range info.Files {
			sessionLogf(id, "  %s (%d bytes)", strings.Join(f.Path, "/"), f.Length)
		}
	}

	return meta, nil
}

func getTrackers(meta *metainfo.MetaInfo) []string {
	var trackers []string
	if strings.HasPrefix(meta.Announce, "http") {
		trackers = append(trackers, meta.Announce)
	}
	for _, tier := range meta.AnnounceList {
		for _, url := range tier {
			if strings.HasPrefix(url, "http") {
				trackers = append(trackers, url)
			}
		}
	}
	return trackers
}

func (s *Session) findPeers() ([]tracker_client.Peer, error) {
	meta := s.meta
	var trackers []string
	for tracker, nextRefresh := range s.nextRefreshes {
		if time.Now().After(nextRefresh) {
			trackers = append(trackers, tracker)
		}
	}

	if len(trackers) == 0 {
		return []tracker_client.Peer{}, fmt.Errorf("no trackers left")
	}

	s.logf("Announcing to %d HTTP tracker(s)...", len(trackers))
	type result struct {
		Tracker string
		Resp    *tracker_client.AnnounceResponse
		Err     error
	}
	ch := make(chan result, len(trackers))
	for _, url := range trackers {
		event := tracker_client.EventStarted
		if s.announced[url] {
			event = tracker_client.EventNone
		}
		req := tracker_client.AnnounceRequest{
			InfoHash: meta.InfoHash,
			PeerID:   s.peerID,
			Port:     uint16(s.port),
			Compact:  1,
			Event:    event,
		}
		go func() {
			c, err := tracker_client.NewTrackerClient(url)
			if err != nil {
				ch <- result{Tracker: url, Err: err}
				return
			}
			resp, err := c.Announce(context.Background(), req)
			ch <- result{Tracker: url, Resp: resp, Err: err}
		}()
	}

	peerSet := make(map[string]tracker_client.Peer)
	totalPeers := 0
	maxComplete, maxIncomplete := 0, 0
	gotStats := false
	for range trackers {
		res := <-ch
		s.logf("[%s]", res.Tracker)
		if res.Err != nil {
			s.logf("  error: %v", res.Err)
			s.nextRefreshes[res.Tracker] = time.Now().Add(peerRefreshInterval)
			continue
		}
		s.announced[res.Tracker] = true
		resp := res.Resp
		s.logf("  interval:   %d", resp.Interval)
		s.logf("  complete:   %d", resp.Complete)
		s.logf("  incomplete: %d", resp.Incomplete)
		s.logf("  peers:      %d", len(resp.Peers.Peers))
		totalPeers += len(resp.Peers.Peers)
		if resp.Complete > maxComplete {
			maxComplete = resp.Complete
		}
		if resp.Incomplete > maxIncomplete {
			maxIncomplete = resp.Incomplete
		}
		gotStats = true
		for _, p := range resp.Peers.Peers {
			peerSet[fmt.Sprintf("%s:%d", p.Ip, p.Port)] = p
		}

		next := max(time.Duration(resp.Interval)*time.Second, peerRefreshInterval)
		s.nextRefreshes[res.Tracker] = time.Now().Add(next)
	}

	// Swarm stats: trackers report overlapping views of one swarm, so max
	// (not sum) is standard client behavior; summing would double-count. The
	// gotStats guard keeps a round where every tracker errored from wiping
	// good numbers to 0. Known limitation: getTrackers only accepts http(s)
	// URLs and the sample torrents are mostly udp://, so these numbers are
	// often 0 or up to peerRefreshInterval stale.
	if gotStats {
		s.statusMu.Lock()
		s.trackerSeeds, s.trackerLeechers = maxComplete, maxIncomplete
		s.statusMu.Unlock()
	}

	if len(peerSet) == 0 {
		return []tracker_client.Peer{}, fmt.Errorf("no peers found")
	}

	s.logf("Found %d peer(s), %d unique", totalPeers, len(peerSet))
	peers := make([]tracker_client.Peer, 0, len(peerSet))
	for _, p := range peerSet {
		peers = append(peers, p)
	}
	sort.Slice(peers, func(i, j int) bool {
		if peers[i].Ip == peers[j].Ip {
			return peers[i].Port < peers[j].Port
		}
		return peers[i].Ip < peers[j].Ip
	})

	return peers, nil
}
