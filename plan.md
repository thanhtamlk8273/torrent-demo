# torrent-demo — roadmap

A for-fun BitTorrent client, built to *understand* the protocol, not to ship a product.
Plumbing (debug UI, tests, frontend, scaffolding) gets delegated; the focus is the core
mechanisms. Rough effort = "focused sessions" (~one solid evening each).

## Done
- bencode parser, message wire format, tracker client (HTTP), metainfo
- PieceManager + per-piece scheduler (claim/unclaim/complete), file layout
- TCP peer protocol: handshake, bitfield, piece download + SHA1 verify
- Multi-peer downloading with a bounded active pool and waiting-peer address book
- Block request pipelining (up to 8 in flight per peer)
- Basic upload/seeding path (`Bitfield`, `Have`, `Unchoke`, `Request` → `Piece`)
- Startup recheck/resume by hashing existing payload pieces
- Debug-only tview dashboard with live download/upload speed
- Unit tests across the core packages (peer tests need reconciliation after the
  current API refactor; see stabilization)
- **Real-world end-to-end validation:** downloaded a 13,674,576,509-byte,
  6,521-piece payload and independently rehashed every piece against the `.torrent`;
  6,521/6,521 matched with zero corruption

## Core roadmap → v1.0
This is the line for "I actually understand BitTorrent." Stop here.

1. **Peer I/O + pipelining**
   - [x] Keep multiple block requests in flight per peer (currently 8).
   - [ ] Serialize every socket write through one owner/channel; current leecher,
     seeder, and keepalive paths can write concurrently.
   - [ ] Make write failures terminate the connection and release claimed work.
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

## Next target — stabilization gate

Do this before GUI or multiple simultaneous torrents. The engine has demonstrated
correct bytes at scale; now make lifecycle and completion behavior deterministic.

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
- [ ] **Tests green after refactor:** reconcile stale `PeerConnection.Run`/`PeerStats`
  tests, add manager transition/invariant tests, then pass `go test ./...`.
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

At that point the single-download engine is a credible v1 core. GUI and multiple
downloads become the next layer, not fixes for unfinished lifecycle behavior.

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
- `PeerConnection` tests still target the pre-refactor `Run(ctx)` and `PeerStats`
  API, so the full test suite does not currently compile cleanly.

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
- Frontend, when wanted: Go backend exposes a few REST endpoints (add/pause/remove) +
  one **SSE** stream for live state (reuse the `Reporter`/progress data the debug TUI
  already produces). React on top — handed off, not hand-written here.
- PeerManager extraction: do it only if the peer code gets annoying to work with, not on
  principle. Architecture for its own sake is the time-sink to avoid.
