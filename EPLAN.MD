# Strip logs from the GUI, add a qBittorrent-style General tab

## Project context (read first)

`torrent-demo` is a Go BitTorrent client with a Wails v2 + React/TypeScript
desktop GUI. It is a **learning side-project, not production code** — favor
minimal-footprint changes over thorough refactors, and do not "clean up" or fix
unrelated things you notice along the way.

Layout:
- Go backend: `internal/{session,peer,peer_manager,piece_manager,metainfo,bencode,tracker_client,message}`
- Wails entrypoint: `main.go` + `app.go` (root), config in `wails.json`
- CLI (unrelated to this work, leave alone): `cmd/torrent-cli/`
- Frontend: `frontend/src/` — **plain global CSS only** (`style.css`), no CSS
  modules, no Tailwind, no component library, no icon library, no state library.
  **Do not add any frontend dependency.**
- Generated Wails bindings (checked in): `frontend/wailsjs/`

Build commands:
- Go: `go build ./... && go vet ./... && go test -race ./...`
- Frontend: `cd frontend && npm run build` (runs `tsc && vite build`)
- Full app: `wails build` → `build/bin/torrent-demo`
- Regenerate TS bindings after changing any bound Go signature/struct:
  `wails generate module`

The backend already fully supports multiple concurrent torrent sessions
(`SessionManager` owns `map[string]*Session`; each `Session` runs its own
`peers.Run` / `refreshPeersLoop` / `progressWatcher` goroutines). `app.go` pushes
the full session list to the frontend once per second via a `sessions:update`
Wails event; the frontend never polls for it.

## Why this change

Two complaints from running the current build:

1. **Logs are eating the UI.** A bottom "All sessions" log pane and a per-session
   "Log" tab display nearly identical lines at the same time, taking ~40% of the
   window for content a normal user never wants. **Remove both from the GUI
   entirely** — logs go to stdout only, and debugging means launching from a
   terminal. Session log lines are already tagged `[session <id>]`, so `grep`
   replaces the per-session tab. (A `-debug` second window was considered and
   rejected: Wails v2 has no real multi-window support.)
2. **The UI looks bland next to qBittorrent.** The diagnosis is mostly a *content*
   gap, not a styling one: qBittorrent's General tab is a dense labeled grid of
   ~20 per-torrent fields (Time Active, ETA, Ratio, Seeds/Peers, Info Hash, Added
   On, Comment…) and this app has no equivalent tab at all. **Add a General tab**
   plus **ETA / Seeds / Ratio table columns**, with the cheap backend tracking
   those require.

Out of scope by explicit decision: wasted/hash-fail byte tracking (nothing counts
it today) and rate limiting (no throttling subsystem exists anywhere).

---

# Part 1 — Remove logs from the GUI

Independently shippable, needs no binding regeneration. Do this first.

### Delete these files
- `frontend/src/components/LogPane.tsx`
- `frontend/src/components/LogList.tsx`

After the edits below, nothing imports either.

### `app.go`
Currently `startup(ctx)` does
`log.SetOutput(io.MultiWriter(os.Stdout, ringBuf))`, where `logRing` is a
~500-line ring buffer type defined in the same file that **also** calls
`runtime.EventsEmit(ctx, "log:line", line)` for every line written.

- Delete the entire `logRing` type, its constructor, and its `Write` method.
- Replace the `log.SetOutput(...)` call with `log.SetOutput(os.Stdout)`.
- Remove the now-unused `io`, `strings`, `sync` imports (`go vet` will catch any
  you miss). Keep `log` and `os`.

> Why explicit `os.Stdout` rather than letting Go default to stderr: the current
> behavior tees to stdout, so `./torrent-demo > run.log` captures everything
> today. Defaulting to stderr would silently break that.

> This removal is a real performance win, not just cleanup — `peer.go` and
> `peer_manager.go` log very frequently, and every line was being JSON-marshalled
> across the Wails IPC bridge for an event that will now have no subscriber.

Leave `sessionLogf` / the `[session <id>]` tagging in `internal/session/session.go`
untouched — it is the whole point of terminal-based debugging now.

### `frontend/src/App.tsx`
Remove: the `MAX_LOG_LINES` const, the `logLines` state, the entire `log:line`
`useEffect`, the `LogPane` import, the `<LogPane .../>` render, and the
`logLines={logLines}` prop passed to `SessionDetail`. Keep the `EventsOn`/
`EventsOff` imports — still used for `sessions:update`.

