// Package debugui is a debug-only terminal dashboard for visualizing peer
// download activity. It is throwaway tooling for diagnosing the torrent
// client during development and is expected to be reworked or removed once
// a real design is settled on.
package debugui

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"torrent-demo/internal/peer"
)

const (
	stripWidth       = 40
	refreshInterval  = 250 * time.Millisecond
	rateTimeConstant = 3 * time.Second // debug-only: EWMA smoothing window for speed display
)

type peerState struct {
	addr       string
	pieceIndex int
	hasPiece   bool
	done       []bool
	doneCount  int

	// debug-only: download speed tracking, sampled and EWMA-smoothed in render().
	bytesTotal     int64
	bytesAtSample  int64
	rateBps        float64
	lastSampleTime time.Time

	// debug-only: upload speed tracking, mirrors the download fields above.
	upBytesTotal     int64
	upBytesAtSample  int64
	upRateBps        float64
	upLastSampleTime time.Time
}

// UI is a debug-only tview dashboard. It implements peer.Reporter so it can
// be attached directly to a PeerConnection via SetReporter.
type UI struct {
	mu     sync.Mutex
	active map[string]*peerState
	doneP  int
	totalP int

	// debug-only: global download speed tracking, mirrors peerState's fields above.
	totalBytes     int64
	bytesAtSample  int64
	globalRateBps  float64
	lastSampleTime time.Time

	// debug-only: global upload speed tracking, mirrors the download fields above.
	totalUpBytes     int64
	upBytesAtSample  int64
	globalUpRateBps  float64
	upLastSampleTime time.Time

	app          *tview.Application
	pages        *tview.Pages
	activeView   *tview.TextView
	uploadView   *tview.TextView
	progressView *tview.TextView
	logView      *tview.TextView

	showingLog bool
	quitFn     func()

	stopRefresh chan struct{}
}

var _ peer.Reporter = (*UI)(nil) // debug-only

const dashboardPage = "dashboard"
const logPage = "log"

// New builds the debug dashboard. Call Run to take over the terminal.
func New() *UI {
	ui := &UI{
		active:      make(map[string]*peerState),
		stopRefresh: make(chan struct{}),
	}

	ui.activeView = tview.NewTextView().SetDynamicColors(true)
	ui.activeView.SetBorder(true).SetTitle(" Active Peers (debug) [Tab: logs] ")

	ui.uploadView = tview.NewTextView().SetDynamicColors(true)
	ui.uploadView.SetBorder(true).SetTitle(" Upload (debug) ")

	ui.progressView = tview.NewTextView().SetDynamicColors(true)
	ui.progressView.SetBorder(true).SetTitle(" Progress (debug) ")

	ui.logView = tview.NewTextView().SetDynamicColors(false).SetMaxLines(2000)
	ui.logView.SetBorder(true).SetTitle(" Log (debug) [Tab: back to dashboard] ")
	ui.logView.SetChangedFunc(func() { ui.app.Draw() })

	panels := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(ui.activeView, 0, 1, false).
		AddItem(ui.uploadView, 0, 1, false)

	dashboard := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(panels, 0, 1, false).
		AddItem(ui.progressView, 3, 0, false)

	ui.pages = tview.NewPages().
		AddPage(dashboardPage, dashboard, true, true).
		AddPage(logPage, ui.logView, true, false)

	ui.app = tview.NewApplication().SetRoot(ui.pages, true)
	ui.app.SetInputCapture(ui.handleInput)

	return ui
}

// handleInput intercepts Ctrl-C (tview's raw mode otherwise swallows SIGINT)
// and Tab (toggles between the dashboard and the full-screen log view).
func (ui *UI) handleInput(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyCtrlC:
		if ui.quitFn != nil {
			ui.quitFn()
		}
		return nil
	case tcell.KeyTab:
		ui.toggleLogView()
		return nil
	}
	return event
}

func (ui *UI) toggleLogView() {
	ui.mu.Lock()
	ui.showingLog = !ui.showingLog
	showingLog := ui.showingLog
	ui.mu.Unlock()

	if showingLog {
		ui.pages.SwitchToPage(logPage)
	} else {
		ui.pages.SwitchToPage(dashboardPage)
	}
}

// LogWriter returns the pane that raw stdout should be piped into. It is
// safe to write to from any goroutine (tview.TextView implements io.Writer
// and documents this as goroutine-safe).
func (ui *UI) LogWriter() *tview.TextView {
	return ui.logView
}

// Run takes over the terminal and blocks until Stop is called or the user
// quits (Ctrl-C). It also starts the periodic panel refresh.
func (ui *UI) Run() error {
	go ui.refreshLoop()
	err := ui.app.Run()
	close(ui.stopRefresh)
	return err
}

