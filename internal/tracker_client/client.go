package tracker_client

import (
	"context"
	"fmt"
	"net/url"
	"time"
	"torrent-demo/internal/bencode"
)

type AnnounceRequest struct {
	InfoHash   [20]byte
	PeerID     [20]byte
	Port       uint16
	Uploaded   int64
	Downloaded int64
	Left       int64
	Compact    int64
	Event      AnnounceEvent
}

type AnnounceResponse struct {
	Interval   int      `bencode:"interval"`
	Complete   int      `bencode:"complete"`
	Incomplete int      `bencode:"incomplete"`
	Peers      PeerList `bencode:"peers"`
}

type PeerList struct {
	Peers []Peer
}

type AnnounceEvent int

const (
	EventNone AnnounceEvent = iota
	EventStarted
	EventStopped
	EventCompleted
)

func (e AnnounceEvent) String() string {
	switch e {
	case EventStarted:
		return "started"
	case EventStopped:
		return "stopped"
	case EventCompleted:
		return "completed"
	default:
		return ""
	}
}

type Peer struct {
	Ip   string `bencode:"ip"`
	Port int    `bencode:"port"`
}

type options struct {
	timeout time.Duration
}

type Option func(*options)

func WithTimeout(d time.Duration) Option {
	return func(o *options) { o.timeout = d }
}

type TrackerClient interface {
	Announce(ctx context.Context, req AnnounceRequest) (*AnnounceResponse, error)
}

func NewTrackerClient(rawURL string, opts ...Option) (TrackerClient, error) {
	o := &options{
		timeout: 15 * time.Second,
	}
	for _, opt := range opts {
		opt(o)
	}

	u, _ := url.Parse(rawURL)
	switch u.Scheme {
	case "http", "https":
		return newHTTPTracker(u, *o)
	case "udp":
		return nil, fmt.Errorf("not supported tracker protocol")
	default:
		return nil, fmt.Errorf("not supported tracker protocol")
	}
}

func (pl *PeerList) UnmarshalBencode(data any) error {
	switch v := data.(type) {
	case string:
		b := []byte(v)
		for i := 0; i+6 <= len(b); i += 6 {
			pl.Peers = append(pl.Peers, Peer{
				Ip:   fmt.Sprintf("%d.%d.%d.%d", b[i], b[i+1], b[i+2], b[i+3]),
				Port: int(b[i+4])<<8 | int(b[i+5]),
			})
		}
	case []any:
		if err := bencode.FillDecoded(data, &pl.Peers); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unexpected type")
	}

	return nil
}
