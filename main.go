package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"time"
	"torrent-demo/internal/debugui"
	"torrent-demo/internal/session"
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

	s := session.NewTorrentSession(file, session.Options{
		DownloadRoot: "downloads",
		Port:         6881,
	})

	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
	defer cancel()

	// debug-only: launch the tview dashboard and pipe process output into its
	// log pane so diagnostics cannot overwrite tcell's terminal screen.
	var dbgUI *debugui.UI
	if *debugFlag {
		dbgUI = debugui.New()
		dbgUI.OnQuit(cancel)
		s.SetReporter(dbgUI)

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

	if err := s.Start(ctx); err != nil {
		return err
	}
	defer s.Stop()

	if dbgUI != nil { // debug-only
		if done, total := s.Progress(); total > 0 {
			dbgUI.SetProgress(done, total)
		}
	}

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
				if dbgUI != nil { // debug-only
					dbgUI.SetProgress(done, total)
				}
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
