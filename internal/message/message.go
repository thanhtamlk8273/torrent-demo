package message

import "io"

type Handshake struct {
	Pstrlen  byte
	Pstr     []byte
	Reserved [8]byte
	InfoHash [20]byte
	PeerId   [20]byte
}

type KeepAlive struct{}

type Choke struct{}

type UnChoke struct{}

type Interested struct{}

type NotInterested struct{}

type Have struct {
	PieceIndex uint32
}

type Bitfield struct {
	Field []byte
}

type Request struct {
	Index uint32
	Begin uint32
	End   uint32
}

type Piece struct {
	Index uint32
	Begin uint32
	Block []byte
}

type Cancel struct {
	Index  uint32
	Begin  uint32
	Length uint32
}

type Port struct {
	Port uint16
}

func NewHandshake(infoHash [20]byte, peerId [20]byte) Handshake {
	return Handshake{
		Pstrlen:  19,
		Pstr:     []byte("BitTorrent protocol"),
		Reserved: [8]byte{},
		InfoHash: infoHash,
		PeerId:   peerId,
	}
}

func (hs *Handshake) WriteTo(w io.Writer) error {

}

func NewKeepAlive() KeepAlive         { return KeepAlive{} }
func NewChoke() Choke                 { return Choke{} }
func NewUnChoke() UnChoke             { return UnChoke{} }
func NewInterested() Interested       { return Interested{} }
func NewNotInterested() NotInterested { return NotInterested{} }

func NewHave(pieceIndex uint32) Have {
	return Have{PieceIndex: pieceIndex}
}

func NewBitfield(field []byte) Bitfield {
	return Bitfield{Field: field}
}

func NewRequest(index, begin, end uint32) Request {
	return Request{Index: index, Begin: begin, End: end}
}

func NewPiece(index, begin uint32, block []byte) Piece {
	return Piece{Index: index, Begin: begin, Block: block}
}

func NewCancel(index, begin, length uint32) Cancel {
	return Cancel{Index: index, Begin: begin, Length: length}
}

func NewPort(port uint16) Port {
	return Port{Port: port}
}
