package debugui

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

func TestBuildStrip(t *testing.T) {
	done := []bool{true, false, true, false}
	strip := buildStrip(done)

	if got := strings.Count(strip, "[green]█"); got != 2 {
		t.Errorf("expected 2 lit cells, got %d in %q", got, strip)
	}
	if got := strings.Count(strip, "[gray]░"); got != 2 {
		t.Errorf("expected 2 unlit cells, got %d in %q", got, strip)
	}
}

func TestBuildStripEmpty(t *testing.T) {
	if got := buildStrip(nil); got != "(no active piece)" {
		t.Errorf("buildStrip(nil) = %q, want placeholder", got)
	}
}

func TestReporterLifecycle(t *testing.T) {
	ui := New()
	addr := "1.2.3.4:6881"

	ui.PeerConnected(addr)
	active, _, _ := ui.render()
	if active == "(no active peers)\n" {
		t.Fatal("expected connected peer to appear in active panel")
	}

	ui.PieceStarted(addr, 7, 4)
	ui.BlockSaved(addr, 0, 16384)
	ui.BlockSaved(addr, 2, 16384)
	ui.BlockSaved(addr, 0, 16384) // duplicate, must not double count

	ui.mu.Lock()
	ps := ui.active[addr]
	if ps.doneCount != 2 {
		t.Errorf("doneCount = %d, want 2", ps.doneCount)
	}
	if ps.pieceIndex != 7 {
		t.Errorf("pieceIndex = %d, want 7", ps.pieceIndex)
	}
	ui.mu.Unlock()

	ui.PieceCompleted(addr, 7)
	ui.mu.Lock()
	if ui.active[addr].hasPiece {
		t.Error("expected hasPiece = false after PieceCompleted")
	}
	ui.mu.Unlock()

	ui.PieceFailed(addr, "hash mismatch p9")
	_, failed, _ := ui.render()
	if failed == "(none yet)\n" {
		t.Fatal("expected failed entry to appear in failed panel")
	}

	ui.PeerDisconnected(addr, nil)
	ui.mu.Lock()
	if _, ok := ui.active[addr]; ok {
		t.Error("expected peer removed from active map after disconnect")
	}
	ui.mu.Unlock()
}

// newTestUI returns a UI driven by a simulation screen, so Run/Stop can be
// exercised without a real terminal.
func newTestUI(t *testing.T) (*UI, tcell.SimulationScreen) {
	t.Helper()
	ui := New()
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	ui.app.SetScreen(screen)
	return ui, screen
}

func TestRunStop(t *testing.T) {
	ui, _ := newTestUI(t)

	done := make(chan error, 1)
	go func() { done <- ui.Run() }()

	ui.PeerConnected("1.2.3.4:6881")
	ui.PieceStarted("1.2.3.4:6881", 1, 4)
	ui.BlockSaved("1.2.3.4:6881", 0, 16384)

	time.Sleep(2 * refreshInterval) // let the refresh loop draw at least once

	ui.Stop()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run() returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return after Stop()")
	}
}

func TestOnQuit(t *testing.T) {
	ui, screen := newTestUI(t)

	quit := make(chan struct{})
	ui.OnQuit(func() { close(quit) })

	done := make(chan error, 1)
	go func() { done <- ui.Run() }()
	time.Sleep(20 * time.Millisecond) // let Run reach its event loop

	screen.InjectKey(tcell.KeyCtrlC, 0, tcell.ModCtrl)

	select {
	case <-quit:
	case <-time.After(2 * time.Second):
		t.Fatal("OnQuit callback was not invoked on Ctrl-C")
	}

	ui.Stop()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return after Stop()")
	}
}

func TestBuildProgress(t *testing.T) {
	if got := buildProgress(0, 0, 0); got != "(no progress yet)" {
		t.Errorf("buildProgress(0,0,0) = %q, want placeholder", got)
	}
	got := buildProgress(5, 10, 2048)
	if !strings.Contains(got, "5/10 pieces") || !strings.Contains(got, "50.0%") || !strings.Contains(got, "2.0 KB/s") {
		t.Errorf("buildProgress(5,10,2048) = %q, want it to mention 5/10 pieces, 50.0%%, and 2.0 KB/s", got)
	}
}

func TestFormatRate(t *testing.T) {
	cases := []struct {
		bps  float64
		want string
	}{
		{500, "500 B/s"},
		{2048, "2.0 KB/s"},
		{5 * 1024 * 1024, "5.00 MB/s"},
	}
	for _, c := range cases {
		if got := formatRate(c.bps); got != c.want {
			t.Errorf("formatRate(%v) = %q, want %q", c.bps, got, c.want)
		}
	}
}

func TestSpeedTracking(t *testing.T) {
	ui := New()
	addr := "1.2.3.4:6881"
	ui.PeerConnected(addr)
	ui.PieceStarted(addr, 1, 4)
	ui.render() // establish the initial sample baseline

	ui.BlockSaved(addr, 0, 16384)
	time.Sleep(50 * time.Millisecond)
	ui.render()

	ui.mu.Lock()
	rate := ui.active[addr].rateBps
	global := ui.globalRateBps
	ui.mu.Unlock()
	if rate <= 0 {
		t.Errorf("expected positive per-peer rate after a block was saved, got %v", rate)
	}
	if global <= 0 {
		t.Errorf("expected positive global rate after a block was saved, got %v", global)
	}
}

func TestSetProgress(t *testing.T) {
	ui := New()
	ui.SetProgress(3, 9)
	_, _, progress := ui.render()
	if !strings.Contains(progress, "3/9 pieces") {
		t.Errorf("render() progress = %q, want it to mention 3/9 pieces", progress)
	}
}

func TestToggleLogView(t *testing.T) {
	ui, screen := newTestUI(t)

	done := make(chan error, 1)
	go func() { done <- ui.Run() }()
	time.Sleep(20 * time.Millisecond)

	ui.mu.Lock()
	initiallyShowing := ui.showingLog
	ui.mu.Unlock()
	if initiallyShowing {
		t.Fatal("expected dashboard to be shown initially")
	}

	screen.InjectKey(tcell.KeyTab, 0, tcell.ModNone)
	time.Sleep(20 * time.Millisecond)
	ui.mu.Lock()
	showing := ui.showingLog
	ui.mu.Unlock()
	if !showing {
		t.Error("expected Tab to switch to the log view")
	}

	screen.InjectKey(tcell.KeyTab, 0, tcell.ModNone)
	time.Sleep(20 * time.Millisecond)
	ui.mu.Lock()
	showing = ui.showingLog
	ui.mu.Unlock()
	if showing {
		t.Error("expected second Tab to switch back to the dashboard")
	}

	ui.Stop()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return after Stop()")
	}
}

func TestFailedEntriesCapped(t *testing.T) {
	ui := New()
	for i := 0; i < maxFailedEntries+10; i++ {
		ui.PieceFailed("addr", "reason")
	}
	ui.mu.Lock()
	defer ui.mu.Unlock()
	if len(ui.failed) != maxFailedEntries {
		t.Errorf("len(failed) = %d, want %d", len(ui.failed), maxFailedEntries)
	}
}
