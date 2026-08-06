package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	peermgr "torrent-demo/internal/peer_manager"
	"torrent-demo/internal/session"
)

type App struct {
	ctx context.Context
	mgr *session.SessionManager
}

func NewApp() *App {
	return &App{
		mgr: session.NewSessionManager(session.Options{
			DownloadRoot: "downloads",
			Port:         6881,
		}),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	log.SetOutput(os.Stdout)
	go a.sessionTicker(ctx)
}

// sessionTicker pushes the full session list to the frontend once a second —
// the frontend's only source of live session state.
func (a *App) sessionTicker(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runtime.EventsEmit(ctx, "sessions:update", a.mgr.List())
		}
	}
}

// Bound methods below are called from the frontend via wailsjs bindings.

// PickTorrentFile opens a native file dialog filtered to .torrent files.
func (a *App) PickTorrentFile() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select a torrent file",
		Filters: []runtime.FileFilter{
			{DisplayName: "Torrent files (*.torrent)", Pattern: "*.torrent"},
		},
	})
}

func (a *App) AddTorrent(path string) (session.SessionSummary, error) {
	return a.mgr.Add(path)
}

func (a *App) StartSession(id string) error {
	return a.mgr.Start(a.ctx, id)
}

func (a *App) StopSession(id string) error {
	return a.mgr.Stop(id)
}

func (a *App) RemoveSession(id string) error {
	return a.mgr.Remove(id)
}

// ListSessions is called once on frontend mount so the first paint isn't
// empty before the first sessions:update tick.
func (a *App) ListSessions() []session.SessionSummary {
	return a.mgr.List()
}

// GetPeers is a thin wrapper over mgr.Peers. Snapshot is mutex-based and
// never blocks, so no timeout plumbing is needed for this slice.
func (a *App) GetPeers(id string) ([]peermgr.PeerSnapshot, error) {
	return a.mgr.Peers(id)
}
