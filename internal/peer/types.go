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

type PieceBook interface {
	SubmitAvailability([]int) []int
}
