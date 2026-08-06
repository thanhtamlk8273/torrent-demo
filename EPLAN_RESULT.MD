# EPLAN execution result — Wails v2 desktop GUI, multi-session torrents

Status: **implemented and engine-verified; GUI build/launch blocked on system packages** (see "Blocker" below).

## What was done

### 1. `internal/peer_manager/peer_manager.go` — mutex at the 3 entry points + `Snapshot`
- Added `mu sync.Mutex` guarding `waiting`/`active`, with an inline comment carrying the KNOWN-TRAP warning (lock **only** at `AddPeer`, `Run`'s own tick handling, `Snapshot`; never inside the 7 inner map methods).
- `AddPeer`: locked at entry, body unchanged.
- `Run`: locked around the `activeFirstBatch(ctx)` call and around the ticker case's `evictStallPeers()` + `fillActiveSlots(ctx)`; everything else unchanged.
- Added `PeerSnapshot` (plain data: Tag, Active, Address, DownloadedBytes, UploadedBytes, BitfieldFraction), `Snapshot()`, and `bitfieldFraction`.
- Converted `AddPeer`'s stray `fmt.Printf` to `pm.logf` so peer-discovery output reaches the GUI log pipe (same goal as the plan's session.go conversion).
- Choke/interest state intentionally left out of `PeerSnapshot` (plan's fast-follow, not this slice).

### 2. `internal/peer_manager/peer_manager_test.go` — new
The plan's `go test -race` verification is only meaningful if something exercises the maps concurrently (no test existed). Added:
- `TestConcurrentAddPeerAndSnapshot` — 2 goroutines × 2000 iterations of `AddPeer` / `Snapshot` against a fake scheduler/book (no network). This is exactly the shape that used to race (refresh-loop `AddPeer` vs new GUI/progress-watcher `Snapshot`).
- `TestSnapshotWaitingFields`, `TestBitfieldFraction` — snapshot content sanity checks.

### 3. `internal/session/session.go` — constructor/status/progressWatcher/Summary
- `NewTorrentSession(id, torrentPath string, options Options) (*Session, error)` — parses eagerly via `readTorrentFile`, stores `id`, `name`, `totalBytes` (via new `computeTotalBytes` mirroring the existing type-switch). `Start()` reuses `s.meta`.
- `type Status string` (`Pending | Running | Stopped | Completed | Errored`) + `SessionSummary` plain-data struct.
- New `sync.RWMutex`-guarded status block: `status`, `lastErr`, `downloadRate`, `uploadRate`, `activePeers`, `waitingPeers`.
- `progressWatcher(ctx)` goroutine in `Start()` (now `wg.Add(3)`): 1-second sampling of `s.peers.Snapshot()`, byte-sum diffs → rates (clamped at 0 when a sum decreases), active/waiting counts, and `done == total > 0` → `status = Completed` + `s.Stop()` once. Replaces the completion detection that used to live only in the CLI ticker.
- `Stop()` marks `Stopped` inside `stopOnce.Do` unless already `Completed`/`Errored`.
- New accessors: `ID()`, `Status()`, `Peers()` (nil pre-Start), `Summary()`.
- `Start` failure path sets `Errored` + `lastErr` (the only sensible trigger for the `Errored` status the plan defines).
- All `fmt.Printf` tracker/announce logging → `log.Printf` so it flows into the GUI log pane.

### 4. `internal/session/manager.go` — new
`SessionManager` per the plan's exact signature list: registry map behind a mutex that guards only the map; `crypto/rand` 8-byte hex IDs; `Add` parses eagerly and registers `Pending`; `Start(ctx, id)`; `Stop(id)`; `Remove(id)` **rejected while Running** (one-way deletion, matching the plan's rationale); `List()` sorted by name then id for a stable UI; `Peers(id)`.