Update the no-selection hint text from
`"Select a torrent to see its peers and log."` to
`"Select a torrent to see its details."`

### `frontend/src/components/SessionDetail.tsx`
Remove the `logLines` prop from `Props`, the `LogList` import, the `useMemo`
import if it becomes unused, the `filtered` log-filter memo, and the Log tab
button + render branch.

### `frontend/src/style.css`
Delete `.log-pane`, `.log-header`, `.log-header button`, `.log-header
button:hover`, `.log-body`, `.log-line`.

**Keep `.mono`** — `PeerTable` still uses it for addresses, and the new General
tab will use it for the info hash.

**Verify:** `go build ./... && go vet ./...`, then `cd frontend && npm run build`.
Commit here.

---

# Part 2 — Backend data for the General tab

## 2a. `internal/metainfo/metainfo.go` — parse four new keys

```go
type MetaInfo struct {
    // ... existing fields unchanged ...
    Comment      string `bencode:"comment"`
    CreatedBy    string `bencode:"created by"`
    CreationDate int    `bencode:"creation date"`   // int, NOT int64
}

type InfoCommon struct {
    // ... existing fields unchanged ...
    Private int `bencode:"private"`                 // int, NOT bool
}
```

### ⚠️ Hard constraint — this WILL break parsing if you get it wrong

`bencode.fill` in `internal/bencode/decode.go` switches on `reflect.Kind` and
handles only `Int`, `String`, `Slice`, `Struct`. A field typed `int64` or `bool`
falls through to `default: return fmt.Errorf("Unsupported type")`, which would
break parsing of **every torrent carrying those keys** — including all three
sample `.torrent` files in this repo. Use `int` for both, and convert at the
session boundary (`int64(meta.CreationDate)`, `private != 0`).

### Safety notes (already verified, no action needed)
- **Missing keys are safe.** `fill` does `bencodeVal, ok := m[tag]; if !ok
  { continue }`, leaving the zero value. `sn.torrent` has no `private` key and
  must still parse.
- **`InfoHash` cannot be perturbed.** It's computed from the raw byte substring
  via `bencode.ExtractInfoBytes(rawData)`, completely independent of struct shape.
- **`Private` genuinely belongs in `InfoCommon`**, not `MetaInfo` — verified
  against real bytes: `internal/metainfo/testdata/rdr.torrent` contains
  `7:privatei0e` inside the info dict. `InfoCommon` is an anonymous embed in
  `SingleFileInfo`/`MultiFileInfo` and `fill` recurses into anonymous untagged
  fields with the same map, so this works.

### Tests — extend `internal/metainfo/metainfo_test.go`
Assert against `testdata/rdr.torrent`'s known real values:
`Comment == "http://rutor.is/torrent/7809171"`,
`CreatedBy == "Transmission/2.92 (14714)"`, `CreationDate == 1603364761`,
`Private == 0`. Keep a case using a torrent *without* these keys to prove the
missing-key path. The existing `TestNewMetaInfo_InfoHash` pins the hash to a
literal and is your regression guard — it must still pass unchanged.

**Verify:** `go test ./internal/metainfo/ -v`, then `go test ./...`.

## 2b. `internal/peer_manager/peer_manager.go` — exact lifetime byte totals

### ⚠️ Read this before touching this file

`PeerManager` has a documented mutex invariant. `pm.mu` may **only** be locked at
leaf entry points — currently `AddPeer`, `Run`'s tick handling, and `Snapshot`.
It must **never** be locked inside `activate`, `markFailed`, `fillActiveSlots`,
`selectCandidates`, `pieceAvailability`, `evictStallPeers`, `identifyEvictTargets`,
or `evict` — those nest up to three levels deep under `Run` and would deadlock
guaranteed (not probabilistically). Preserve this invariant exactly.

Three additions, all shaped to fit that invariant:

