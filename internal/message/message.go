package message

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	idChoke         uint8 = 0
	idUnchoke       uint8 = 1
	idInterested    uint8 = 2
	idNotInterested uint8 = 3
	idHave          uint8 = 4
	idBitfield      uint8 = 5
	idRequest       uint8 = 6
	idPiece         uint8 = 7
	idCancel        uint8 = 8
	idPort          uint8 = 9

	lenChoke         uint32 = 1
	lenUnchoke       uint32 = 1
	lenInterested    uint32 = 1
	lenNotInterested uint32 = 1
	lenHave          uint32 = 5
	lenRequest       uint32 = 13
	lenCancel        uint32 = 13
)

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
	Index  uint32
	Begin  uint32
	Length uint32
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

func (hs *Handshake) WriteTo(w io.Writer) (int64, error) {
	buf := make([]byte, 0, 68)
	buf = append(buf, hs.Pstrlen)
	buf = append(buf, hs.Pstr...)
	buf = append(buf, hs.Reserved[:]...)
	buf = append(buf, hs.InfoHash[:]...)
	buf = append(buf, hs.PeerId[:]...)
	n, err := w.Write(buf)
	return int64(n), err
}

func (h *Handshake) ReadFrom(r io.Reader) (int64, error) {
	var count int64
	if err := binary.Read(r, binary.BigEndian, &h.Pstrlen); err != nil {
		return count, err
	}
	count += int64(binary.Size(h.Pstrlen))

	h.Pstr = make([]byte, h.Pstrlen)
	n, err := io.ReadFull(r, h.Pstr)
	count += int64(n)
	if err != nil {
		return count, err
	}

	if err := binary.Read(r, binary.BigEndian, &h.Reserved); err != nil {
		return count, err
	}
	count += int64(binary.Size(h.Reserved))

	if err := binary.Read(r, binary.BigEndian, &h.InfoHash); err != nil {
		return count, err
	}
	count += int64(binary.Size(h.InfoHash))

	if err := binary.Read(r, binary.BigEndian, &h.PeerId); err != nil {
		return count, err
	}
	count += int64(binary.Size(h.PeerId))

	return count, nil
}

func NewKeepAlive() KeepAlive { return KeepAlive{} }

func (ka *KeepAlive) WriteTo(w io.Writer) (int64, error) {
	buf := make([]byte, 4)
	n, err := w.Write(buf)
	return int64(n), err
}

func (ka *KeepAlive) ReadFrom(r io.Reader) (int64, error) {
	var val uint32
	if err := binary.Read(r, binary.BigEndian, &val); err != nil {
		return 0, err
	}
	if val != 0 {
		return 4, fmt.Errorf("expected keepalive length 0, got %d", val)
	}
	return 4, nil
}

func NewChoke() Choke                 { return Choke{} }
func NewUnChoke() UnChoke             { return UnChoke{} }
func NewInterested() Interested       { return Interested{} }
func NewNotInterested() NotInterested { return NotInterested{} }

func (c *Choke) WriteTo(w io.Writer) (int64, error) {
	buf := make([]byte, 0, 5)
	buf = binary.BigEndian.AppendUint32(buf, lenChoke)
	buf = append(buf, idChoke)
	n, err := w.Write(buf)
	return int64(n), err
}

func (c *Choke) ReadFrom(r io.Reader) (int64, error) {
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return 0, err
	}
	if length != lenChoke {
		return 4, fmt.Errorf("expected choke length %d, got %d", lenChoke, length)
	}
	var id uint8
	if err := binary.Read(r, binary.BigEndian, &id); err != nil {
		return 4, err
	}
	if id != idChoke {
		return 5, fmt.Errorf("expected choke id %d, got %d", idChoke, id)
	}
	return 5, nil
}

func (u *UnChoke) WriteTo(w io.Writer) (int64, error) {
	buf := make([]byte, 0, 5)
	buf = binary.BigEndian.AppendUint32(buf, lenUnchoke)
	buf = append(buf, idUnchoke)
	n, err := w.Write(buf)
	return int64(n), err
}