### 5. `cmd/torrent-cli/main.go` — new (CLI preserved)
Today's `run()` moved verbatim from root `main.go`; the only forced change is the new `NewTorrentSession("cli", file, ...)` `(*Session, error)` signature. `Start`/`Stop`/`Done`/`Wait`/progress ticker untouched.

### 6. Root `main.go` + `app.go` — Wails entry
- `main.go`: `wails.Run(&options.App{..., AssetServer: embed all:frontend/dist, OnStartup, Bind: app})`.
- `app.go`: `App{ctx, mgr}` with `NewApp()` (Options: `downloads`, port 6881); `startup(ctx)` stores ctx, installs `log.SetOutput(io.MultiWriter(os.Stdout, logRing))`, starts the 1-second `sessions:update` ticker. Bound methods: `PickTorrentFile` (native dialog filtered `*.torrent`), `AddTorrent`, `StartSession`, `StopSession`, `RemoveSession`, `ListSessions`, `GetPeers`. `logRing` caps ~500 lines and emits `log:line` per write.
- `go get github.com/wailsapp/wails/v2@latest` → v2.13.0 in go.mod.

### 7. `frontend/` + `wails.json` — scaffolded and implemented
- Scaffolded with `wails init -n torrent-demo -t react-ts` in a scratch dir (inside the workspace, since `/tmp` is ephemeral here), copied only `frontend/` + `wails.json`; scratch removed. React 19 / Vite 7 / TS 5.6 template.
- `src/App.tsx`: `sessions` state fed by `sessions:update` push event + one `ListSessions()` on mount; error banner; selection handling (drops stale selection on removal).
- Components exactly per the plan: `AddTorrentButton` (PickTorrentFile → AddTorrent), `SessionList`/`SessionRow` (name, status badge, progress bar, ↓/↑ rates, active/waiting counts, Start/Stop/Remove buttons gated by status), `SessionDetail` (polls `GetPeers(id)` every 1.5 s while mounted, cleanup cancels), `PeerTable`, `LogPane` (`log:line` event, capped 500 lines, auto-scroll, clear). No state library; plain React state + Wails events.
- `wailsjs/` bindings: the react-ts template ships runtime + a stub `App` binding; replaced with bindings for the 7 real methods and added `go/models.ts` (`session.SessionSummary`, `peermgr.PeerSnapshot`). These were hand-written to match exactly what wails v2.13's generator produces (verified against `internal/typescriptify/typescriptify.go`: a Go `type Status string` field renders as plain `string`; without `json` tags, keys are the Go field names, capitalized). `wails build` will regenerate them identically.
- `.gitignore`: added `frontend/node_modules/` and `frontend/dist/` (the scaffold's own .gitignore was not copied, per plan).

## Verification performed

| Check | Command | Result |
|---|---|---|
| Engine build | `go build ./internal/... ./cmd/...` | OK |
| Vet | `go vet ./internal/... ./cmd/...` | OK |
| Race tests | `go test -race ./internal/... ./cmd/...` | All green; peer_manager concurrent AddPeer/Snapshot test ran (6.0 s) with no races → mutex correctly placed at the 2 externally-called entry points (Run's tick path is exercised only in a live session) |
| Frontend typecheck+build | `cd frontend && npm run build` | tsc + vite clean; `frontend/dist` emitted (satisfies `go:embed all:frontend/dist`) |
| CLI behavior | `go run ./cmd/torrent-cli rdr.torrent` | Parses, announces to 11 HTTP trackers, received 19 peers from `tracker.dler.org` (several trackers DNS-sinkholed by this sandboxed network), then proceeds into peer activation — behavior unchanged from the old CLI, only transport now `log` with timestamps |
| Wails doctor | `wails doctor` | wails v2.13.0; Ubuntu 22.04; `libgtk-3-dev` (3.24.33) and `libwebkit2gtk-4.0-dev` (2.50.4) **Available but not installed** — note: this distro resolves the **4.0** webkit variant, matching EPLAN's stated fallback |
| Root build (stub) | `go build .` | Compiles — but this is wails' **no-tag stub** (`internal/app/app_default_unix.go` returns "Wails applications will not build without the correct build tags" at runtime). The real build needs `-tags production`, which `wails build` supplies |
| Root build (real) | `go build -tags production .` | Fails: `Package gtk+-3.0 was not found` / `Package webkit2gtk-4.0 was not found` in pkg-config — i.e. the missing dev headers |

## Decisions & deviations (all minor, in the plan's spirit)

- **PeerManager test added** — the plan mandates `go test -race ./...` as the mutex check; with no peer_manager test that check was vacuous. The concurrent test makes it real for the two externally-called entry points. (Run's tick lock can only be exercised in a live session.)
- **`AddPeer`'s `fmt.Printf` → `logf`** — same rationale the plan gives for session.go: "diagnostic logs" in the GUI would otherwise silently miss peer discovery.
- **`Start` failure → `Errored`** — gives the plan's `Errored` status its only sensible trigger; CLI behavior unchanged (still returns the error and exits 1).
- **`SessionSummary`/`PeerSnapshot` have no `json` tags** — keys are capitalized Go field names; the hand-written `models.ts` and all frontend code use those exact keys, and wails' generator will reproduce them. Kept Go structs byte-identical to the plan.
- **`List()` sorted by name, then id** — map iteration is random; a stable UI needs deterministic order.
- **`.gitignore` entries added** — without them `git add` would stage ~50k `node_modules` files.
- **Hand-written `wailsjs/`** — the template ships only a `Greet` stub; bindings were written to match wails v2.13 generation rules and will be regenerated (identically) by the first `wails build`.

## ⚠️ Blocker — GUI build/launch needs system packages, sudo is unavailable here

The remaining EPLAN verification steps (`wails build`, launch with `DISPLAY=:0`, render check, and the interactive GUI checks) **cannot run in this container**:

- The user approved `sudo apt install libgtk-3-dev libwebkit2gtk-4.1-dev`, but `sudo` fails here:
  ```
  sudo: The "no new privileges" flag is set, which prevents sudo from running as root.
  ```
  This is a container security setting (uid 1000, non-root, cannot escalate). `wails doctor` confirms the packages are "Available" but not installed, and `go build -tags production .` fails at pkg-config for `gtk+-3.0` / `webkit2gtk-4.0`.

**To unblock (needs an environment with root — host WSL shell or a rebuilt container):**
```
sudo apt install -y libgtk-3-dev libwebkit2gtk-4.0-dev
```
(On Ubuntu 22.04 the resolvable package is `libwebkit2gtk-4.0-dev` — the plan's documented fallback for `4.1`.)

Then, from the repo root:
```
cd frontend && npm run build && cd ..
wails build          # adds -tags production; regenerates frontend/wailsjs + embeds frontend/dist
DISPLAY=:0 ./build/bin/torrent-demo   # (or ./torrent-demo) and confirm the window renders
```

**Then complete the manual GUI verification** (needs a human at the file picker):
1. Add `rdr.torrent` and `sn.torrent` via the picker → both appear immediately as Pending with correct name/size/piece counts, without starting either.
2. Start one, leave the other Pending → independent status/peer counts/rates; start the second → both progress independently.
3. Open a running session's detail view → peer table populates/updates; closing stops polling.
4. `RemoveSession` rejected while Running, succeeds once Stopped.
5. Let a small torrent finish → status auto-flips to Completed via `progressWatcher` (no manual Stop).
6. `go run ./cmd/torrent-cli rdr.torrent` → unchanged CLI behavior (verified above up to the network limits of this sandbox).

## Known limitations (per plan, not this slice)
- PeerManager's mutex is flagged tech debt; the channel-based single-owner design is the documented target (needs `ctx` threading through `Snapshot`/`Peers`).
- Choke/interest not in `PeerSnapshot` (needs `GetChoked`/`GetInterested` fast-follow).
- One shared log pane; per-session log tagging deferred.
- `Start()` not safely re-callable after `Stop()` → `Remove` is one-way, Start button only for Pending sessions.
- Adding the same torrent name twice would make two sessions write to the same `downloads/<name>/` dir (not addressed by the plan).

---

# Second pass — Better-looking, properly multi-session Wails GUI

Status: **implemented and verified — `wails build` succeeded and the GUI renders on the WSLg display.**

## 1. Backend: session-tagged logs (`internal/session/session.go`)

- Added `sessionLogf(id, format, ...)` (package-level, usable from `readTorrentFile` before a `*Session` exists) and `(s *Session) logf`, mirroring `internal/peer/peer.go`'s pattern. Every `log.Printf` in `refreshPeersLoop`/`findPeers`/`readTorrentFile` now emits `[session <id>]`-prefixed lines.
- `readTorrentFile(file)` → `readTorrentFile(id, file)`; its one caller (`NewTorrentSession`) passes the id it already has in scope.
- Stripped the leading `\n` from the 3 affected format strings (`Announcing to…`, `[tracker]`, `Found N peer(s)…`) so the prefixed lines don't produce `[session x] `-only stray lines in app.go's log splitter. Trailing `\n`s kept (the splitter drops the empty tail element).
- No changes to `internal/peer`, `internal/peer_manager`, or `app.go`.
- Verified: `go build ./internal/... ./cmd/... && go vet ./internal/... ./cmd/... && go test -race ./internal/... ./cmd/...` all green.

## 2. Frontend: single-owner log stream

- `App.tsx` now owns the `log:line` subscription (one `useEffect`, capped at 500, using the cancel function `EventsOn` returns) and passes `logLines` down as a prop. `LogPane` no longer subscribes.
- New `LogList.tsx`: presentational `{lines, emptyHint}` body (auto-scroll, `.log-line` mapping, empty-state hint) shared by the global pane and the per-session tab.
- `LogPane.tsx` is prop-driven with a local `clearedAt` watermark — "Clear" hides lines locally without mutating the shared array. Header relabeled "All sessions".
- Note: the shipped `runtime.d.ts` from the scaffold typed `EventsOn` as `void`; after `wails build` regenerated it, it correctly returns `() => void` — the temporary cast was removed.

## 3. Master-detail layout

- `App.tsx` renders `.main-layout` → `.sidebar` (SessionList, fixed 300px, `flex:1; min-height:0` so it scrolls) + `.detail-panel` (SessionDetail, or a "Select a torrent…" placeholder). The bottom `LogPane` stays below, so the global log and per-session tabs coexist on the same data.
- `.session-list`'s `overflow-y: auto` is now actually bounded by the sidebar.

## 4. Tabs + per-session log filter (`SessionDetail.tsx`)

- New `tab` state (`'peers' | 'log'`), not reset when switching sessions. Heading dropped the hardcoded "— peers" suffix.
- Log tab renders `logLines.filter(...)` through the shared `LogList`; empty hint "No log lines for this session yet."
- **Deviation from the plan (documented):** the plan's `l.startsWith('[session <id>]')` can never match — every log line carries the `log` package's timestamp prefix (`2026/08/06 … [session x] …`). Used `l.includes('[session <id>]')` instead; still collision-free (ids are 16-hex) and stays within the plan's "no app.go changes" scope.

## 5. Header aggregate stats (`App.tsx`)

- `sessions.reduce` → `{running, down, up}`; rendered as a third `.app-header` child: `N running / M total · ↓ x/s · ↑ y/s` via `formatRate`. Purely client-side.

## 6. Visual design pass (`style.css`)

- Design tokens added (`--space-1..4`, `--radius-sm/md/lg`, `--accent-rgb`, `--shadow-sm/md`).
- Session rows: `radius-md` + `shadow-sm`, hover lifts (`shadow-md`, `translateY(-1px)`), selected gets an accent ring (`0 0 0 2px rgba(var(--accent-rgb), .35), var(--shadow-md)`).
- `.session-detail` and `.log-pane`: `radius-md` + `shadow-sm`.
- Status badges now all distinct — `pending` = yellow `--yellow` (finally used) with dark text for contrast, `running` = accent, `stopped` = gray, `completed` = green, `errored` = red.
- Scroll fixes: `.peer-table` and `.log-body` gained `flex:1; min-height:0` so both actually scroll inside their flex panels (the plan asserted the peer table "already scrolls internally" — it was being clipped by `overflow:hidden`; one-line CSS fix).

## Verification

- `go build ./internal/... ./cmd/... && go vet ./internal/... ./cmd/... && go test -race ./internal/... ./cmd/...` — green.
- `cd frontend && npm run build` (tsc + vite) — clean at each wiring point; final CSS 5.49 kB.
- `wails build` (wails v2.13.0) — full production build **succeeded in ~3-5 s**, binary at `build/bin/torrent-demo` (10.5 MB); regenerated `wailsjs/` confirmed identical to the hand-written bindings (7 methods, capitalized model keys, `EventsOn` → `() => void`).
- **Launched under DISPLAY=:0 via the WSLg X socket** (the container had no `/tmp/.X11-unix` — bridged with `ln -sf /mnt/wslg/.X11-unix/X0 /tmp/.X11-unix/X0`; `GDK_BACKEND=x11` is what wails forces, and GTK's Wayland auto-detection isn't used). Window `0x60000e "torrent-demo"` at exactly 1100x760, WebKitWebProcess running, process stable.
- **Pixel-verified rendering** (XGetImage + PIL, `gui-screenshot.png` in the repo): 665 distinct colors; dominant colors exactly the theme (`#1b2632` bg, `#232f3e` panel, `#3a4a5e` border, `#4f9cf9` accent); accent button bbox x 966-1087 / y 12-43 (header top-right); bottom log-pane band and sidebar/detail regions present. Master-detail layout renders correctly.
- The app is left **running** (background job) on the WSLg display so the remaining interactive checks can be done by hand.

### Remaining manual checks (need a human at the file picker / real torrents)
1. Add several `.torrent` files → sidebar scrolls independently while header + detail stay fixed.
2. Select sessions: Peers tab polls (unchanged `GetPeers` effect keyed on `s.ID`); Log tab shows only `[session <id>]` lines; bottom "All sessions" pane interleaves everything; Clear works independently in both.
3. Cycle Pending → Running → Completed/Errored/Stopped → all five badges visually distinct.
4. Header stats update live as sessions start/stop/progress.

---

# Third pass — qBittorrent-style layout (table + toolbar + stacked detail)

Status: **implemented and verified — `wails build` succeeded, GUI relaunched on the WSLg display and pixel-verified.**

Sections 1 (session-tagged logs), 2 (single-owner log stream), 5 (header stats) and the design tokens from section 6 were already in place from the previous pass — this pass delivered the structural change (sections 3, 4) plus the table/toolbar styling.

## 3. Session table + action toolbar

- **New `SessionTable.tsx`** replaces `SessionList.tsx` + `SessionRow.tsx` (both deleted). One full-width table: `Name | Size | Progress | Status | Down | Up | Peers`.
  - Sticky `<thead>` (reusing the `.peer-table th` pattern), `z-index: 1` so rows scroll under it.
  - Row `onClick` selects; `.selected` = tinted background `rgba(var(--accent-rgb), .18)` (qBittorrent-style solid highlight instead of the old border-only).
  - Progress cell: 16px `.progress-track`/`.progress-fill` with the percentage absolutely centered on top (`position: relative` cell + `.progress-label` with text-shadow).
  - Status = plain colored text via `.status-text` + the 5-way color map (no pill badge).
  - No per-row buttons. Empty state = one `colSpan=7` hint row.
- **Toolbar** replaces the old `<header className="app-header">` in `App.tsx`: `＋ Add`, `▶ Start`, `■ Stop`, `✕ Remove` (unicode glyphs, no icon library) acting on `selectedId`, with the aggregate stats line pushed right (`margin-left: auto`). Gating: Start enabled only for `Pending`, Stop only for `Running`, Remove only when not `Running`, all disabled with no selection (`:disabled { opacity: .45 }`).
  - **Decision (documented):** kept the existing per-row gating exactly as `SessionRow.tsx` had it — Start is enabled for `Pending` only, NOT for `Stopped`, because the engine's `Start()` isn't safely re-callable after `Stop()` (per the earlier plan); the EPLAN's "Pending/Stopped" example was loose.
- `AddTorrentButton.tsx` deleted — the Add action (PickTorrentFile → AddTorrent) is inlined as the toolbar's first button.

## 4. Detail panel below the table

- `.main-layout` flipped to a column: `.table-panel { flex: 1 1 55%; min-height: 0; overflow-y: auto }` + `.detail-panel { flex: 1 1 45% }` — each scrolls independently inside its own flex box; no drag-to-resize splitter (non-goal).
- `SessionDetail` (unchanged from last pass): `peers|log` tabs kept across selections, `logLines` prop, per-session filter via `includes('[session <id>]')` (timestamp-prefix deviation already documented), heading without the "— peers" suffix.

## 6. Table + toolbar styling (`style.css`)

- Added `.toolbar` (bg-panel, radius-md, shadow-sm), `.toolbar-button` (bg contrast on the panel toolbar, hover accent, disabled fade), `.session-table` (sticky thead, row hover, tinted `.selected`), `.progress-cell` (+16px track, centered label), `.status-*` colors (pending=yellow, running=accent, stopped=gray, completed=green, errored=red).
- Removed the dead card styles: `.app-header`, `.add-button`, `.session-list`, `.session-row*`, `.session-main`, `.session-title`, `.status-badge`, `.session-meta`, `.session-rates`, `.session-actions`, `.session-error`. `.session-name` kept (now the Name column cell, ellipsized). `.log-pane` height trimmed 180→160px.

## Verification

- `go build ./internal/... ./cmd/... && go vet ./internal/... ./cmd/... && go test -race ./internal/... ./cmd/...` — green (backend untouched this pass).
- `cd frontend && npm run build` (tsc + vite) — clean (37 modules after the component swap).
- `wails build` — full production build succeeded in ~4.7 s; binary at `build/bin/torrent-demo`.
- Relaunched under DISPLAY=:0 (WSLg socket bridge); window `0x60000e` at 1100x760; **pixel-verified the new layout**: 681 distinct colors; row profile shows toolbar band (y≈20, panel + button borders), sticky table header (y≈75, panel + dim header text), empty table/detail areas (y≈200/420, bg), bottom log pane (y≈640/700, panel). `gui-screenshot.png` refreshed.
- App left running (background job) for interactive checks.

### Remaining manual checks (need a human at the file picker)
1. Add several `.torrent`s → table rows with aligned columns; table scrolls independently while toolbar + detail stay fixed.
2. Click a row → tinted highlight + detail panel updates; toolbar buttons enable/disable by the selected row's status.
3. Peers tab polls; Log tab shows only that session's lines; bottom "All sessions" pane interleaves everything; Clear independent in both.
4. Cycle Pending → Running → Completed/Errored/Stopped → five status-text colors distinct.
5. Progress cell shows the percentage centered on the bar; header stats update live.

---

# Fourth pass — strip logs from the GUI, add qBittorrent-style General tab

Status: **implemented and verified** — full production `wails build` succeeded, app
relaunched on the WSLg display, pixel-verified (no log UI), backend empirically
race-tested end-to-end with a local fake tracker.

## Part 1 — Logs removed from the GUI (shippable boundary)

- `app.go`: deleted the `logRing` type/constructor/`Write`; `log.SetOutput`
  now targets `os.Stdout` only (so `> run.log` still captures); dropped
  `io`/`strings`/`sync` imports. Performance note from the plan holds: no more
  per-line JSON marshalling across the IPC bridge.
- Deleted `frontend/src/components/LogPane.tsx` + `LogList.tsx`.
- `App.tsx`: removed `MAX_LOG_LINES`, `logLines`, the `log:line` effect,
  the `<LogPane/>` render and the `logLines` prop; hint text now "Select a
  torrent to see its details." `SessionDetail.tsx`: dropped the log tab /
  `useMemo` filter / `LogList` import (reduced to the peers table until
  Part 4 re-added tabs).
- `style.css`: deleted `.log-pane`, `.log-header(…)`, `.log-body`, `.log-line`;
  `.mono` kept (now also used by the info-hash field).
- Verified: zero `log:line`/`logRing`/`LogPane`/`LogList` references anywhere;
  built CSS contains no `.log-*` rules. Pixel check of the running window shows
  the bottom log band is gone.

## Part 2 — Backend data

### 2a. metainfo (4 keys, `int` not `int64`/`bool` — per the bencode.fill constraint)
`MetaInfo.Comment/CreatedBy/CreationDate`, `InfoCommon.Private`. Tests pass
against real bytes. **Deviations from the plan's literals (real data wins):**
- `rdr.torrent` comment is `http://rutor.is/torrent/780917` (plan said
  `…7809171` — one digit off).
- `rdr.torrent` and `sn.torrent` carry **no** `private` key in the info dict
  (the `private` match the plan referenced is top-level `private_properties`),
  and `sn.torrent` **has** comment/created-by/creation-date. The missing-key
  fixture is `try.torrent` (copied into `testdata/`); it does carry a comment,
  so the missing-key test asserts the three truly-absent keys.
- `TestNewMetaInfo_InfoHash` regression pin: unchanged and green.

### 2b. peer_manager
`MaxParallelPeers = kMaxParallelPeers` (alias only), `retiredDown/retiredUp`
fields, the two retirement accumulations in `evict()` before
`delete(pm.active, tag)`, and `TotalTransferred()` as a fourth leaf-lock
method (locks `pm.mu`, sums active conns + retired, calls no other method).
Mutex invariant untouched; `go test -race ./internal/peer_manager/` green.

### 2c. session
- Immutable (constructor, pre-publication): `location` **moved out of
  `Start()`** into `NewTorrentSession` (avoids the Summary race; Pending shows a
  real save path), `infoHash` (hex), `pieceLength`, `pieceCount`,
  `comment`, `createdBy`, `creationDate` (int64), `private`, `addedAt`.
- Mutable (under `statusMu`): `seeds`, `totalDownloaded`, `totalUploaded`,
  `trackerSeeds`, `trackerLeechers`, `startedAt`, `completedAt`.
- `progressWatcher`: rates from `TotalTransferred()` (monotonic — fixes the
  eviction flicker), seeds via `BitfieldFraction >= 1.0` on active peers,
  totals + `completedAt` under `statusMu`.
- `findPeers`: max (not sum) complete/incomplete across the response loop,
  written once under `statusMu` with a `gotStats` guard; known-limitation
  comment (http-only trackers, sample torrents mostly udp) included.
- `SessionSummary`: 11 → 27 fields; `Summary()` falls back to `pieceCount`
  when `Progress()` returns 0 (Pending display).
- **Empirical race coverage:** temporary `session_e2e_verify_test.go` (deleted
  after the run) drove a real session end-to-end against a local fake HTTP
  tracker under `-race`: Pending summary (SavePath/hash/pieces/metadata/
  Private/timestamps/MaxConnections=100), Start → Running + `startedAt` +
  `TrackerSeeds/Leechers` 1/3 from the fake announce, watcher ticks through
  `TotalTransferred`, Stop → totals frozen, clean shutdown. All passed, no
  races. (Left no test files behind, matching the repo's existing layout.)

## Part 3 — Bindings
`wails generate module`: all 16 new `SessionSummary` fields in `models.ts`,
zero `convertValues` shims. (The step the plan flagged as the likely stall
point passed first try.)

## Part 4 — Frontend
- `format.ts`: `formatDuration` (two largest units, `∞` guards),
  `formatEta` (running/pieces/remaining/rate rules), `formatRatio`,
  `formatDate`, `formatSwarm` (bare number when tracker is empty — the
  documented mitigation), `formatTimeActive` (recomputed per render, no timer).
- `GeneralTab.tsx` (new): local `Field` helper (+`wide`/`wrap`/`mono`),
  Transfer section (time active, ETA, status, progress, connections n (100
  max), seeds/leechers via `formatSwarm`, queued peers, downloaded/uploaded,
  down/up speed, share ratio), Information section (save path, total size,
  pieces `N × size, have M`, info hash v1 mono, added/started/completed/created
  on, created by, privacy, comment; `field-error` for `summary.Error`).
  Purely presentational — rides the 1s `sessions:update` push.
- `SessionDetail.tsx`: segmented tab bar, `'general' | 'peers'` (initial
  general), glyphs `ℹ`/`⇅`; `GetPeers` polling kept unconditional (noted as
  follow-up, per plan).
- `SessionTable.tsx`: 10 columns in qBittorrent order
  `Name|Size|Progress|Status|Seeds|Peers|Down|Up|ETA|Ratio`; empty-state
  `colSpan` bumped 7 → 10; WaitingPeers dropped from the table (moved to
  General "Queued peers", per the product decision).
- `style.css`: General-tab grid (3-col, 2-col < 860px), segmented tab control,
  `td` nowrap, `.session-name` max-width 260px, 60/40 split.

## Verification summary
- `go build ./... && go vet ./...` clean at every step.
- `go test -race ./...` green (incl. new metainfo tests + peer_manager).
- `cd frontend && npm run build` clean after each frontend step.
- `wails build` → `build/bin/torrent-demo` (3.9s).
- Launched under DISPLAY=:0 (WSLg socket bridge): window 1100x760 renders;
  pixel row-profile: toolbar (y≈20), sticky table header (y≈75), plain bg below
  — **no log pane anywhere** (previously panel-colored at y≈640/700).
  `gui-screenshot.png` refreshed.
- `./build/bin/torrent-demo > run.log` captures output (verified; `run.log`
  left in the repo root while the app runs).

## Remaining manual checks (need a human at the file picker)
The app is currently running. Add `rdr.torrent` / `sn.torrent` / `try.torrent`
via **＋ Add**, then:
1. Pending torrent's General tab: save path, total size, pieces
   `N × size, have M`, info hash, comment, created-by populated; ETA and Time
   active show `—`.
2. **Start**: Downloaded/Uploaded/Ratio rise monotonically, ETA counts down,
   Seeds/Peers columns populate (likely sparse — udp tracker limitation).
3. **Stop**: totals freeze rather than reset.
4. `tail -f run.log` shows `[session <id>]`-tagged lines streaming to stdout.
5. All five status colors distinct; both tabs switch; glyphs `ℹ`/`⇅` render
   (not tofu) in WebKitGTK.

## Deviations from the plan
- Plan-literal fixes in 2a (comment value, missing-key fixture).
- The plan's "Commit here" boundary was skipped: the tree already carries all
  prior uncommitted work from earlier passes, so committing "Part 1 only" isn't
  possible without sweeping unrelated changes in. Left to the user.
- Session backend verified via a temporary test (created, run, deleted) instead
  of the plan's `-race` GUI run, which needs the interactive file dialog.
- `Field` gained a `mono` prop for the info-hash value (the plan's Field helper
  didn't have it; needed by the plan's own "className=mono" requirement).
