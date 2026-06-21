# torrent-demo — roadmap

A for-fun BitTorrent client, built to *understand* the protocol, not to ship a product.
Plumbing (debug UI, tests, frontend, scaffolding) gets delegated; the focus is the core
mechanisms. Rough effort = "focused sessions" (~one solid evening each).

## Done
- bencode parser, message wire format, tracker client (HTTP), metainfo
- PieceManager + per-piece scheduler (claim/unclaim/complete), file layout
- TCP peer protocol: handshake, bitfield, piece download + SHA1 verify
- Multi-peer download with retry/backoff, debug-only tview dashboard (live speed)
- Unit tests across the core packages

## Core roadmap → v1.0 (~5–6 sessions)
This is the line for "I actually understand BitTorrent." Stop here.

1. **Peer read/write split + pipelining** (1–2) — *current task.*
   Split the single Run loop into read + write goroutines; route all writes through
   one channel (kills the unsynchronized `p.conn` writes). Keep ~5–10 block requests
   in flight per peer instead of stop-and-wait. Fixes the diagnosed throughput cap.
2. **Rarest-first piece selection** (~0.5) — track piece availability across peers,
   pick the rarest in `GetWork`. Teaches swarm-health scheduling.
3. **Endgame mode** (~1) — when few blocks remain, request from all peers + `Cancel`
   the losers. Fixes the "one stuck peer stalls completion" case seen live.
4. **Choking / tit-for-tat + seeding** (2–3) — **the heart of the protocol.** Build the
   upload path (answer `Request` with `Piece`), implement reciprocation + optimistic
   unchoke. This is the thing BitTorrent actually invented; without it the rest is just
   parallel downloading.

**Ownership debt — NON-NEGOTIABLE:** after the peer rewrite + PeerManager, rewrite
`UnmarshalBencode` (internal/tracker_client/client.go) by hand. It's the one piece of
*core* logic written with LLM help that I don't fully own (the `any` type switch on the
compact/list peer forms). Rewrite it myself from understanding — do NOT delegate this one.
This is the whole point: keep the hard, model-building slices mine.

→ **After core: stop and take a breather. Switch to the "LLM loader" project** (load a
small model, run a forward pass by hand — same "build the toy to feel the mechanism"
instinct as this whole project).

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