// Stop releases the terminal.
func (ui *UI) Stop() {
	ui.app.Stop()
}

// OnQuit registers fn to run when the user presses Ctrl-C inside the
// dashboard, in lieu of the terminal's normal SIGINT handling (tview puts
// the terminal in raw mode, so Ctrl-C arrives as a key event instead). It
// does not stop the dashboard itself, just signals the caller (typically to
// cancel the download); call Stop separately once ready to release it.
func (ui *UI) OnQuit(fn func()) {
	ui.quitFn = fn
}

// SetProgress updates the small overall piece-progress panel. Safe to call
// from any goroutine.
func (ui *UI) SetProgress(done, total int) {
	ui.mu.Lock()
	ui.doneP, ui.totalP = done, total
	ui.mu.Unlock()
}

func (ui *UI) refreshLoop() {
	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ui.stopRefresh:
			return
		case <-ticker.C:
			active, upload, progress := ui.render()
			ui.app.QueueUpdateDraw(func() {
				ui.activeView.SetText(active)
				ui.uploadView.SetText(upload)
				ui.progressView.SetText(progress)
			})
		}
	}
}

func (ui *UI) render() (active, upload, progress string) {
	ui.mu.Lock()
	defer ui.mu.Unlock()

	now := time.Now()
	if !ui.lastSampleTime.IsZero() {
		ui.globalRateBps = ewmaRate(ui.globalRateBps, ui.totalBytes-ui.bytesAtSample, now.Sub(ui.lastSampleTime))
	}
	ui.bytesAtSample = ui.totalBytes
	ui.lastSampleTime = now

	if !ui.upLastSampleTime.IsZero() {
		ui.globalUpRateBps = ewmaRate(ui.globalUpRateBps, ui.totalUpBytes-ui.upBytesAtSample, now.Sub(ui.upLastSampleTime))
	}
	ui.upBytesAtSample = ui.totalUpBytes
	ui.upLastSampleTime = now

	addrs := make([]string, 0, len(ui.active))
	for addr := range ui.active {
		addrs = append(addrs, addr)
	}
	sort.Slice(addrs, func(i, j int) bool {
		pi, pj := ui.active[addrs[i]], ui.active[addrs[j]]
		ai := pi.hasPiece || pi.rateBps > 0 || pi.upRateBps > 0
		aj := pj.hasPiece || pj.rateBps > 0 || pj.upRateBps > 0
		if ai != aj {
			return ai
		}
		return addrs[i] < addrs[j]
	})

	var b strings.Builder
	if len(addrs) == 0 {
		b.WriteString("(no active peers)\n")
	}
	var ub strings.Builder
	if len(addrs) == 0 {
		ub.WriteString("(no active peers)\n")
	}
	for _, addr := range addrs {
		ps := ui.active[addr]
		if ps.lastSampleTime.IsZero() {
			ps.lastSampleTime = now
			ps.bytesAtSample = ps.bytesTotal
		} else {
			ps.rateBps = ewmaRate(ps.rateBps, ps.bytesTotal-ps.bytesAtSample, now.Sub(ps.lastSampleTime))
			ps.bytesAtSample = ps.bytesTotal
			ps.lastSampleTime = now
		}

		if ps.hasPiece {
			fmt.Fprintf(&b, "%s  piece %d  (%d/%d)  %s\n", tview.Escape(addr), ps.pieceIndex, ps.doneCount, len(ps.done), formatRate(ps.rateBps))
			b.WriteString(buildStrip(ps.done))
			b.WriteString("\n\n")
		} else {
			fmt.Fprintf(&b, "%s  (idle)  %s\n\n", tview.Escape(addr), formatRate(ps.rateBps))
		}

		if ps.upLastSampleTime.IsZero() {
			ps.upLastSampleTime = now
			ps.upBytesAtSample = ps.upBytesTotal
		} else {
			ps.upRateBps = ewmaRate(ps.upRateBps, ps.upBytesTotal-ps.upBytesAtSample, now.Sub(ps.upLastSampleTime))
			ps.upBytesAtSample = ps.upBytesTotal
			ps.upLastSampleTime = now
		}
		fmt.Fprintf(&ub, "%s  %s  (%s total)\n\n", tview.Escape(addr), formatRate(ps.upRateBps), formatBytes(ps.upBytesTotal))
	}

	progress = buildProgress(ui.doneP, ui.totalP, ui.globalRateBps, ui.globalUpRateBps)

	return b.String(), ub.String(), progress
}