```go
// 1. Exported alias in the existing const block — do NOT rename kMaxParallelPeers
//    (it has 5 call sites); just expose it for the GUI's "n (100 max)" display.
MaxParallelPeers = kMaxParallelPeers

// 2. Two plain uint64 fields on PeerManager.
retiredDown, retiredUp uint64

// 3. Two lines inside evict(), immediately before delete(pm.active, tag).
//    evict() always runs under pm.mu (called from evictStallPeers ← Run's tick
//    case, which locks), so these need no additional synchronization.
//    Note evict() already calls activePeer.conn.GetDownloadedBytes() on the
//    preceding line for its log message.
pm.retiredDown += activePeer.conn.GetDownloadedBytes()
pm.retiredUp   += activePeer.conn.GetUploadedBytes()
```

Plus one new method, structurally identical to the existing `Snapshot()`:

```go
// TotalTransferred returns lifetime bytes for this torrent: bytes still held by
// live connections plus bytes retired by evicted ones. Safe from any goroutine.
func (pm *PeerManager) TotalTransferred() (down, up uint64)
```

It locks `pm.mu`, iterates `pm.active` summing each `conn.GetDownloadedBytes()` /
`GetUploadedBytes()`, adds `retiredDown` / `retiredUp`, and returns. **It calls no
other `PeerManager` method** — that is precisely the property that makes
`Snapshot()` safe today, so this is a fourth leaf lock site of the same shape, not
a new nesting hazard.

### Why exact totals are achievable here
`delete(pm.active, ...)` occurs at exactly **one** place in the file (inside
`evict()`), so the invariant "a peer leaves `active` only through `evict`" holds.
That means `retired` rises by precisely what the active sum drops, making the
total exact and monotonic by construction. Re-activation is safe because
`activate()` builds a brand-new `PeerConnection` starting at zero.

**Critical:** reading `retired` and summing `active` must happen under the *same*
lock hold. Do not split this into a `Retired()` getter that callers combine with
`Snapshot()` — an eviction landing between the two reads would cause a one-tick
dip in a supposedly monotonic counter.

Free bonus: deriving rates from these monotonic totals also fixes an existing bug
where the displayed download rate flickers to zero whenever a peer is evicted.

**Verify:** `go build ./... && go test -race ./internal/peer_manager/ -v`
(`peer_manager_test.go` exists and exercises the lock paths).

## 2c. `internal/session/session.go`

### Immutable fields — set in `NewTorrentSession`

Add to `Session`: `location`, `infoHash` (hex string), `pieceLength`,
`pieceCount`, `comment`, `createdBy`, `creationDate`, `private`, `addedAt`.

These need no lock: they're written before the `*Session` is published into
`SessionManager.sessions`, exactly like the existing `name` / `totalBytes`.

> ### ⚠️ `location` MUST move from `Start()` to the constructor
> It is currently assigned inside `Start()` (`s.location =
> filepath.Join(s.downloadRoot, s.name)`) while `Summary()` reads session state
> from the ticker goroutine. Adding `SavePath` to the summary without moving this
> would introduce a **genuine data race**. Move the `filepath.Join` into the
> `NewTorrentSession` struct literal; `Start()` keeps only the
> `os.MkdirAll(s.location, 0755)`. The value is identical — this is not a
> behavior change — and it has the bonus of showing a real save path while the
> torrent is still Pending, which is what qBittorrent does.

Add a nil-guarded helper beside the existing `computeTotalBytes` /
`getTorrentName` to reach the info dict:
```go
func infoCommon(meta *metainfo.MetaInfo) metainfo.InfoCommon
```

### Mutable fields — guarded by the existing `statusMu`

Add: `seeds`, `totalDownloaded`, `totalUploaded`, `trackerSeeds`,
`trackerLeechers`, `startedAt`, `completedAt`.

### Timestamps

All `int64` Unix seconds, `0` = unset. **No new critical sections needed** — both
mutable timestamps piggyback on locks already taken on exactly the transition
being recorded:

| field | set where | lock |
|---|---|---|
| `addedAt` | `NewTorrentSession` struct literal | none (written pre-publication) |
| `startedAt` | `Start()`, inside the **existing** `statusMu` block that sets `StatusRunning` | already held |
| `completedAt` | `progressWatcher`'s completion branch, inside the **existing** `statusMu` block | already held |

If `Start()` fails before reaching that block, `startedAt` stays 0 — correct.

> Use `int64` Unix seconds rather than `time.Time`: Wails' TS generator emits a
> `convertValues` shim for `time.Time` and the field lands as `any`/string,
> requiring `new Date(...)` parsing plus a zero-check against
> `"0001-01-01T00:00:00Z"`. `int64` maps to a plain TS `number` and `0` is an
> unambiguous sentinel.

