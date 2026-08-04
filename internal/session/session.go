package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"torrent-demo/internal/metainfo"
	"torrent-demo/internal/peer"
	peermgr "torrent-demo/internal/peer_manager"
	piecemanager "torrent-demo/internal/piece_manager"
	"torrent-demo/internal/tracker_client"
)

type Options struct {
	DownloadRoot string
	Port         int
}

const peerRefreshInterval = 3 * time.Minute

type Session struct {
	downloadRoot string
	torrentPath  string
	location     string
	port         int
	peerID       [20]byte
	meta         *metainfo.MetaInfo
	pieces       *piecemanager.PieceManager
	peers        *peermgr.PeerManager
	reporter     peer.Reporter

	cancel        context.CancelFunc
	stopOnce      sync.Once
	done          chan struct{}
	nextRefreshes map[string]time.Time
	announced     map[string]bool
}

func NewTorrentSession(torrentPath string, options Options) *Session {
	var peerID [20]byte
	copy(peerID[:], "-TD0001-123456789012")

	return &Session{
		downloadRoot:  options.DownloadRoot,
		torrentPath:   torrentPath,
		port:          options.Port,
		peerID:        peerID,
		done:          make(chan struct{}),
		nextRefreshes: make(map[string]time.Time),
		announced:     make(map[string]bool),
	}
}

// SetReporter wires an external observer (e.g. a debug dashboard) into the
// peer manager. Must be called before Start.
func (s *Session) SetReporter(r peer.Reporter) {
	s.reporter = r
}

func (s *Session) Start(ctx context.Context) error {
	meta, err := readTorrentFile(s.torrentPath)
	if err != nil {
		return fmt.Errorf("readTorrentFile: %w", err)
	}
	s.meta = &meta

	for _, tracker := range getTrackers(&meta) {
		s.nextRefreshes[tracker] = time.Now()
	}
	if len(s.nextRefreshes) == 0 {
		return fmt.Errorf("no trackers found")
	}

	name := getTorrentName(s.meta)
	s.location = filepath.Join(s.downloadRoot, name)
	if err := os.MkdirAll(s.location, 0755); err != nil {
		return fmt.Errorf("failed to create download dir: %w", err)
	}

	pieces, err := piecemanager.NewPieceManager(s.location, meta)
	if err != nil {
		return fmt.Errorf("failed to init piece manager: %w", err)
	}
	s.pieces = pieces

	s.peers = peermgr.NewPeerManager(s.meta.InfoHash, s.peerID, s.pieces, s.pieces)
	if s.reporter != nil {
		s.peers.SetReporter(s.reporter)
	}

	// Populate the waiting room before PeerManager.Run, which errors out if it
	// can't activate a first batch from an empty waiting room.
	if err := s.refreshPeers(); err != nil {
		return fmt.Errorf("failed to find peers: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		s.peers.Run(ctx)
	}()
	go func() {
		defer wg.Done()
		s.refreshPeersLoop(ctx)
	}()
	go func() {
		wg.Wait()
		close(s.done)
	}()

	return nil
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

func (s *Session) refreshPeersLoop(ctx context.Context) {
	ticker := time.NewTicker(peerRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.refreshPeers(); err != nil {
				fmt.Printf("findPeers error: %v\n", err)
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

func readTorrentFile(file string) (metainfo.MetaInfo, error) {
	// Read metainfo from torrent file
	meta, err := metainfo.NewMetaInfo(file)
	if err != nil {
		return metainfo.MetaInfo{}, fmt.Errorf("failed to parse torrent: %w", err)
	}

	fmt.Printf("Announce: %s\n", meta.Announce)
	if len(meta.AnnounceList) > 0 {
		fmt.Printf("Announce-list:\n")
		for i, tier := range meta.AnnounceList {
			fmt.Printf("  Tier %d: %s\n", i+1, strings.Join(tier, ", "))
		}
	}

	switch info := meta.Info.Info.(type) {
	case metainfo.SingleFileInfo:
		fmt.Printf("Mode:         single-file\n")
		fmt.Printf("Name:         %s\n", info.Name)
		fmt.Printf("Length:       %d bytes\n", info.Length)
		fmt.Printf("Piece length: %d bytes\n", info.PieceLength)
		fmt.Printf("Pieces:       %d\n", len(info.Pieces)/20)
	case metainfo.MultiFileInfo:
		fmt.Printf("Mode:         multi-file\n")
		fmt.Printf("Name:         %s\n", info.Name)
		fmt.Printf("Piece length: %d bytes\n", info.PieceLength)
		fmt.Printf("Files:        %d\n", len(info.Files))
		for _, f := range info.Files {
			fmt.Printf("  %s (%d bytes)\n", strings.Join(f.Path, "/"), f.Length)
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

	fmt.Printf("\nAnnouncing to %d HTTP tracker(s)...\n", len(trackers))
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
	for range trackers {
		res := <-ch
		fmt.Printf("\n[%s]\n", res.Tracker)
		if res.Err != nil {
			fmt.Printf("  error: %v\n", res.Err)
			s.nextRefreshes[res.Tracker] = time.Now().Add(peerRefreshInterval)
			continue
		}
		s.announced[res.Tracker] = true
		resp := res.Resp
		fmt.Printf("  interval:   %d\n", resp.Interval)
		fmt.Printf("  complete:   %d\n", resp.Complete)
		fmt.Printf("  incomplete: %d\n", resp.Incomplete)
		fmt.Printf("  peers:      %d\n", len(resp.Peers.Peers))
		totalPeers += len(resp.Peers.Peers)
		for _, p := range resp.Peers.Peers {
			peerSet[fmt.Sprintf("%s:%d", p.Ip, p.Port)] = p
		}

		next := max(time.Duration(resp.Interval)*time.Second, peerRefreshInterval)
		s.nextRefreshes[res.Tracker] = time.Now().Add(next)
	}

	if len(peerSet) == 0 {
		return []tracker_client.Peer{}, fmt.Errorf("no peers found")
	}

	fmt.Printf("\nFound %d peer(s), %d unique\n", totalPeers, len(peerSet))
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
