package metadatafile_test

import (
	"testing"
	metainfo "torrent-demo/internal/metainfo"
)

func TestNewMetaInfo_MultiFile(t *testing.T) {
	meta, err := metainfo.NewMetaInfo("testdata/rdr.torrent")
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if meta.Announce == "" {
		t.Error("expected announce to be set")
	}

	info, ok := meta.Info.(metainfo.MultiFileInfo)
	if !ok {
		t.Fatal("expected multi-file torrent")
	}

	if info.Name == "" {
		t.Error("expected name to be set")
	}

	if info.PieceLength == 0 {
		t.Error("expected piece length to be set")
	}

	if len(info.Pieces) == 0 {
		t.Error("expected pieces to be set")
	}

	if len(info.Files) == 0 {
		t.Error("expected files to be non-empty")
	}

	for _, f := range info.Files {
		if f.Length == 0 {
			t.Errorf("file %v has zero length", f.Path)
		}
		if len(f.Path) == 0 {
			t.Error("expected file path to be set")
		}
	}
}

func TestNewMetaInfo_AnnounceList(t *testing.T) {
	meta, err := metainfo.NewMetaInfo("testdata/rdr.torrent")
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if len(meta.AnnounceList) == 0 {
		t.Fatal("expected announce-list to be non-empty")
	}

	for i, tier := range meta.AnnounceList {
		if len(tier) == 0 {
			t.Errorf("tier %d is empty", i)
		}
		for _, url := range tier {
			if url == "" {
				t.Errorf("tier %d contains empty URL", i)
			}
		}
	}

	if meta.AnnounceList[0][0] != "udp://46.148.18.250:2710" {
		t.Errorf("unexpected first tracker: %s", meta.AnnounceList[0][0])
	}
}