### `progressWatcher` changes

It currently sums `DownloadedBytes`/`UploadedBytes` across **active peers only**
from `s.peers.Snapshot()` each tick, feeds `sampleRate(cur, prev)`, and discards
the sums.

- Replace the byte summing with `down, up := s.peers.TotalTransferred()`. Feed the
  existing `sampleRate` from those (its signature is unchanged), and store them
  into `totalDownloaded` / `totalUploaded` under `statusMu`.
- **Keep** the `Snapshot()` loop — it's still needed for counting active/waiting,
  and now also seeds:
  ```go
  if ps.Active && ps.BitfieldFraction >= 1.0 { seeds++ }
  ```
  `bitfieldFraction` explicitly guards `size == 0 → 0`, so `>= 1.0` is exact
  (equal ints divide to exactly 1.0) with no false positives from unsized
  bitfields. Waiting peers are excluded deliberately — their `lastBitfield` is
  stale. Peers that haven't sent a bitfield yet read 0 and simply aren't counted;
  add a one-line comment saying so.

### `findPeers` — capture tracker swarm stats

`AnnounceResponse` carries `Complete` (seeders) and `Incomplete` (leechers). These
are currently logged and then discarded — only `Interval` is consumed.

Track `maxComplete` / `maxIncomplete` plus a `gotStats bool` across the response
loop, then write once **under `statusMu`** after the loop:

```go
if gotStats {
    s.statusMu.Lock()
    s.trackerSeeds, s.trackerLeechers = maxComplete, maxIncomplete
    s.statusMu.Unlock()
}
```

- **Max, not sum.** Trackers report overlapping views of a single swarm; summing
  double-counts and can exceed the real swarm size several-fold. Max is standard
  client behavior.
- The `gotStats` guard prevents a round where every tracker errored from wiping
  good numbers to 0.
- The lock is **mandatory**: `findPeers` runs on the `refreshPeersLoop` goroutine
  (and synchronously inside `Start` for the first call) while `Summary()` reads
  from the `sessionTicker` goroutine.

> **Known limitation, document it in a comment:** `getTrackers` only accepts
> `http(s)://` URLs, and the sample torrents in this repo are overwhelmingly
> `udp://`, so these numbers will often be 0 or up to 3 minutes stale
> (`peerRefreshInterval`). The frontend's `formatSwarm` renders a bare number
> instead of a misleading `(0)`. The real fix is UDP tracker support (BEP 15),
> a separate future project — do not attempt it here.

### `SessionSummary` — 11 → 27 fields

Add: `Seeds int`, `MaxConnections int`, `TotalDownloaded uint64`,
`TotalUploaded uint64`, `TrackerSeeds int`, `TrackerLeechers int`,
`SavePath string`, `InfoHash string`, `PieceLength int`, `AddedAt int64`,
`StartedAt int64`, `CompletedAt int64`, `Comment string`, `CreatedBy string`,
`CreationDate int64`, `Private bool`.

`MaxConnections` comes from `peermgr.MaxParallelPeers`.

In `Summary()`, add a fallback so a Pending torrent shows its real piece count
(the piece manager is nil before `Start`, so `Progress()` returns `0, 0`):
```go
done, total := s.Progress()
if total == 0 { total = s.pieceCount }
```
Leave `Progress()` itself untouched — it's the piece-manager truth.
`SessionTable`'s percentage already guards `TotalPieces > 0`, so 0% still renders
correctly.

**Verify after 2c:** `go build ./... && go vet ./... && go test -race ./...`

---

# Part 3 — Regenerate bindings

```
wails generate module
```

### ⚠️ This is the single most likely place this plan stalls
`frontend/wailsjs/go/models.ts` is checked in, and `npm run build` runs `tsc`.
Skipping this step produces a confusing failure like
`Property 'Seeds' does not exist on type 'SessionSummary'`.

After running it, confirm `models.ts` lists all 16 new `SessionSummary` fields and
that **no `convertValues` shim appeared** (it shouldn't — there's no `time.Time`
in the struct).

---

# Part 4 — Frontend

## 4a. `frontend/src/format.ts` — new helpers

The file currently exports only `formatBytes(n)` and `formatRate(bps)`. Leave
those unchanged and add:

