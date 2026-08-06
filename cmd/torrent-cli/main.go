package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"
	"torrent-demo/internal/session"
)

const downloadTimeout = 60 * time.Minute

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

	s, err := session.NewTorrentSession("cli", file, session.Options{
		DownloadRoot: "downloads",
		Port:         6881,
	})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
	defer cancel()

	if err := s.Start(ctx); err != nil {
		return err
	}
	defer s.Stop()

	fmt.Printf("\nDownloading (timeout: %s)...\n", downloadTimeout)

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
				done, total := s.Progress()
				fmt.Printf("progress: %d/%d pieces\n", done, total)
				if total > 0 && done == total {
					s.Stop()
					return
				}
			}
		}
	}()

	<-s.Done()
	<-progressDone

	done, total := s.Progress()
	fmt.Printf("\nFinished: %d/%d pieces downloaded\n", done, total)

	return nil
}
