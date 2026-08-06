package metainfo_test

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

	info, ok := meta.Info.Info.(metainfo.MultiFileInfo)
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

func TestNewMetaInfo_InfoHash(t *testing.T) {
	meta, err := metainfo.NewMetaInfo("testdata/rdr.torrent")
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	want := [20]byte{0xbb, 0x87, 0xcf, 0xc7, 0x79, 0x92, 0x11, 0x42, 0xe6, 0x1b,
		0x97, 0x68, 0x23, 0x4c, 0x14, 0x96, 0x4c, 0x0e, 0x2f, 0xc3}
	if meta.InfoHash != want {
		t.Errorf("info hash = %x, want %x", meta.InfoHash, want)
	}
}

func TestNewMetaInfo_ExtendedFields(t *testing.T) {
	meta, err := metainfo.NewMetaInfo("testdata/rdr.torrent")
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if meta.Comment != "http://rutor.is/torrent/780917" {
		t.Errorf("comment = %q, want %q", meta.Comment, "http://rutor.is/torrent/780917")
	}
	if meta.CreatedBy != "Transmission/2.92 (14714)" {
		t.Errorf("created by = %q, want %q", meta.CreatedBy, "Transmission/2.92 (14714)")
	}
	if meta.CreationDate != 1603364761 {
		t.Errorf("creation date = %d, want %d", meta.CreationDate, 1603364761)
	}

	info, ok := meta.Info.Info.(metainfo.MultiFileInfo)
	if !ok {
		t.Fatal("expected multi-file torrent")
	}
	if info.Private != 0 {
		t.Errorf("private = %d, want 0", info.Private)
	}
}

// A torrent without created by / creation date / private keys must still
// parse, leaving the zero values (bencode.fill skips missing keys). try.torrent
// does carry a comment, which is why it is not asserted here.
func TestNewMetaInfo_MissingExtendedKeys(t *testing.T) {
	meta, err := metainfo.NewMetaInfo("testdata/try.torrent")
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if meta.CreatedBy != "" {
		t.Errorf("created by = %q, want empty", meta.CreatedBy)
	}
	if meta.CreationDate != 0 {
		t.Errorf("creation date = %d, want 0", meta.CreationDate)
	}
	if info := meta.Info.Info.Common(); info.Private != 0 {
		t.Errorf("private = %d, want 0", info.Private)
	}
}

// Positive path for the new private key: a private flag of 1 must round-trip.
func TestInfoField_UnmarshalBencode_Private(t *testing.T) {
	data := map[string]any{
		"piece length": 512,
		"pieces":       "xxxx",
		"name":         "myfile.txt",
		"length":       1024,
		"private":      1,
	}
	var f metainfo.InfoField
	if err := f.UnmarshalBencode(data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	info, ok := f.Info.(metainfo.SingleFileInfo)
	if !ok {
		t.Fatalf("expected SingleFileInfo, got %T", f.Info)
	}
	if info.Private != 1 {
		t.Errorf("private = %d, want 1", info.Private)
	}
}

// --- InfoField.UnmarshalBencode ---

func TestInfoField_UnmarshalBencode_SelectsSingleFile(t *testing.T) {
	data := map[string]any{
		"piece length": 512,
		"pieces":       "xxxx",
		"name":         "myfile.txt",
		"length":       1024,
	}
	var f metainfo.InfoField
	if err := f.UnmarshalBencode(data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := f.Info.(metainfo.SingleFileInfo); !ok {
		t.Fatalf("expected SingleFileInfo, got %T", f.Info)
	}
}

func TestInfoField_UnmarshalBencode_SelectsMultiFile(t *testing.T) {
	data := map[string]any{
		"piece length": 512,
		"pieces":       "xxxx",
		"name":         "mydir",
		"files": []any{
			map[string]any{
				"length": 100,
				"path":   []any{"subdir", "file.txt"},
			},
		},
	}
	var f metainfo.InfoField
	if err := f.UnmarshalBencode(data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := f.Info.(metainfo.MultiFileInfo); !ok {
		t.Fatalf("expected MultiFileInfo, got %T", f.Info)
	}
}

func TestInfoField_UnmarshalBencode_NotDict(t *testing.T) {
	var f metainfo.InfoField
	if err := f.UnmarshalBencode("not a dict"); err == nil {
		t.Fatal("expected error for non-dict input")
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
