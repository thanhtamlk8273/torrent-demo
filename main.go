package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	metainfo "torrent-demo/internal/metainfo"
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

	switch info := meta.Info.(type) {
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

	return nil
}
