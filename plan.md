# torrent-demo — roadmap

A for-fun BitTorrent client, built to *understand* the protocol, not to ship a product.
Plumbing (debug UI, tests, frontend, scaffolding) gets delegated; the focus is the core
mechanisms. Rough effort = "focused sessions" (~one solid evening each).

## Done
- bencode parser, message wire format, tracker client (HTTP), metainfo
- PieceManager + per-piece scheduler (claim/unclaim/complete), file layout
- TCP peer protocol: handshake, bitfield, piece download + SHA1 verify
- Multi-peer downloading with a bounded active pool and waiting-peer address book
- Block request pipelining (currently 8 in flight per peer)
- Basic upload/seeding path (`Bitfield`, `Have`, `Unchoke`, `Request` → `Piece`)
- Startup recheck/resume by hashing existing payload pieces
- Debug-only tview dashboard with live download/upload speed
- Unit tests across the core packages (`go test ./...` passes after the exploratory
  implementation was reverted)
- **Real-world end-to-end validation:** downloaded a 13,674,576,509-byte,
  6,521-piece payload and independently rehashed every piece against the `.torrent`;
  6,521/6,521 matched with zero corruption

## Core roadmap → v1.0
This is the line for "I actually understand BitTorrent." Stop here.

1. **Peer I/O + pipelining**
   - [x] Keep multiple block requests in flight per peer (currently 8).
   - [ ] Restore the experimentally validated 64-request window. A real-swarm run
     improved from roughly 2–3 MB/s to 7–10 MB/s; 128 requests added little over 64.
   - [ ] Serialize every socket write through one owner/channel; current leecher,
     seeder, and keepalive paths can write concurrently.
   - [ ] Make write failures terminate the connection and release claimed work.
   - [ ] Later optimization: let one connection pipeline blocks across two or more
     pieces so its request window does not drain at every piece boundary. Do not
     block the first functional GUI on this.
2. **Rarest-first piece selection**
   - [x] Calculate piece availability and produce a rarest-first ranking.
   - [ ] Connect that ranking to the scheduler that actually claims work; `GetWork`
     still performs a linear first-available scan.
   - [ ] Keep availability snapshots fresh from active peers without exposing their
     mutable internals to the manager.
3. **Endgame mode**
   - [ ] When only a small number of blocks remain, allow duplicate block requests.
   - [ ] Accept the first valid response and send `Cancel` to the losing peers.
   - [ ] Prove the final piece cannot remain stranded behind a dead/slow peer.
4. **Choking / tit-for-tat + seeding**
   - [x] Serve valid block requests for completed pieces.
   - [x] Announce existing/new pieces with `Bitfield` and `Have`.
   - [ ] Implement reciprocation, upload-slot limits, and optimistic unchoke.
   - [ ] Exercise the upload path between two instances of this client.

## Current direction — right shape + functional GUI

Freeze throughput tuning after restoring the measured 64-request baseline. In the
real swarm, 32 → 64 requests per peer produced a large gain (roughly 7–10 MB/s
overall), while 64 → 128 produced little additional value. The exploratory code was
reverted after recording the result, so the current source is back at 8. The
remaining gap with mature clients is understood as future work (multi-piece request
pipelining, better peer admission/quality, more transports/extensions), not a reason
to keep min-maxing `peer.go` before the application has the right outer shape.

- [ ] Introduce a small `TorrentSession` boundary that owns the PieceManager,
  PeerManager, lifecycle, and progress aggregation.
- [ ] Expose commands such as start/stop (then pause/resume/remove when their
  semantics are real) without exposing manager queues, bitfields, or goroutines.
- [ ] Expose immutable session and peer snapshots suitable for both the debug TUI
  and a GUI.
- [ ] Build the first desktop frontend with Wails v2 + React + TypeScript.
- [ ] First GUI slice: open a `.torrent`, start/stop it, show overall progress and
  download/upload rates, active peer count, peer table, and diagnostic logs.
- [ ] Keep the GUI dependent only on the session command/snapshot API so later
  scheduling and peer-protocol improvements remain internal.

Do not resume throughput tuning unless a correctness issue appears or a measured
engine limitation blocks the GUI/session design.

## Next-session handoff — findings from the reverted experiment

The implementation used to obtain these results was intentionally reverted. Rebuild
the useful parts in small, reviewed slices instead of restoring the experiment as one
large patch.

### Small fixes worth restoring first

- [ ] Set the per-peer request window from 8 to **64** and keep it there unless a
  repeatable benchmark disproves the real-swarm result. Do not use 128 by default.
- [ ] Announce the real tracker `left` value from verified local pieces, including
  the short final piece. This requires constructing/rechecking `PieceManager` before
  announcing. Add visible verification progress or persisted resume state because
  hashing a 13.7 GB payload otherwise looks like a startup hang.