- **`formatDuration(seconds)`** — two largest non-zero units (`2d 4h`, `3h 12m`,
  `5m 3s`, `41s`). Return `'∞'` for non-finite/negative, and also for absurdly
  large values (e.g. > 365d) rather than printing a nonsense number.
- **`formatEta(summary)`** — takes the whole summary so the logic isn't duplicated
  between the table and the General tab. Rules in order:
  `Status !== 'Running'` → `'—'`; `TotalPieces === 0` → `'—'`;
  `remaining = TotalBytes * (1 - DonePieces / TotalPieces)`, `remaining <= 0` →
  `'—'`; `DownloadRate <= 0` → `'∞'`; else
  `formatDuration(remaining / DownloadRate)`. Comment that `remaining` is
  approximate (piece counts scaled by total size, so the short tail piece skews it
  slightly — fine for an ETA).
- **`formatRatio(up, down)`** — `down <= 0` → `up > 0 ? '∞' : '0.00'`; else
  `(up / down).toFixed(2)`.
- **`formatDate(unixSec)`** — `unixSec <= 0` → `'—'`; else
  `new Date(unixSec * 1000).toLocaleString()`.
- **`formatSwarm(connected, swarm)`** — `swarm > 0 ? \`${connected} (${swarm})\`
  : String(connected)`. This is the mitigation for empty tracker stats above.
- **`formatTimeActive(summary)`** — `StartedAt === 0` → `'—'`; end is
  `CompletedAt` when set, else `Date.now() / 1000`; return
  `formatDuration(end - StartedAt)`. Recomputed on every render, which happens
  once per second via `sessions:update` — no timer needed.

## 4b. New `frontend/src/components/GeneralTab.tsx`

Props: `{summary: session.SessionSummary}`. **Purely presentational — no new bound
Go method and no polling.** Every field already arrives on the existing 1s
`sessions:update` push. (This is what makes the General tab cheap; the Peers tab
polls because peer data is not in the summary.)

Define a small local helper in the same file (~6 lines, don't make it a separate
component file):
```tsx
function Field({label, value, wide}: {label: string; value: React.ReactNode; wide?: boolean})
```
rendering `<div className={'field' + (wide ? ' wide' : '')}><span
className="field-label">{label}</span><span className="field-value">{value}</span></div>`.

Structure — two labeled sections, each a 3-column grid:

```tsx
<div className="general-tab">
  <section className="info-section">
    <h3 className="section-title">Transfer</h3>
    <div className="general-grid">{/* Field × N */}</div>
  </section>
  <section className="info-section">
    <h3 className="section-title">Information</h3>
    <div className="general-grid">{/* Field × N */}</div>
  </section>
</div>
```

**Transfer section:** Time active (`formatTimeActive`), ETA (`formatEta`), Status,
Progress (`{pct}%`), Connections (`${ActivePeers} (${MaxConnections} max)`), Seeds
(`formatSwarm(Seeds, TrackerSeeds)`), Leechers
(`formatSwarm(ActivePeers - Seeds, TrackerLeechers)`), Queued peers
(`WaitingPeers`), Downloaded (`formatBytes(TotalDownloaded)`), Uploaded
(`formatBytes(TotalUploaded)`), Down speed (`formatRate`), Up speed
(`formatRate`), Share ratio (`formatRatio(TotalUploaded, TotalDownloaded)`).

**Information section:** Save path (`wide`), Total size, Pieces
(`${TotalPieces} × ${formatBytes(PieceLength)}, have ${DonePieces}`), Info hash v1
(`wide`, with `className="mono"` reusing the existing rule), Added on, Started on,
Completed on, Created on (`formatDate(CreationDate)`), Created by, Privacy
(`Private ? 'Private torrent' : 'Public torrent'`), Comment (`wide`). If
`summary.Error` is non-empty, append one `wide` field styled with
`.field-error`.

## 4c. `frontend/src/components/SessionDetail.tsx`

- `tab` state becomes `'general' | 'peers'`, initial value `'general'`.
- Tab buttons get a unicode glyph + label — **no icon library**, matching the
  toolbar's existing `＋ ▶ ■ ✕` approach:
  ```tsx
  <button className={`tab ${tab === 'general' ? 'active' : ''}`} onClick={() => setTab('general')}>
    <span className="tab-glyph">ℹ</span><span>General</span>
  </button>
  ```
  Glyphs: General = `ℹ` (U+2139), Peers = `⇅` (U+21C5). Both exist in DejaVu Sans
  (the WebKitGTK fallback on Linux/WSL) and Segoe UI. If either renders as tofu,
  fall back to `▤` and `⋮⋮`.