// buildProgress renders the "done/total pieces" line with a small bar and
// the current overall download/upload speeds.
func buildProgress(done, total int, downRateBps, upRateBps float64) string {
	if total == 0 {
		return "(no progress yet)"
	}
	const barWidth = 30
	lit := done * barWidth / total
	bar := strings.Repeat("█", lit) + strings.Repeat("░", barWidth-lit)
	pct := float64(done) * 100 / float64(total)
	return fmt.Sprintf("[green]%s[white]  %d/%d pieces (%.1f%%)  ↓ %s  ↑ %s", bar, done, total, pct, formatRate(downRateBps), formatRate(upRateBps))
}

// ewmaRate folds a newly-observed instantaneous rate (deltaBytes over
// elapsed) into prevRate using a continuous-time exponential moving
// average, so it stays meaningful across the irregular intervals at which
// render() happens to be called (refresh ticker vs. test calls).
func ewmaRate(prevRate float64, deltaBytes int64, elapsed time.Duration) float64 {
	if elapsed <= 0 {
		return prevRate
	}
	instant := float64(deltaBytes) / elapsed.Seconds()
	alpha := 1 - math.Exp(-elapsed.Seconds()/rateTimeConstant.Seconds())
	return alpha*instant + (1-alpha)*prevRate
}

// formatRate renders bytes/sec using the binary-prefix units (KB/MB = 1024,
// 1024*1024) that BitTorrent clients conventionally display speed in.
func formatRate(bps float64) string {
	switch {
	case bps >= 1024*1024:
		return fmt.Sprintf("%.2f MB/s", bps/(1024*1024))
	case bps >= 1024:
		return fmt.Sprintf("%.1f KB/s", bps/1024)
	default:
		return fmt.Sprintf("%.0f B/s", bps)
	}
}

// formatBytes renders a byte count using the same binary-prefix units as
// formatRate, for displaying running totals (e.g. bytes uploaded so far).
func formatBytes(n int64) string {
	switch {
	case n >= 1024*1024:
		return fmt.Sprintf("%.2f MB", float64(n)/(1024*1024))
	case n >= 1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// buildStrip renders a fixed-width strip of lit/unlit blocks. When the
// number of blocks exceeds stripWidth, each cell represents a group of
// blocks and only lights up once every block in that group is done.
func buildStrip(done []bool) string {
	n := len(done)
	if n == 0 {
		return "(no active piece)"
	}
	width := n
	if width > stripWidth {
		width = stripWidth
	}
	group := (n + width - 1) / width

	var b strings.Builder
	for i := 0; i < width; i++ {
		start := i * group
		if start >= n {
			break
		}
		end := min(start+group, n)
		lit := true
		for _, d := range done[start:end] {
			if !d {
				lit = false
				break
			}
		}
		if lit {
			b.WriteString("[green]█[white]")
		} else {
			b.WriteString("[gray]░[white]")
		}
	}
	return b.String()
}

// --- peer.Reporter implementation (debug-only) ---

func (ui *UI) PeerConnected(addr string) {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	ui.active[addr] = &peerState{addr: addr}
}

func (ui *UI) PieceStarted(addr string, index, blockCount int) {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	ps, ok := ui.active[addr]
	if !ok {
		ps = &peerState{addr: addr}
		ui.active[addr] = ps
	}
	ps.pieceIndex = index
	ps.hasPiece = true
	ps.done = make([]bool, blockCount)
	ps.doneCount = 0
}

func (ui *UI) BlockSaved(addr string, blockIndex, size int) {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	ps, ok := ui.active[addr]
	if !ok || blockIndex < 0 || blockIndex >= len(ps.done) {
		return
	}
	if !ps.done[blockIndex] {
		ps.done[blockIndex] = true
		ps.doneCount++
		ps.bytesTotal += int64(size)
		ui.totalBytes += int64(size)
	}
}

func (ui *UI) PieceCompleted(addr string, index int) {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	if ps, ok := ui.active[addr]; ok {
		ps.hasPiece = false
		ps.done = nil
		ps.doneCount = 0
	}
}

// PieceFailed is a no-op: the dedicated Failed Peers panel was retired in
// favor of the Upload panel. Kept to satisfy peer.Reporter.
func (ui *UI) PieceFailed(addr, reason string) {}

func (ui *UI) PeerDisconnected(addr string, err error) {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	delete(ui.active, addr)
}

func (ui *UI) BlockSent(addr string, size int) {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	ps, ok := ui.active[addr]
	if !ok {
		return
	}
	ps.upBytesTotal += int64(size)
	ui.totalUpBytes += int64(size)
}
