package tracker_client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func newTestHTTPTracker(t *testing.T, serverURL string) *HttpTrackerClient {
	t.Helper()
	u, _ := url.Parse(serverURL)
	c, err := newHTTPTracker(u, options{timeout: 0})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestAnnounce_Success(t *testing.T) {
	// bencode: d8:intervali1800e8:completei10e10:incompletei5e5:peersld2:ip9:127.0.0.14:porti6881eeee
	resp := "d8:intervali1800e8:completei10e10:incompletei5e5:peersld2:ip9:127.0.0.14:porti6881eeee"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, resp)
	}))
	defer srv.Close()

	c := newTestHTTPTracker(t, srv.URL+"/announce")
	got, err := c.Announce(context.Background(), AnnounceRequest{
		Port:  6881,
		Event: EventStarted,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Interval != 1800 {
		t.Errorf("interval: want 1800, got %d", got.Interval)
	}
	if got.Complete != 10 {
		t.Errorf("complete: want 10, got %d", got.Complete)
	}
	if got.Incomplete != 5 {
		t.Errorf("incomplete: want 5, got %d", got.Incomplete)
	}
	if len(got.Peers.Peers) != 1 {
		t.Fatalf("peers: want 1, got %d", len(got.Peers.Peers))
	}
	if got.Peers.Peers[0].Ip != "127.0.0.1" {
		t.Errorf("peer ip: want 127.0.0.1, got %s", got.Peers.Peers[0].Ip)
	}
	if got.Peers.Peers[0].Port != 6881 {
		t.Errorf("peer port: want 6881, got %d", got.Peers.Peers[0].Port)
	}
}

func TestAnnounce_QueryParams(t *testing.T) {
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		fmt.Fprint(w, "d8:intervali60e8:completei0e10:incompletei0e5:peersleee")
	}))
	defer srv.Close()

	c := newTestHTTPTracker(t, srv.URL+"/announce")
	req := AnnounceRequest{
		Port:       6881,
		Uploaded:   100,
		Downloaded: 200,
		Left:       300,
		Event:      EventStarted,
	}
	c.Announce(context.Background(), req)

	if gotQuery.Get("port") != "6881" {
		t.Errorf("port: want 6881, got %s", gotQuery.Get("port"))
	}
	if gotQuery.Get("uploaded") != "100" {
		t.Errorf("uploaded: want 100, got %s", gotQuery.Get("uploaded"))
	}
	if gotQuery.Get("downloaded") != "200" {
		t.Errorf("downloaded: want 200, got %s", gotQuery.Get("downloaded"))
	}
	if gotQuery.Get("left") != "300" {
		t.Errorf("left: want 300, got %s", gotQuery.Get("left"))
	}
	if gotQuery.Get("event") != "started" {
		t.Errorf("event: want started, got %s", gotQuery.Get("event"))
	}
	if gotQuery.Get("compact") != "0" {
		t.Errorf("compact: want 0, got %s", gotQuery.Get("compact"))
	}
}

func TestAnnounce_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newTestHTTPTracker(t, srv.URL+"/announce")
	// should not panic, returns empty response
	got, err := c.Announce(context.Background(), AnnounceRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Interval != 0 {
		t.Errorf("expected zero response on server error")
	}
}

func TestPeerList_CompactPeers(t *testing.T) {
	// 192.168.1.1:6881 and 10.0.0.1:51413 encoded as compact binary
	compact := []byte{
		192, 168, 1, 1, 0x1A, 0xE1, // 192.168.1.1:6881
		10, 0, 0, 1, 0xC8, 0xD5, // 10.0.0.1:51413
	}
	// wrap in bencode response: peers is a binary string
	body := fmt.Sprintf("d8:intervali60e8:completei0e10:incompletei0e5:peers%d:%se", len(compact), compact)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	c := newTestHTTPTracker(t, srv.URL+"/announce")
	got, err := c.Announce(context.Background(), AnnounceRequest{Compact: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Peers.Peers) != 2 {
		t.Fatalf("peers: want 2, got %d", len(got.Peers.Peers))
	}
	if got.Peers.Peers[0].Ip != "192.168.1.1" {
		t.Errorf("peer 0 ip: want 192.168.1.1, got %s", got.Peers.Peers[0].Ip)
	}
	if got.Peers.Peers[0].Port != 6881 {
		t.Errorf("peer 0 port: want 6881, got %d", got.Peers.Peers[0].Port)
	}
	if got.Peers.Peers[1].Ip != "10.0.0.1" {
		t.Errorf("peer 1 ip: want 10.0.0.1, got %s", got.Peers.Peers[1].Ip)
	}
	if got.Peers.Peers[1].Port != 51413 {
		t.Errorf("peer 1 port: want 51413, got %d", got.Peers.Peers[1].Port)
	}
}

func TestPeerList_NonCompactPeers(t *testing.T) {
	// non-compact: peers is a list of dicts
	body := "d8:intervali60e8:completei0e10:incompletei0e5:peersld2:ip9:127.0.0.14:porti6881eed2:ip7:1.2.3.44:porti1234eeee"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	c := newTestHTTPTracker(t, srv.URL+"/announce")
	got, err := c.Announce(context.Background(), AnnounceRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Peers.Peers) != 2 {
		t.Fatalf("peers: want 2, got %d", len(got.Peers.Peers))
	}
	if got.Peers.Peers[0].Ip != "127.0.0.1" {
		t.Errorf("peer 0 ip: want 127.0.0.1, got %s", got.Peers.Peers[0].Ip)
	}
	if got.Peers.Peers[0].Port != 6881 {
		t.Errorf("peer 0 port: want 6881, got %d", got.Peers.Peers[0].Port)
	}
	if got.Peers.Peers[1].Ip != "1.2.3.4" {
		t.Errorf("peer 1 ip: want 1.2.3.4, got %s", got.Peers.Peers[1].Ip)
	}
	if got.Peers.Peers[1].Port != 1234 {
		t.Errorf("peer 1 port: want 1234, got %d", got.Peers.Peers[1].Port)
	}
}

func TestAnnounce_NetworkError(t *testing.T) {
	c := newTestHTTPTracker(t, "http://127.0.0.1:1/announce") // nothing listening
	_, err := c.Announce(context.Background(), AnnounceRequest{})
	if err == nil {
		t.Error("expected error on network failure, got nil")
	}
}