- [ ] Generate an honest `-TD0001-` peer ID with random per-process suffix bytes.
  Spoofing qBittorrent (`-qB5000-`) changed the connected count by only one peer,
  which is normal run-to-run variance; unknown client identity was not the bottleneck.
- [ ] Make the wire reader consume and ignore unsupported message payloads while
  keeping the stream aligned, with a bounded payload size (the experiment used
  1 MiB). A live run observed **zero** unsupported messages, so treat this as protocol
  robustness, not a peer-count fix.
- [ ] Keep peer telemetry as monotonic downloaded/uploaded byte counters. Calculate
  rates centrally from counter deltas and sample time; peers should not publish
  independently calculated instantaneous rates.
- [ ] Clone bitfields at ownership/snapshot boundaries. Shallow copies are fine only
  while the shared backing bytes are provably immutable.

### PeerManager policy to implement deliberately

- [ ] Do not permanently remove a known peer merely because it reached a failure
  count. Keep its profile and use exponential retry backoff capped at **5 minutes**.
- [ ] Give a newly active peer an approximately **3-minute proof/grace window**,
  then revisit usefulness every **1 minute**. A 15-second manager tick is adequate
  scheduling resolution; do not turn each condition into another stored peer state.
- [ ] New peers and peers with no trustworthy bitfield need an exploration path.
  Treat their availability optimistically for admission (conceptually all ones),
  and allow a stale last-known bitfield to become speculative again no more often
  than about every **5 minutes**. PeerManager owns this refresh policy; PieceManager
  remains responsible for rarest-first piece ranking/claiming.
- [ ] Deduplicate admission candidates, enforce the active capacity while admitting,
  and make connection replacement atomic from the manager loop.
- [ ] Identify duplicate live connections by the handshake peer ID. If the same peer
  connects again, replace only the connection and reset connection-local observations;
  retain the peer profile/history. Never do the inbound handshake inside the manager
  event loop.
- [ ] Prefer terminal connection events over the temporary `IsStopped` polling used
  in the experiment, but keep the manager model small: active/waiting plus retry
  metadata, not a large peer state machine.

### Inbound/upload path to rebuild

- [ ] Add a configurable listen/announce port (the experiment used 6881).
- [ ] Accept TCP connections in a listener goroutine; perform each BitTorrent
  handshake in a short-lived worker with a deadline and a bounded number of pending
  handshakes (the experiment used 32), then send only validated connections and their
  remote peer IDs into the manager channel.
- [ ] Reject self-connections and close rejected/abandoned sockets on every path.
  Exercise duplicate-peer replacement and cancellation under the race detector.
- [ ] Test seeder first, wait briefly for it to listen, then start the leecher. Local
  testing proved the upload code can unchoke and serve blocks. Public inbound traffic
  still requires the OS firewall and router/NAT to allow/forward the chosen port;
  successful outbound downloading/uploading does not prove inbound reachability.

### Measurements to remember

- The test torrent exposed about 188 tracker candidates, but one observed run produced
  only 16 successful handshakes and 15 active peers. Of 93 pre-handshake failures:
  77 timed out, 10 were refused, 3 ended with EOF, 2 reset, and 1 was other.
- Tolerating unsupported messages did not increase the live count in that run.
- qBittorrent's larger count/rate is therefore more plausibly connection quality,
  inbound reachability, protocol/transports, and scheduling maturity than a simple
  client-ID rejection.
- One connection currently owns one `currentPiece`. Near a piece boundary its request
  window drains; mature clients pipeline blocks from multiple pieces on one TCP
  connection. Park this optimization until after `TorrentSession` and the first GUI.
- Multiple peers downloading the same block/piece belongs in endgame mode, not normal
  throughput tuning.

## Stabilization gate

Do the lifecycle-critical parts while shaping the session boundary. The engine has
demonstrated correct bytes at scale; now make lifecycle and completion behavior
deterministic. The first GUI does not need to wait for every rarest-first, endgame,
or tit-for-tat improvement, but it must not reach through the session API into peer
internals.

- [ ] **Peer lifecycle events:** a peer owns `dialing → handshaking → running → dead`
  and emits exactly one terminal event (`ConnectionFailed` or `ConnectionClosed`)
  with its final immutable snapshot.
- [ ] **Manager pool invariants:** the manager owns only `waiting/in-pool/backoff/drop`;
  every active entry has a non-nil connection, every death/eviction removes it from
  `active`, and `len(active)` never exceeds the cap.
- [ ] **Immediate slot recovery:** failed dial, handshake failure, EOF, and write
  failure free their slot immediately instead of waiting for a polling eviction tick.
- [ ] **Central triage/backoff:** one policy decides retry delay versus drop using
  death kind, failure count, usefulness, and last-known bitfield.