func (u *UnChoke) ReadFrom(r io.Reader) (int64, error) {
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return 0, err
	}
	if length != lenUnchoke {
		return 4, fmt.Errorf("expected unchoke length %d, got %d", lenUnchoke, length)
	}
	var id uint8
	if err := binary.Read(r, binary.BigEndian, &id); err != nil {
		return 4, err
	}
	if id != idUnchoke {
		return 5, fmt.Errorf("expected unchoke id %d, got %d", idUnchoke, id)
	}
	return 5, nil
}

func (i *Interested) WriteTo(w io.Writer) (int64, error) {
	buf := make([]byte, 0, 5)
	buf = binary.BigEndian.AppendUint32(buf, lenInterested)
	buf = append(buf, idInterested)
	n, err := w.Write(buf)
	return int64(n), err
}

func (i *Interested) ReadFrom(r io.Reader) (int64, error) {
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return 0, err
	}
	if length != lenInterested {
		return 4, fmt.Errorf("expected interested length %d, got %d", lenInterested, length)
	}
	var id uint8
	if err := binary.Read(r, binary.BigEndian, &id); err != nil {
		return 4, err
	}
	if id != idInterested {
		return 5, fmt.Errorf("expected interested id %d, got %d", idInterested, id)
	}
	return 5, nil
}

func (ni *NotInterested) WriteTo(w io.Writer) (int64, error) {
	buf := make([]byte, 0, 5)
	buf = binary.BigEndian.AppendUint32(buf, lenNotInterested)
	buf = append(buf, idNotInterested)
	n, err := w.Write(buf)
	return int64(n), err
}

func (ni *NotInterested) ReadFrom(r io.Reader) (int64, error) {
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return 0, err
	}
	if length != lenNotInterested {
		return 4, fmt.Errorf("expected not-interested length %d, got %d", lenNotInterested, length)
	}
	var id uint8
	if err := binary.Read(r, binary.BigEndian, &id); err != nil {
		return 4, err
	}
	if id != idNotInterested {
		return 5, fmt.Errorf("expected not-interested id %d, got %d", idNotInterested, id)
	}
	return 5, nil
}

func NewHave(pieceIndex uint32) Have {
	return Have{PieceIndex: pieceIndex}
}

func (h *Have) WriteTo(w io.Writer) (int64, error) {
	buf := make([]byte, 0, 9)
	buf = binary.BigEndian.AppendUint32(buf, lenHave)
	buf = append(buf, idHave)
	buf = binary.BigEndian.AppendUint32(buf, h.PieceIndex)
	n, err := w.Write(buf)
	return int64(n), err
}

func (h *Have) ReadFrom(r io.Reader) (int64, error) {
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return 0, err
	}
	if length != lenHave {
		return 4, fmt.Errorf("expected have length %d, got %d", lenHave, length)
	}
	var id uint8
	if err := binary.Read(r, binary.BigEndian, &id); err != nil {
		return 4, err
	}
	if id != idHave {
		return 5, fmt.Errorf("expected have id %d, got %d", idHave, id)
	}
	if err := binary.Read(r, binary.BigEndian, &h.PieceIndex); err != nil {
		return 5, err
	}
	return 9, nil
}

func NewBitfield(field []byte) Bitfield {
	return Bitfield{Field: field}
}

func (h *Bitfield) WriteTo(w io.Writer) (int64, error) {
	fieldLen := uint32(1 + len(h.Field))
	buf := make([]byte, 0, 4+fieldLen)
	buf = binary.BigEndian.AppendUint32(buf, fieldLen)
	buf = append(buf, idBitfield)
	buf = append(buf, h.Field...)
	n, err := w.Write(buf)
	return int64(n), err
}

func (h *Bitfield) ReadFrom(r io.Reader) (int64, error) {
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return 0, err
	}
	if length < 1 {
		return 4, fmt.Errorf("expected bitfield length >= 1, got %d", length)
	}
	var id uint8
	if err := binary.Read(r, binary.BigEndian, &id); err != nil {
		return 4, err
	}
	if id != idBitfield {
		return 5, fmt.Errorf("expected bitfield id %d, got %d", idBitfield, id)
	}

	h.Field = make([]byte, length-1)
	n, err := io.ReadFull(r, h.Field)
	if err != nil {
		return int64(5 + n), err
	}
	return int64(5 + n), nil
}

func NewRequest(index, begin, length uint32) Request {
	return Request{Index: index, Begin: begin, Length: length}
}