- **Keep the existing `GetPeers` polling `useEffect` unconditional** — do not gate
  it on `tab === 'peers'`. It's a tempting optimization but interacts subtly with
  the `cancelled` flag and the `[s.ID]` dependency. Note it as a possible
  follow-up instead.

## 4d. `frontend/src/components/SessionTable.tsx` — 7 → 10 columns

Use qBittorrent's column order rather than appending to the right:

```
Name | Size | Progress | Status | Seeds | Peers | Down | Up | ETA | Ratio
```

- **Seeds**: `formatSwarm(s.Seeds, s.TrackerSeeds)`
- **Peers**: `formatSwarm(s.ActivePeers - s.Seeds, s.TrackerLeechers)` — connected
  leechers. **This deliberately drops `WaitingPeers` from the table** (it
  currently shows `Active/Waiting`); it moves to the General tab as "Queued
  peers". This was an explicit product decision — keep it.
- **ETA**: `formatEta(s)`
- **Ratio**: `formatRatio(s.TotalUploaded, s.TotalDownloaded)`
- **⚠️ Bump the empty-state row's `colSpan` from 7 to 10.** Easy to miss; it
  silently breaks the "No torrents yet" row's centring.

## 4e. `frontend/src/style.css`

Existing tokens you should reuse: `--bg`, `--bg-panel`, `--bg-row`,
`--bg-row-hover`, `--border`, `--text`, `--text-dim`, `--accent`, `--accent-rgb`,
`--green`, `--yellow`, `--red`, `--gray`, `--space-1..4`, `--radius-sm/md/lg`,
`--shadow-sm/md`.

```css
.general-tab   { flex: 1; min-height: 0; overflow-y: auto;
                 display: flex; flex-direction: column; gap: var(--space-3); }
.info-section  { border: 1px solid var(--border); border-radius: var(--radius-sm);
                 padding: var(--space-2) var(--space-3); }
.section-title { margin: 0 0 var(--space-2); font-size: 12px; font-weight: 600;
                 color: var(--text-dim); text-transform: uppercase; letter-spacing: .04em; }
.general-grid  { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr));
                 column-gap: var(--space-4); row-gap: var(--space-1); }
.field         { display: flex; gap: var(--space-2); min-width: 0;
                 font-size: 12px; line-height: 1.6; }
.field.wide    { grid-column: 1 / -1; }
.field-label   { flex: 0 0 96px; color: var(--text-dim); }
.field-value   { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.field-value.wrap { white-space: normal; word-break: break-all; }  /* info hash, comment */
.field-error   { color: var(--red); }

@media (max-width: 860px) { .general-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
```

Restyle the tab bar as a segmented control (**replaces** the current `.tab` rules):
```css
.tab-bar    { display: flex; gap: 2px; padding: 2px; background: var(--bg);
              border: 1px solid var(--border); border-radius: var(--radius-sm); flex-shrink: 0; }
.tab        { display: inline-flex; align-items: center; gap: var(--space-1);
              background: transparent; border: 1px solid transparent; border-radius: 4px;
              color: var(--text-dim); padding: var(--space-1) var(--space-3);
              font-size: 12px; cursor: pointer; }
.tab:hover  { color: var(--text); }
.tab.active { background: rgba(var(--accent-rgb), .18); border-color: var(--accent);
              color: var(--accent); }
.tab-glyph  { font-size: 13px; line-height: 1; opacity: .9; }
```

Table: add `.session-table td { white-space: nowrap; }` — 10 columns in an 1100px
window will otherwise wrap. `.session-name` already sets its own ellipsis via
`max-width: 340px`; lower that to ~260px to make room.

Optional taste tweak: with the 160px log pane gone, `.table-panel { flex: 1 1 55% }`
/ `.detail-panel { flex: 1 1 45% }` leaves the detail panel roomy. `60% / 40%`
reads better with the fixed-height General grid.

---

# Execution order & verification

Each step builds and runs independently. Do them in order.

