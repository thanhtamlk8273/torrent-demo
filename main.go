package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"torrent-demo/internal/metainfo"
	"torrent-demo/internal/tracker_client"
)

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
	}
	ch := make(chan result, len(trackers))
	for _, url := range trackers {
		url := url
		go func() {
			fmt.Printf("\n[%s]\n", url)
			c, err := tracker_client.NewTrackerClient(url)
			if err != nil {
				fmt.Printf("  error: %v\n", err)
				return
			}
			resp, err := c.Announce(context.Background(), req)
			if err != nil {
				fmt.Printf("  error: %v\n", err)
				return
			}

			ch <- result{
				Tracker: url,
				Resp:    resp,
			}
		}()
	}

	for range trackers {
		res := <-ch
		resp := res.Resp
		fmt.Printf("\nAnswer from %s\n", res.Tracker)
		fmt.Printf("  interval:   %d\n", resp.Interval)
		fmt.Printf("  complete:   %d\n", resp.Complete)
		fmt.Printf("  incomplete: %d\n", resp.Incomplete)
		fmt.Printf("  peers:      %d\n", len(resp.Peers.Peers))
		for _, p := range resp.Peers.Peers {
			fmt.Printf("    %s:%d\n", p.Ip, p.Port)
		}
	}
	return nil
}
