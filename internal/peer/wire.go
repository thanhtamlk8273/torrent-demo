package peer

import (
	"fmt"
	"io"
	"torrent-demo/internal/message"
)

func sendHandshake(w io.Writer, infoHash, peerId [20]byte) error {
	handShake := message.NewHandshake(infoHash, peerId)
	_, err := handShake.WriteTo(w)
	return err
}

func receiveHandshake(r io.Reader, expectedInfoHash [20]byte) error {
	var handShake message.Handshake
	if _, err := handShake.ReadFrom(r); err != nil {
		return err
	}
	if handShake.InfoHash != expectedInfoHash {
		return fmt.Errorf("info_hash mismatch")
	}
	return nil
}

func sendBitfield(w io.Writer, bf Bitfield) error {
	bitfield := message.NewBitfield(bf.Bytes())
	_, err := bitfield.WriteTo(w)
	return err
}

func sendHave(w io.Writer, index uint32) error {
	have := message.NewHave(index)
	_, err := have.WriteTo(w)
	return err
}

func sendKeepAlive(w io.Writer) error {
	keepAlive := message.NewKeepAlive()
	_, err := keepAlive.WriteTo(w)
	return err
}

func sendInterested(w io.Writer) error {
	interested := message.NewInterested()
	_, err := interested.WriteTo(w)
	return err
}

func sendNotInterested(w io.Writer) error {
	notInterested := message.NewNotInterested()
	_, err := notInterested.WriteTo(w)
	return err
}

func sendChoke(w io.Writer) error {
	choke := message.NewChoke()
	_, err := choke.WriteTo(w)
	return err
}

func sendUnchoke(w io.Writer) error {
	unchoke := message.NewUnchoke()
	_, err := unchoke.WriteTo(w)
	return err
}

func sendRequest(w io.Writer, index, begin, length uint32) error {
	req := message.NewRequest(index, begin, length)
	_, err := req.WriteTo(w)
	return err
}

func sendPiece(w io.Writer, index, begin uint32, block []byte) error {
	piece := message.NewPiece(index, begin, block)
	_, err := piece.WriteTo(w)
	return err
}
