package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"torrent-demo/internal/debugui"
	"torrent-demo/internal/metainfo"
	"torrent-demo/internal/peer"
	piecemanager "torrent-demo/internal/piece_manager"
	"torrent-demo/internal/tracker_client"
)

const (
	maxConnectedPeers    = 32
	maxActiveDownloads   = 8
	downloadTimeout      = 60 * time.Minute
	peerRetryCooldown    = 2 * time.Minute
	maxPeerRetryCooldown = 30 * time.Minute
)

// debug-only: launches a tview dashboard instead of relying solely on
// stdout logging. Off by default; normal runs are unaffected.
var debugFlag = flag.Bool("debug", false, "debug-only: launch a tview dashboard showing live peer/piece state")

type peerRunResult struct {
	peer tracker_client.Peer
	err  error
}

type peerRetry struct {
	peer     tracker_client.Peer
	attempts int
	readyAt  time.Time
}

type limitedScheduler struct {
	base   peer.PieceScheduler
	slots  chan struct{}
	mu     sync.Mutex
	active map[int]struct{}
}

func newLimitedScheduler(base peer.PieceScheduler, limit int) *limitedScheduler {
	return &limitedScheduler{
		base:   base,
		slots:  make(chan struct{}, limit),
		active: make(map[int]struct{}),
	}
}

func (s *limitedScheduler) GetWork(field peer.Bitfield) (peer.PieceWork, bool) {
	select {
	case s.slots <- struct{}{}:
	default:
		return peer.PieceWork{}, false
	}

	work, ok := s.base.GetWork(field)
	if !ok {
		<-s.slots
		return peer.PieceWork{}, false
	}

	s.mu.Lock()
	s.active[work.Index] = struct{}{}
	s.mu.Unlock()
	return work, true
}

func (s *limitedScheduler) CompletePiece(index int, data []byte) error {
	err := s.base.CompletePiece(index, data)
	s.release(index)
	return err
}

func (s *limitedScheduler) UnclaimPiece(index int) {
	s.base.UnclaimPiece(index)
	s.release(index)
}

func (s *limitedScheduler) GetLocalBitfield() peer.Bitfield {
	return s.base.GetLocalBitfield()
}

func (s *limitedScheduler) GetBlock(index, begin, length int) ([]byte, error) {
	return s.base.GetBlock(index, begin, length)
}