- [ ] **Candidate correctness:** deduplicate selected peer tags, stop selection when
  capacity is reached, include unknown peers for exploration, and never activate a
  missing waiting-map entry.
- [ ] **Usefulness-based eviction:** do not equate zero recent traffic with a bad peer;
  retain choked peers that advertise needed pieces and evict only under capacity
  pressure or explicit liveness failure.
- [ ] **Snapshot boundary:** expose immutable stats/bitfield/activity snapshots; the
  manager must not inspect or mutate leecher/seeder protocol state.
- [ ] **Claim cleanup:** every terminal peer path releases incomplete piece/block
  claims exactly once.
- [ ] **Shutdown hygiene:** stop tickers, close/unblock socket goroutines, make
  shutdown idempotent, and wait for owned goroutines where required.
- [ ] **Tests stay green through the refactor:** add manager
  transition/invariant tests and keep `go test ./...` passing.
- [ ] **Concurrency validation:** pass `go test -race ./...` and repeated
  connect/fail/EOF/cancel stress tests without races, leaks, deadlocks, or stuck slots.
- [ ] **Tail test:** repeatedly finish a controlled swarm from “few pieces left” and
  record bounded completion time; use this as the acceptance test for rarest-first +
  endgame.

### Stabilization exit criteria

- All tests and race tests pass.
- Dead peers vacate slots promptly and retries obey one documented policy.
- A completed payload survives an independent full piece-hash recheck.
- The last-piece/endgame test completes reliably across repeated runs.
- The engine exposes a small session-facing snapshot/command API suitable for a GUI,
  without leaking peer internals.

At that point the single-download engine is a credible v1 core. The functional GUI
can already exist on the stable session boundary; multiple simultaneous downloads
remain a later layer.

**Ownership debt — NON-NEGOTIABLE:** after the peer rewrite + PeerManager, rewrite
`UnmarshalBencode` (internal/tracker_client/client.go) by hand. It's the one piece of
*core* logic written with LLM help that I don't fully own (the `any` type switch on the
compact/list peer forms). Rewrite it myself from understanding — do NOT delegate this one.
This is the whole point: keep the hard, model-building slices mine.

→ **After core: stop and take a breather. Switch to the "LLM loader" project** (load a
small model, run a forward pass by hand — same "build the toy to feel the mechanism"
instinct as this whole project).

## PeerManager state design — decided 2026-07-19, revisit this week

**Diagnosis of the "state soup":** the pain came from mixing two layers into one state
machine — *liveness* (present tense, only exists while a socket exists) and
*failure/retry history* (past tense, only matters between connections). One `status`
enum + the manager peeking into live connections (`isStuckInConnecting`) put both in
one place; every edge case (nil conn Shutdown, `nbActivePeers` drift, blocked
commandChan) lives at that seam.

### Decisions

- **Connection owns everything about its own life, except retry.** Dial, handshake,
  and a probation window ("produce handshake + first signal within X or die") are all
  internal deadlines. A connection can't be "stuck" — stuck *is* dead. Delete
  `isStuckInConnecting`; the manager never inspects a live connection's internals.
- **Crash-only, one chance.** A connection never reconnects or repairs itself. It gets
  one shot to prove itself with metrics (download/upload speed, bitfield). Anything
  weird → die. Recreating is the manager's decision, later, with a *fresh* connection.
- **Death carries one bit of context:** failed-before-handshake (aggressive backoff →
  drop) vs closed-after-connected (was real, worth redialing sooner). `EventKind`
  in `peer/types.go` already has exactly this split (`ConnectionFailed` vs
  `ConnectionClosed`) — use it.
- **Stored state stays tiny; derive the rest.** Downloading / useful / choked are
  *derived predicates* (`currentPiece != nil`, `theirBits ∧ ¬mine ≠ ∅`, leecher
  flags), never stored. Litmus test: if a stored value being wrong would need
  re-sync code, delete it and compute it instead.
- **Connect wide, download bounded.** Idle handshaked conns are nearly free and feed
  rarest-first availability. `Interested` at anyone useful — no limit needed. DO
  bound + track *who holds a claimed piece* (memory = pieces in flight × piece size;
  also the endgame-stall tool).

### Manager = two rooms, two flows (final shape)

- **Two rooms.** `waiting` = addresses + history (nbFailure, eligibleAt, last-known
  bitfield snapshot). `active` = live connections, bounded by the hardcap. No third
  room: "prison" is just a waiting peer whose `eligibleAt` is in the future →
  priority 0. Eligibility is one timestamp comparison, not a queue transfer.
