package peer

type EventKind int

const (
	BitfieldEvent EventKind = iota
	HaveEvent
	ConnectionFailed
	ConnectionClosed
)

type PeerEvent struct {
	tag     string
	kind    EventKind
	payload any
}

type PieceWork struct {
	Index int
	Size  int
	Hash  [20]byte
}

type PieceScheduler interface {
	GetWork(bf Bitfield) (PieceWork, bool)
	CompletePiece(index int, data []byte) error
	UnclaimPiece(index int)

	// Maybe should be a seperate interface
	GetLocalBitfield() Bitfield
	GetBlock(index, begin, length int) ([]byte, error)
}

// Reporter receives debug-only lifecycle signals for one peer connection so
// an external observer (e.g. a debug dashboard) can track progress without
// peer.go depending on that observer's implementation. Safe to leave unset.
type Reporter interface {
	PeerConnected(addr string)
	PieceStarted(addr string, index, blockCount int)
	BlockSaved(addr string, blockIndex, size int)
	PieceCompleted(addr string, index int)
	PieceFailed(addr, reason string)
	PeerDisconnected(addr string, err error)
	BlockSent(addr string, size int)
}

type PieceBook interface {
	GetNextPieces(availability []int) []int
}
