package tracker_client

import "testing"

// compactPeers builds the 6-bytes-per-peer compact representation: 4 bytes IP,
// 2 bytes big-endian port.
func compactPeers(t *testing.T, entries ...[6]byte) string {
	t.Helper()
	var b []byte
	for _, e := range entries {
		b = append(b, e[:]...)
	}
	return string(b)
}

func TestPeerListUnmarshalCompact(t *testing.T) {
	// port 6881 = 0x1AE1 -> bytes 0x1A, 0xE1; port 80 -> 0x00, 0x50.
	data := compactPeers(t,
		[6]byte{1, 2, 3, 4, 0x1A, 0xE1},
		[6]byte{5, 6, 7, 8, 0x00, 0x50},
	)

	var pl PeerList
	if err := pl.UnmarshalBencode(data); err != nil {
		t.Fatalf("UnmarshalBencode: %v", err)
	}

	if len(pl.Peers) != 2 {
		t.Fatalf("got %d peers, want 2", len(pl.Peers))
	}
	if pl.Peers[0].Ip != "1.2.3.4" || pl.Peers[0].Port != 6881 {
		t.Errorf("peer[0] = %s:%d, want 1.2.3.4:6881", pl.Peers[0].Ip, pl.Peers[0].Port)
	}
	if pl.Peers[1].Ip != "5.6.7.8" || pl.Peers[1].Port != 80 {
		t.Errorf("peer[1] = %s:%d, want 5.6.7.8:80", pl.Peers[1].Ip, pl.Peers[1].Port)
	}
}

func TestPeerListUnmarshalCompactIgnoresTrailingPartial(t *testing.T) {
	// 6 valid bytes + 1 stray byte: the partial record must be dropped, not
	// read out of bounds.
	data := compactPeers(t, [6]byte{9, 9, 9, 9, 0, 1}) + "X"

	var pl PeerList
	if err := pl.UnmarshalBencode(data); err != nil {
		t.Fatalf("UnmarshalBencode: %v", err)
	}
	if len(pl.Peers) != 1 {
		t.Fatalf("got %d peers, want 1 (trailing partial ignored)", len(pl.Peers))
	}
	if pl.Peers[0].Port != 1 {
		t.Errorf("port = %d, want 1", pl.Peers[0].Port)
	}
}

func TestPeerListUnmarshalRejectsUnexpectedType(t *testing.T) {
	var pl PeerList
	if err := pl.UnmarshalBencode(12345); err == nil {
		t.Error("expected error for non-string, non-list payload")
	}
}

func TestNewTrackerClientSchemes(t *testing.T) {
	cases := []struct {
		url     string
		wantErr bool
	}{
		{"http://tracker.example.com/announce", false},
		{"https://tracker.example.com/announce", false},
		{"udp://tracker.example.com:80", true},
		{"ftp://tracker.example.com", true},
	}
	for _, c := range cases {
		client, err := NewTrackerClient(c.url)
		if c.wantErr {
			if err == nil {
				t.Errorf("%s: expected error, got nil", c.url)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected error: %v", c.url, err)
		}
		if client == nil {
			t.Errorf("%s: expected a client, got nil", c.url)
		}
	}
}

func TestAnnounceEventString(t *testing.T) {
	cases := map[AnnounceEvent]string{
		EventStarted:   "started",
		EventStopped:   "stopped",
		EventCompleted: "completed",
		EventNone:      "",
	}
	for ev, want := range cases {
		if got := ev.String(); got != want {
			t.Errorf("AnnounceEvent(%d).String() = %q, want %q", ev, got, want)
		}
	}
}
