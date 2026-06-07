package metadatafile

import (
	"fmt"
	"os"
	"torrent-demo/internal/bencode"
)

type MetaInfo struct {
	Info         Info
	Announce     string
	AnnounceList [][]string
}

type Info interface {
	isInfo()
}

type InfoCommon struct {
	PieceLength int
	Pieces      []byte
}

type SingleFileInfo struct {
	InfoCommon
	Name   string
	Length int
}

func (SingleFileInfo) isInfo() {}

type MultiFileInfo struct {
	InfoCommon
	Name  string
	Files []FileEntry
}

func (MultiFileInfo) isInfo() {}

type FileEntry struct {
	Length int
	Path   []string
}

func NewMetaInfo(file string) (MetaInfo, error) {
	rawData, err := os.ReadFile(file)
	if err != nil {
		return MetaInfo{}, fmt.Errorf("failed to read file: %w", err)
	}

	decodedData, err := bencode.Decode(rawData)
	if err != nil {
		return MetaInfo{}, fmt.Errorf("failed to decode file: %w", err)
	}

	return parseMetadataFile(decodedData)
}

func parseMetadataFile(metaInfoAny any) (MetaInfo, error) {
	metaInfoDict, ok := metaInfoAny.(map[string]any)
	if !ok {
		return MetaInfo{}, fmt.Errorf("failed to parse MetaInfo")
	}

	infoAny, ok := metaInfoDict["info"]
	if !ok {
		return MetaInfo{}, fmt.Errorf("failed to parse MetaInfo")
	}

	info, err := parseInfoDic(infoAny)
	if err != nil {
		return MetaInfo{}, fmt.Errorf("failed to parse MetaInfo: %w", err)
	}

	announce, ok := metaInfoDict["announce"].(string)
	if !ok {
		return MetaInfo{}, fmt.Errorf("failed to parse MetaInfo")
	}

	announceList := parseAnnounceList(metaInfoDict["announce-list"])

	return MetaInfo{
		Info:         info,
		Announce:     announce,
		AnnounceList: announceList,
	}, nil
}

func parseAnnounceList(raw any) [][]string {
	tiers, ok := raw.([]any)
	if !ok {
		return nil
	}
	result := make([][]string, 0, len(tiers))
	for _, tierAny := range tiers {
		tier, ok := tierAny.([]any)
		if !ok {
			continue
		}
		urls := make([]string, 0, len(tier))
		for _, urlAny := range tier {
			if url, ok := urlAny.(string); ok {
				urls = append(urls, url)
			}
		}
		result = append(result, urls)
	}
	return result
}

func parseInfoDic(infoDictAny any) (Info, error) {
	infoDict, ok := infoDictAny.(map[string]any)
	if !ok {
		return SingleFileInfo{}, fmt.Errorf("failed to parse Info")
	}

	// Parse common fields
	pieceLength, ok := infoDict["piece length"].(int)
	if !ok {
		return SingleFileInfo{}, fmt.Errorf("failed to parse Info")
	}

	pieces, ok := infoDict["pieces"].(string)
	if !ok {
		return SingleFileInfo{}, fmt.Errorf("failed to parse Info")
	}

	common := InfoCommon{
		PieceLength: pieceLength,
		Pieces:      []byte(pieces),
	}

	// Parse mode specific fields
	if _, ok := infoDict["files"]; ok {
		return parseMultiFileInfo(infoDict, common)
	}
	return parseSingleFileInfoDict(infoDict, common)
}

func parseMultiFileInfo(infoDict map[string]any, common InfoCommon) (Info, error) {
	name, ok := infoDict["name"].(string)
	if !ok {
		return MultiFileInfo{}, fmt.Errorf("failed to parse Info")
	}

	filesDict, ok := infoDict["files"].([]any)
	if !ok {
		return MultiFileInfo{}, fmt.Errorf("failed to parse Info")
	}

	var files []FileEntry
	for _, fileDictAny := range filesDict {
		fileDict, ok := fileDictAny.(map[string]any)
		if !ok {
			return MultiFileInfo{}, fmt.Errorf("failed to parse Info")
		}

		length, ok := fileDict["length"].(int)
		if !ok {
			return MultiFileInfo{}, fmt.Errorf("failed to parse Info")
		}

		pathList, ok := fileDict["path"].([]any)
		if !ok {
			return MultiFileInfo{}, fmt.Errorf("failed to parse Info")
		}

		path := make([]string, len(pathList))
		for i, v := range pathList {
			path[i], ok = v.(string)
			if !ok {
				return MultiFileInfo{}, fmt.Errorf("failed to parse path element")
			}
		}

		files = append(files, FileEntry{
			Length: length,
			Path:   path,
		})
	}

	return MultiFileInfo{
		InfoCommon: common,
		Name:       name,
		Files:      files,
	}, nil
}

func parseSingleFileInfoDict(infoDict map[string]any, common InfoCommon) (Info, error) {
	name, ok := infoDict["name"].(string)
	if !ok {
		return SingleFileInfo{}, fmt.Errorf("failed to parse Info")
	}

	length, ok := infoDict["length"].(int)
	if !ok {
		return SingleFileInfo{}, fmt.Errorf("failed to parse Info")
	}

	return SingleFileInfo{
		InfoCommon: common,
		Name:       name,
		Length:     length,
	}, nil
}