0. **Baseline.** `go build ./... && go vet ./... && go test ./...`, then
   `cd frontend && npm run build`. Note anything already failing — **do not fix
   pre-existing breakage**, just don't attribute it to this work.
1. **Logs, backend** (`app.go`) → `go build ./... && go vet ./...`
2. **Logs, frontend** (delete 2 components, strip `App.tsx` / `SessionDetail.tsx`,
   remove log CSS) → `npm run build`. **Shippable; good commit boundary.**
3. **metainfo** (4 fields + tests) → `go test ./internal/metainfo/ -v`, `go test ./...`
4. **peer_manager** (`MaxParallelPeers`, `retiredDown/Up`, `TotalTransferred`) →
   `go build ./... && go test -race ./internal/peer_manager/ -v`
5. **session, immutable fields** (incl. the `location` move, `infoCommon` helper,
   `TotalPieces` fallback) → `go build ./... && go vet ./... && go test -race ./...`
6. **session, mutable fields** (`progressWatcher` rewrite, `startedAt`, tracker
   stats in `findPeers`, `Summary()` wiring) → same commands
7. **`wails generate module`** → confirm the 16 new fields appear in
   `frontend/wailsjs/go/models.ts`
8. **General tab** (`format.ts` helpers, `GeneralTab.tsx`, tab changes, CSS) →
   `npm run build`
9. **Table columns** (+ `colSpan` fix, table CSS) → `npm run build`
10. **End-to-end.** `wails build`, then run `build/bin/torrent-demo` **from a
    terminal** and confirm:
    - Log lines stream to stdout with `[session <id>]` tags, and
      `./build/bin/torrent-demo > run.log` captures them.
    - No log UI anywhere in the window.
    - A Pending torrent's General tab shows save path, total size, pieces, info
      hash, comment, and created-by populated, with `—` for ETA and Time active.
    - After Start: Downloaded / Uploaded / Ratio rise monotonically, ETA counts
      down, Seeds/Peers columns populate.
    - After Stop: totals freeze rather than reset to zero.
    - All five status colors still distinct (Pending/Running/Stopped/Completed/
      Errored).

There are two `.torrent` files in the repo root (`rdr.torrent`, `sn.torrent`) for
manual testing. Note that peer discovery frequently finds nothing because only
HTTP trackers are supported and these files are dominated by `udp://` trackers —
that is a known limitation, **not** a bug introduced by this work.

### Race coverage
`go test -race ./...` will not reach the new `session` code — no test files exist
in `internal/session`. The analytical argument is that every new *mutable* field is
read and written under `statusMu`, and every new *immutable* field is written
before the `*Session` is published into `SessionManager.sessions` — which is
exactly why `location` had to move to the constructor. For empirical coverage,
build once with `go build -race -tags desktop,production -o /tmp/torrent-demo-race .`
and run it against a live torrent for a few minutes after step 6.

---

# Critical files

- `app.go` — log pipeline removal
- `internal/metainfo/metainfo.go` + `metainfo_test.go` — 4 new parsed keys
- `internal/peer_manager/peer_manager.go` — exported const, retired counters,
  `TotalTransferred`
- `internal/session/session.go` — 16 new summary fields, `location` move,
  `progressWatcher` / `findPeers` changes
- `frontend/src/components/GeneralTab.tsx` (new)
- `frontend/src/components/SessionDetail.tsx`, `SessionTable.tsx`
- `frontend/src/format.ts`, `frontend/src/style.css`
- **Deleted:** `frontend/src/components/LogPane.tsx`, `LogList.tsx`

# Explicit non-goals — do not do these

- No wasted / hash-fail byte tracking (nothing counts it in `piece_manager`
  today), no rate limiting (no throttling subsystem exists).
- No UDP tracker (BEP 15) or DHT (BEP 5) support — this is the reason swarm
  numbers are often empty. Agreed future project.
- No column sorting or resizing, no per-file progress view, no drag-to-resize
  splitter, no multi-select or context menu.
- No tab-gated peer polling (noted as a follow-up only).
- **No change to the `PeerManager` mutex design.** `TotalTransferred` deliberately
  reuses the existing leaf-entry-point shape rather than altering the invariant.
  The mutex there is known, deliberately-accepted tech debt tracked separately;
  a channel-based single-owner redesign is planned as future work alongside UDP
  tracker support. Do not start it here.
- No new frontend dependencies of any kind.
