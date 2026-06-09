package metainfo

import (
	"crypto/sha1"
	"fmt"
	"os"
	"torrent-demo/internal/bencode"
)

type MetaInfo struct {
	Info         InfoField `bencode:"info"`
	InfoHash     [20]byte
	Announce     string     `bencode:"announce"`
	AnnounceList [][]string `bencode:"announce-list"`
}

type Info interface {
	isInfo()
}

type InfoField struct{ Info }

func (InfoField) isInfo() {}

type InfoCommon struct {
	PieceLength int    `bencode:"piece length"`
	Pieces      []byte `bencode:"pieces"`
}

type SingleFileInfo struct {
	InfoCommon
	Name   string `bencode:"name"`
	Length int    `bencode:"length"`
}

func (SingleFileInfo) isInfo() {}

type MultiFileInfo struct {
	InfoCommon
	Name  string      `bencode:"name"`
	Files []FileEntry `bencode:"files"`
}

func (MultiFileInfo) isInfo() {}

type FileEntry struct {
	Length int      `bencode:"length"`
	Path   []string `bencode:"path"`
}

func NewMetaInfo(file string) (MetaInfo, error) {
	rawData, err := os.ReadFile(file)
	if err != nil {
		return MetaInfo{}, fmt.Errorf("failed to read file: %w", err)
	}

	infoBytes, err := bencode.ExtractInfoBytes(rawData)
	if err != nil {
		return MetaInfo{}, fmt.Errorf("failed to decode file: %w", err)
	}

	var metaInfo MetaInfo
	if err := bencode.Unmarshal(rawData, &metaInfo); err != nil {
		return MetaInfo{}, fmt.Errorf("failed to decode file: %w", err)
	}
	metaInfo.InfoHash = sha1.Sum(infoBytes)
	return metaInfo, nil
}

func (f *InfoField) UnmarshalBencode(data any) error {
	m, ok := data.(map[string]any)
	if !ok {
		return fmt.Errorf("expected dict")
	}
	if _, hasFiles := m["files"]; hasFiles {
		var multi MultiFileInfo
		if err := bencode.FillDecoded(data, &multi); err != nil {
			return err
		}
		f.Info = multi
	} else {
		var single SingleFileInfo
		if err := bencode.FillDecoded(data, &single); err != nil {
			return err
		}
		f.Info = single
	}
	return nil
}