func (s *limitedScheduler) release(index int) {
	s.mu.Lock()
	_, ok := s.active[index]
	if ok {
		delete(s.active, index)
	}
	s.mu.Unlock()
	if !ok {
		return
	}

	select {
	case <-s.slots:
	default:
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: %s <file>\n", os.Args[0])
	flag.PrintDefaults()
	os.Exit(1)
}

func peerKey(p tracker_client.Peer) string {
	return fmt.Sprintf("%s:%d", p.Ip, p.Port)
}

func peerRetryDelay(attempts int) time.Duration {
	delay := peerRetryCooldown
	for i := 1; i < attempts; i++ {
		delay *= 2
		if delay >= maxPeerRetryCooldown {
			return maxPeerRetryCooldown
		}
	}
	return delay
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	flag.Usage = usage
	flag.Parse()

	if flag.NArg() < 1 {
		usage()
	}

	file := flag.Arg(0)

	meta, err := metainfo.NewMetaInfo(file)
	if err != nil {
		return fmt.Errorf("failed to parse torrent: %w", err)
	}

	fmt.Printf("Announce: %s\n", meta.Announce)
	if len(meta.AnnounceList) > 0 {
		fmt.Printf("Announce-list:\n")
		for i, tier := range meta.AnnounceList {
			fmt.Printf("  Tier %d: %s\n", i+1, strings.Join(tier, ", "))
		}
	}

	var name string
	switch info := meta.Info.Info.(type) {
	case metainfo.SingleFileInfo:
		name = info.Name
		fmt.Printf("Mode:         single-file\n")
		fmt.Printf("Name:         %s\n", info.Name)
		fmt.Printf("Length:       %d bytes\n", info.Length)
		fmt.Printf("Piece length: %d bytes\n", info.PieceLength)
		fmt.Printf("Pieces:       %d\n", len(info.Pieces)/20)
	case metainfo.MultiFileInfo:
		name = info.Name
		fmt.Printf("Mode:         multi-file\n")
		fmt.Printf("Name:         %s\n", info.Name)
		fmt.Printf("Piece length: %d bytes\n", info.PieceLength)
		fmt.Printf("Files:        %d\n", len(info.Files))
		for _, f := range info.Files {
			fmt.Printf("  %s (%d bytes)\n", strings.Join(f.Path, "/"), f.Length)
		}
	}

	// collect all HTTP trackers
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

	if len(trackers) == 0 {
		fmt.Println("No HTTP trackers found")
		return nil
	}

	var peerID [20]byte
	copy(peerID[:], "-TD0001-123456789012")

	req := tracker_client.AnnounceRequest{
		InfoHash: meta.InfoHash,
		PeerID:   peerID,
		Port:     6881,
		Compact:  1,
		Event:    tracker_client.EventStarted,
	}

	fmt.Printf("\nAnnouncing to %d HTTP tracker(s)...\n", len(trackers))
	type result struct {
		Tracker string
		Resp    *tracker_client.AnnounceResponse
		Err     error
	}
	ch := make(chan result, len(trackers))
	for _, url := range trackers {
		url := url
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
			continue
		}
		resp := res.Resp
		fmt.Printf("  interval:   %d\n", resp.Interval)
		fmt.Printf("  complete:   %d\n", resp.Complete)
		fmt.Printf("  incomplete: %d\n", resp.Incomplete)
		fmt.Printf("  peers:      %d\n", len(resp.Peers.Peers))
		totalPeers += len(resp.Peers.Peers)
		for _, p := range resp.Peers.Peers {
			peerSet[fmt.Sprintf("%s:%d", p.Ip, p.Port)] = p
		}
	}

	if len(peerSet) == 0 {
		fmt.Println("\nNo peers found")
		return nil
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

	location := filepath.Join("downloads", name)
	if err := os.MkdirAll(location, 0755); err != nil {
		return fmt.Errorf("failed to create download dir: %w", err)
	}

	pm, err := piecemanager.NewPieceManager(location, meta)
	if err != nil {
		return fmt.Errorf("failed to init piece manager: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
	defer cancel()

	// debug-only: launch the tview dashboard and pipe stdout into its log
	// pane so the existing fmt.Printf calls below keep working untouched.
	var dbgUI *debugui.UI
	if *debugFlag {
		dbgUI = debugui.New()
		dbgUI.OnQuit(cancel)
		if done, total := pm.Progress(); total > 0 {
			dbgUI.SetProgress(done, total)
		}

		origStdout := os.Stdout
		r, w, perr := os.Pipe()
		if perr != nil {
			return fmt.Errorf("failed to set up debug UI: %w", perr)
		}
		os.Stdout = w
		go io.Copy(dbgUI.LogWriter(), r)

		uiDone := make(chan error, 1)
		go func() { uiDone <- dbgUI.Run() }()

		defer func() {
			dbgUI.Stop()
			<-uiDone
			os.Stdout = origStdout
		}()
	}

	scheduler := newLimitedScheduler(pm, maxActiveDownloads)

	var wg sync.WaitGroup
	doneCh := make(chan peerRunResult, len(peers))
	startPeer := func(p tracker_client.Peer) {
		// fmt.Printf("[%s] starting peer connection\n", peerKey(p))
		wg.Add(1)
		go func(p tracker_client.Peer) {
			defer wg.Done()
			result := peerRunResult{peer: p}
			defer func() { doneCh <- result }()

			pc, err := peer.NewPeerConnection(p.Ip, p.Port, meta.InfoHash, peerID, scheduler)
			if err != nil {
				// fmt.Printf("[%s:%d] dial failed: %v\n", p.Ip, p.Port, err)
				result.err = err
				return
			}
			if dbgUI != nil { // debug-only
				pc.SetReporter(dbgUI)
			}
			fmt.Printf("[%s:%d] connected\n", p.Ip, p.Port)
			if err := pc.Run(ctx); err != nil {
				fmt.Printf("[%s:%d] disconnected: %v\n", p.Ip, p.Port, err)
				result.err = err
			}
		}(p)
	}

	fmt.Printf("\nDownloading with up to %d connected peer(s), %d active download(s) for %s into %s...\n", min(maxConnectedPeers, len(peers)), maxActiveDownloads, downloadTimeout, location)

	progressDone := make(chan struct{})
	go func() {
		defer close(progressDone)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				done, total := pm.Progress()
				fmt.Printf("progress: %d/%d pieces\n", done, total)
				if dbgUI != nil { // debug-only
					dbgUI.SetProgress(done, total)
				}
			}
		}
	}()

	started := 0
	activePeers := 0
	retryAttempts := make(map[string]int)
	var retryQueue []peerRetry

	scheduleRetry := func(p tracker_client.Peer) {
		key := peerKey(p)
		retryAttempts[key]++
		delay := peerRetryDelay(retryAttempts[key])
		retryQueue = append(retryQueue, peerRetry{
			peer:     p,
			attempts: retryAttempts[key],
			readyAt:  time.Now().Add(delay),
		})
		// fmt.Printf("[%s] retrying in %s after failed attempt %d\n", key, delay, retryAttempts[key])
	}

	popReadyRetry := func(now time.Time) (tracker_client.Peer, bool) {
		for i, retry := range retryQueue {
			if retry.readyAt.After(now) {
				continue
			}
			retryQueue = append(retryQueue[:i], retryQueue[i+1:]...)
			return retry.peer, true
		}
		return tracker_client.Peer{}, false
	}

	nextRetryAt := func() (time.Time, bool) {
		if len(retryQueue) == 0 {
			return time.Time{}, false
		}
		next := retryQueue[0].readyAt
		for _, retry := range retryQueue[1:] {
			if retry.readyAt.Before(next) {
				next = retry.readyAt
			}
		}
		return next, true
	}

	startAvailablePeers := func() {
		for activePeers < maxConnectedPeers {
			if started < len(peers) {
				startPeer(peers[started])
				started++
				activePeers++
				// fmt.Printf("active peers: %d/%d\n", activePeers, maxConnectedPeers)
				continue
			}

			p, ok := popReadyRetry(time.Now())
			if !ok {
				return
			}
			startPeer(p)
			activePeers++
			// fmt.Printf("active peers: %d/%d\n", activePeers, maxConnectedPeers)
		}
	}

	for {
		startAvailablePeers()

		if ctx.Err() != nil {
			wg.Wait()
			break
		}

		if activePeers == 0 && started >= len(peers) && len(retryQueue) == 0 {
			break
		}

		var retryTimer *time.Timer
		var retryTimerCh <-chan time.Time
		if activePeers < maxConnectedPeers {
			if next, ok := nextRetryAt(); ok {
				delay := time.Until(next)
				if delay < 0 {
					delay = 0
				}
				retryTimer = time.NewTimer(delay)
				retryTimerCh = retryTimer.C
			}
		}

		select {
		case <-ctx.Done():
			wg.Wait()
			activePeers = 0
		case result := <-doneCh:
			activePeers--
			// fmt.Printf("[%s] peer slot released; active peers: %d/%d\n", peerKey(result.peer), activePeers, maxConnectedPeers)
			if result.err != nil && ctx.Err() == nil {
				scheduleRetry(result.peer)
			}
		case <-retryTimerCh:
		}

		if retryTimer != nil {
			retryTimer.Stop()
		}
	}

	wg.Wait()
	cancel()
	<-progressDone

	done, total := pm.Progress()
	fmt.Printf("\nFinished: %d/%d pieces downloaded\n", done, total)

	return nil
}