- **Priority is computed at pick time, never stored.** When a slot opens, rank
  eligible waiting peers: `score = f(needsCoverage(lastBitfield, myBitfield),
  nbFailure, deathKind, unknownBonus)`. A stored priority goes stale two ways at
  once (what we know changes AND what we need changes). Same move as killing the
  status enum.
- **One triage function, called once per death** — the only place recycle policy
  lives: `triage(deathKind, lastBitfield, nbFailure, fedUsAnything) →
  Drop | backToWaiting(eligibleAt)`. Never-handshaked → sentences grow with
  nbFailure → eventually Drop. Died-after-connected with useful pieces → short
  sentence. Evicted-at-cap while useful → gentle sentence (not punishment).
- **Two one-way messages between the flows, nothing else:**
  active → waiting: "died, here's the final snapshot" (triage input).
  waiting → active: "freshly dialed connection for the free slot."
  Wanting a third message is the smell to stop and rethink.
- **Separate flows ≠ separate goroutines.** One manager loop calling
  `reviewActive()` then `admitFromWaiting()`, each touching only its own room.
  All the conceptual separation, zero locks. Split into goroutines only if a real
  reason shows up — going the other direction is much harder.
- **Different rhythms:** active flow runs on the tick (usefulness review, piece
  steering); waiting flow is event-driven (slot freed / eligibleAt timer). Don't
  re-rank the waiting room when nothing can be admitted.

### Policy caveats (feed these into triage/scoring)

- A peer choking us still counts as useful if it *has* pieces we need — remote unchoke
  rotates (~30s optimistic). Killing on "no bytes this cycle" churns exactly the good
  peers. "Useful" should mean *has something I need*, not *fed me last cycle*.
- Empty/absent bitfield ≠ dead: some peers only trickle `Have`s. Alive-but-boring is
  a manager eviction question (only when at cap), not a connection liveness failure.
- A dead peer's bitfield is a *memory, not a fact* — rank by it, don't filter by it;
  refresh on the new handshake.
- Reserve room for unknowns (fresh tracker peers, no bitmap/history) — rank them
  optimistically or you'll re-dial the same familiar peers forever. Explore/exploit.
- Cap the address book (waiting room + remembered bitmaps) — the tracker keeps
  announcing; a long session otherwise accumulates dead addresses to re-triage
  forever.

### Known breakage in current WIP (found reading, not yet fixed — mine to fix)

- A dial/handshake failure updates a zero-value stats snapshot but does not notify the
  manager or immediately free the active slot.
- EOF/cancellation similarly ends the connection goroutine without removing its
  manager entry.
- `evict` moves a peer into `waiting` but does not delete it from `active`.
- `selectCandidates` can append the same tag once per matching piece and
  `fillActiveSlots` does not stop at the active-peer cap.
- Active availability uses `profile.lastBitfield`, which is only refreshed on
  eviction, so rarest-first inputs go stale during a live connection.
- The rarest-first ranking is still disconnected from `PieceManager.GetWork`, which
  claims the first available piece by index.
- “Zero speed” eviction treats healthy-but-choked/useful peers as stalled.
- Socket writes remain unsynchronized across leecher, seeder, and keepalive paths.
- The current reverted baseline passes `go test ./...`; preserve that while fixing
  the lifecycle issues above.

## Parked — future optional mini-projects
Each is roughly its own thing with its own budget. Pick up only if the specific itch is
still there. NOT part of v1.0.

- **DHT (Kademlia)** (3–5) — trackerless peer discovery: XOR distance, k-buckets,
  iterative lookups. *The one I'm most drawn to — it's why I started this.* Save it so it
  gets real focus, not afterthought energy. A genuine distributed-systems project.
- **uTP (BEP 29)** (3–5+) — peer data transfer over UDP instead of TCP. Rebuild
  reliability (seq/ACK/retransmit), in-order reassembly, RTO estimation, and LEDBAT
  congestion control (delay-based — politely *yields* bandwidth, opposite of TCP's
  fight-for-it). The deep TCP-vs-UDP networking lesson; debugging time is un-delegatable.
- **UDP trackers (BEP 15)** (~1) — peer list over UDP instead of HTTP. Mechanical; the
  `case "udp"` stub in `tracker_client/client.go` is already waiting for it. (Note: this is
  peer *discovery* over UDP — a different thing from uTP, which is data *transfer*.)
- **Magnet links (BEP 9/10)** (1–2) — bootstrap from just an infohash; fetch the metadata
  from peers via the extension protocol. Mostly negotiation plumbing.

## Notes
- Frontend: Wails v2 desktop shell with React + TypeScript. Prefer direct Wails
  bindings for commands and events/snapshots for live state; add REST + SSE only if
  a browser/remote-control frontend becomes a real requirement.
- PeerManager extraction: do it only if the peer code gets annoying to work with, not on
  principle. Architecture for its own sake is the time-sink to avoid.