func (rq *Request) WriteTo(w io.Writer) (int64, error) {
	buf := make([]byte, 0, 17)
	buf = binary.BigEndian.AppendUint32(buf, lenRequest)
	buf = append(buf, idRequest)
	buf = binary.BigEndian.AppendUint32(buf, rq.Index)
	buf = binary.BigEndian.AppendUint32(buf, rq.Begin)
	buf = binary.BigEndian.AppendUint32(buf, rq.Length)
	n, err := w.Write(buf)
	return int64(n), err
}

func (rq *Request) ReadFrom(r io.Reader) (int64, error) {
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return 0, err
	}
	if length != lenRequest {
		return 4, fmt.Errorf("expected request length %d, got %d", lenRequest, length)
	}
	var id uint8
	if err := binary.Read(r, binary.BigEndian, &id); err != nil {
		return 4, err
	}
	if id != idRequest {
		return 5, fmt.Errorf("expected request id %d, got %d", idRequest, id)
	}

	if err := binary.Read(r, binary.BigEndian, &rq.Index); err != nil {
		return 5, err
	}

	if err := binary.Read(r, binary.BigEndian, &rq.Begin); err != nil {
		return 9, err
	}

	if err := binary.Read(r, binary.BigEndian, &rq.Length); err != nil {
		return 13, err
	}

	return int64(17), nil
}

func NewPiece(index, begin uint32, block []byte) Piece {
	return Piece{Index: index, Begin: begin, Block: block}
}

func (p *Piece) WriteTo(w io.Writer) (int64, error) {
	pieceLen := uint32(9 + len(p.Block))
	buf := make([]byte, 0, 4+pieceLen)
	buf = binary.BigEndian.AppendUint32(buf, pieceLen)
	buf = append(buf, idPiece)
	buf = binary.BigEndian.AppendUint32(buf, p.Index)
	buf = binary.BigEndian.AppendUint32(buf, p.Begin)
	buf = append(buf, p.Block...)
	n, err := w.Write(buf)
	return int64(n), err
}

func (p *Piece) ReadFrom(r io.Reader) (int64, error) {
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return 0, err
	}
	if length < 9 {
		return 4, fmt.Errorf("expected piece length >= 9, got %d", length)
	}
	var id uint8
	if err := binary.Read(r, binary.BigEndian, &id); err != nil {
		return 4, err
	}
	if id != idPiece {
		return 5, fmt.Errorf("expected piece id %d, got %d", idPiece, id)
	}

	if err := binary.Read(r, binary.BigEndian, &p.Index); err != nil {
		return 5, err
	}

	if err := binary.Read(r, binary.BigEndian, &p.Begin); err != nil {
		return 9, err
	}

	p.Block = make([]byte, length-9)
	n, err := io.ReadFull(r, p.Block)
	if err != nil {
		return int64(9 + n), err
	}

	return int64(13 + n), nil
}

func NewCancel(index, begin, length uint32) Cancel {
	return Cancel{Index: index, Begin: begin, Length: length}
}

func (c *Cancel) WriteTo(w io.Writer) (int64, error) {
	buf := make([]byte, 0, 17)
	buf = binary.BigEndian.AppendUint32(buf, lenCancel)
	buf = append(buf, idCancel)
	buf = binary.BigEndian.AppendUint32(buf, c.Index)
	buf = binary.BigEndian.AppendUint32(buf, c.Begin)
	buf = binary.BigEndian.AppendUint32(buf, c.Length)
	n, err := w.Write(buf)
	return int64(n), err
}

func (c *Cancel) ReadFrom(r io.Reader) (int64, error) {
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return 0, err
	}
	if length != lenCancel {
		return 4, fmt.Errorf("expected cancel length %d, got %d", lenCancel, length)
	}
	var id uint8
	if err := binary.Read(r, binary.BigEndian, &id); err != nil {
		return 4, err
	}
	if id != idCancel {
		return 5, fmt.Errorf("expected cancel id %d, got %d", idCancel, id)
	}
	if err := binary.Read(r, binary.BigEndian, &c.Index); err != nil {
		return 5, err
	}
	if err := binary.Read(r, binary.BigEndian, &c.Begin); err != nil {
		return 9, err
	}
	if err := binary.Read(r, binary.BigEndian, &c.Length); err != nil {
		return 13, err
	}
	return 17, nil
}

func NewPort(port uint16) Port {
	return Port{Port: port}
}
