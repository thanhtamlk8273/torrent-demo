package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"torrent-demo/internal/debugui"
	"torrent-demo/internal/metainfo"
	peermgr "torrent-demo/internal/peer_manager"
	piecemanager "torrent-demo/internal/piece_manager"
	"torrent-demo/internal/tracker_client"
)

const downloadTimeout = 60 * time.Minute

// debug-only: launches a tview dashboard instead of relying solely on
// stdout logging. Off by default; normal runs are unaffected.
var debugFlag = flag.Bool("debug", false, "debug-only: launch a tview dashboard showing live peer/piece state")

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: %s <file>\n", os.Args[0])
	flag.PrintDefaults()
	os.Exit(1)
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

	pieces, err := piecemanager.NewPieceManager(location, meta)
	if err != nil {
		return fmt.Errorf("failed to init piece manager: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
	defer cancel()

	// debug-only: launch the tview dashboard and pipe process output into its
	// log pane so diagnostics cannot overwrite tcell's terminal screen.
	var dbgUI *debugui.UI
	if *debugFlag {
		dbgUI = debugui.New()
		dbgUI.OnQuit(cancel)
		if done, total := pieces.Progress(); total > 0 {
			dbgUI.SetProgress(done, total)
		}

		origStdout := os.Stdout
		origStderr := os.Stderr
		origLogWriter := log.Writer()
		r, w, perr := os.Pipe()
		if perr != nil {
			return fmt.Errorf("failed to set up debug UI: %w", perr)
		}
		os.Stdout = w
		os.Stderr = w
		log.SetOutput(w)

		copyDone := make(chan struct{})
		go func() {
			_, _ = io.Copy(dbgUI.LogWriter(), r)
			close(copyDone)
		}()

		uiDone := make(chan error, 1)
		go func() { uiDone <- dbgUI.Run() }()

		defer func() {
			dbgUI.Stop()
			<-uiDone
			os.Stdout = origStdout
			os.Stderr = origStderr
			log.SetOutput(origLogWriter)
			_ = w.Close()
			<-copyDone
			_ = r.Close()
		}()
	}

	peerMgr := peermgr.NewPeerManager(meta.InfoHash, peerID, pieces, pieces)
	if dbgUI != nil { // debug-only
		peerMgr.SetReporter(dbgUI)
	}
	for _, p := range peers {
		peerMgr.AddPeer(p)
	}

	fmt.Printf("\nDownloading (timeout: %s) into %s...\n", downloadTimeout, location)

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
				done, total := pieces.Progress()
				fmt.Printf("progress: %d/%d pieces\n", done, total)
				if dbgUI != nil { // debug-only
					dbgUI.SetProgress(done, total)
				}
				if total > 0 && done == total {
					cancel()
					return
				}
			}
		}
	}()

	peerMgr.Run(ctx)
	<-progressDone

	done, total := pieces.Progress()
	fmt.Printf("\nFinished: %d/%d pieces downloaded\n", done, total)

	return nil
}
