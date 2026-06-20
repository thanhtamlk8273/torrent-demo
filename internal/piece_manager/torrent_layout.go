package piecemanager

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"torrent-demo/internal/mathutil"
	"torrent-demo/internal/metainfo"
)

type FileLayout struct {
	torrentOffset int64
	path          string
	size          int64
	file          *os.File
}

type TorrentLayout struct {
	location    string
	torrentSize int64
	pieceSize   int64
	piecesCount int
	files       []FileLayout
}

func NewTorrentLayout(location string, info metainfo.Info) (*TorrentLayout, error) {
	if ok := dirExists(location); !ok {
		return nil, fmt.Errorf("NewTorrentLayout: can't open %s\n", location)
	}

	var layout TorrentLayout
	switch i := info.(type) {
	case metainfo.SingleFileInfo:
		layout.pieceSize = int64(i.InfoCommon.PieceLength)
		layout.torrentSize = int64(i.Length)
		fileLayout, err := openFileLayout(location, []string{i.Name}, 0, int64(i.Length))
		if err != nil {
			return nil, err
		}
		layout.files = append(layout.files, fileLayout)
	case metainfo.MultiFileInfo:
		layout.pieceSize = int64(i.InfoCommon.PieceLength)
		torrentOffset := int64(0)
		for _, fileInfo := range i.Files {
			fileLayout, err := openFileLayout(location, fileInfo.Path, torrentOffset, int64(fileInfo.Length))
			if err != nil {
				return nil, err
			}
			layout.files = append(layout.files, fileLayout)
			torrentOffset += int64(fileInfo.Length)
			layout.torrentSize += int64(fileInfo.Length)
		}
	}

	layout.location = location
	layout.piecesCount = int(mathutil.CeilDiv(layout.torrentSize, layout.pieceSize))

	return &layout, nil
}

func (l *TorrentLayout) pieceSizeAt(index int) int64 {
	if index+1 == l.piecesCount {
		return l.torrentSize - l.pieceSize*int64(index)
	}
	return l.pieceSize
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

	readSize := l.pieceSizeAt(index)

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

func readAtCapped(f *os.File, _ int64, offset int64, dataSize int64) ([]byte, error) {
	data := make([]byte, dataSize)
	if n, err := f.ReadAt(data, offset); err != nil {
		if err == io.EOF {
			return data[:n], nil
		}
		return nil, err
	}
	return data, nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

func openFileLayout(location string, pathParts []string, torrentOffset int64, size int64) (FileLayout, error) {
	filePath, err := safeJoin(location, pathParts...)
	if err != nil {
		return FileLayout{}, err
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return FileLayout{}, err
	}
	file, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return FileLayout{}, err
	}
	if err := file.Truncate(size); err != nil {
		return FileLayout{}, fmt.Errorf("openFileLayout: can't truncate %s: %w", filePath, err)
	}
	return FileLayout{
		torrentOffset: torrentOffset,
		path:          filePath,
		size:          size,
		file:          file,
	}, nil
}

func safeJoin(base string, parts ...string) (string, error) {
	for _, part := range parts {
		if part == ".." || part == "" || filepath.IsAbs(part) {
			return "", fmt.Errorf("safeJoin: invalid path segment %q", part)
		}
	}

	joined := filepath.Join(append([]string{base}, parts...)...)

	absBase, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	absJoined, err := filepath.Abs(joined)
	if err != nil {
		return "", err
	}
	if absJoined != absBase && !strings.HasPrefix(absJoined, absBase+string(os.PathSeparator)) {
		return "", fmt.Errorf("safeJoin: path %q escapes base directory %q", joined, base)
	}

	return joined, nil
}
