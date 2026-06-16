package piecemanager

import (
	"fmt"
	"io"
	"os"
)

type FileLayout struct {
	torrentOffset int64
	path          string
	size          int64
	file          *os.File
}

type TorrentLayout struct {
	torrentSize int64
	pieceSize   int64
	piecesCount int
	files       []FileLayout
}

func (l *TorrentLayout) writePiece(index int, data []byte) error {
	if index >= l.piecesCount {
		return fmt.Errorf("writePiece: piece index %d out of bounds [0, %d)", index, l.piecesCount)
	}

	pieceOffset := int64(index) * l.pieceSize
	startIdx := -1
	for idx, fLayout := range l.files {
		if pieceOffset < fLayout.torrentOffset || pieceOffset >= fLayout.torrentOffset+fLayout.size {
			continue
		}
		startIdx = idx
		break
	}

	if startIdx == -1 {
		return fmt.Errorf("writePiece: no file covers piece %d", index)
	}

	for len(data) > 0 && startIdx < len(l.files) {
		fileLayout := l.files[startIdx]
		fileOffset := pieceOffset - fileLayout.torrentOffset
		newData, err := writeAtCapped(fileLayout.file, fileLayout.size, fileOffset, data)
		if err != nil {
			return err
		}
		pieceOffset = pieceOffset + int64(len(data)-len(newData))
		startIdx += 1
		data = newData
	}

	if len(data) > 0 {
		return fmt.Errorf("writePiece: piece %d data exceeds available files", index)
	}

	return nil
}

func (l *TorrentLayout) readPiece(index int) ([]byte, error) {
	if index >= l.piecesCount {
		return nil, fmt.Errorf("readPiece: piece index %d out of bounds [0, %d)", index, l.piecesCount)
	}

	data := []byte{}

	pieceOffset := int64(index) * l.pieceSize
	startIdx := -1
	for idx, fLayout := range l.files {
		if pieceOffset < fLayout.torrentOffset || pieceOffset >= fLayout.torrentOffset+fLayout.size {
			continue
		}
		startIdx = idx
		break
	}

	if startIdx == -1 {
		return nil, fmt.Errorf("readPiece: no file covers piece %d", index)
	}

	readSize := l.pieceSize
	for readSize > 0 && startIdx < len(l.files) {
		fileLayout := l.files[startIdx]
		fileOffset := pieceOffset - fileLayout.torrentOffset
		newData, err := readAtCapped(fileLayout.file, fileLayout.size, fileOffset, readSize)
		if err != nil {
			return nil, err
		}
		pieceOffset = pieceOffset + int64(len(newData))
		startIdx += 1
		readSize -= int64(len(newData))
		data = append(data, newData...)
	}

	if readSize > 0 {
		return nil, fmt.Errorf("readPiece: piece %d data spans beyond available files", index)
	}

	return data, nil
}

func writeAtCapped(f *os.File, fileSize int64, offset int64, data []byte) ([]byte, error) {
	writeSize := int64(len(data))
	if offset+int64(len(data)) > fileSize {
		writeSize = fileSize - offset
	}
	if _, err := f.WriteAt(data[:writeSize], offset); err != nil {
		return nil, err
	}
	return data[writeSize:], nil
}

func readAtCapped(f *os.File, fileSize int64, offset int64, dataSize int64) ([]byte, error) {
	data := make([]byte, dataSize)
	if n, err := f.ReadAt(data, offset); err != nil {
		if err == io.EOF {
			return data[:n], nil
		}
		return nil, err
	}
	return data, nil
}
