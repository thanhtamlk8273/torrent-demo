package tracker_client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"torrent-demo/internal/bencode"
)

type HttpTrackerClient struct {
	baseURL *url.URL
	client  *http.Client
}

func encodeBytes(b []byte) string {
	var s strings.Builder
	for _, c := range b {
		fmt.Fprintf(&s, "%%%02X", c)
	}
	return s.String()
}

func newHTTPTracker(rawURL *url.URL, options options) (*HttpTrackerClient, error) {
	return &HttpTrackerClient{
		baseURL: rawURL,
		client: &http.Client{
			Timeout: options.timeout,
		},
	}, nil
}

func (c *HttpTrackerClient) Announce(ctx context.Context, req AnnounceRequest) (*AnnounceResponse, error) {
	u := *c.baseURL
	params := url.Values{}
	params.Set("port", strconv.FormatInt(int64(req.Port), 10))
	params.Set("uploaded", strconv.FormatInt(req.Uploaded, 10))
	params.Set("downloaded", strconv.FormatInt(req.Downloaded, 10))
	params.Set("left", strconv.FormatInt(req.Left, 10))
	params.Set("compact", strconv.FormatInt(req.Compact, 10))
	params.Set("event", req.Event.String())
	u.RawQuery = params.Encode() +
		"&info_hash=" + encodeBytes(req.InfoHash[:]) +
		"&peer_id=" + encodeBytes(req.PeerID[:])

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	httpResp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, err
	}

	var resp AnnounceResponse
	bencode.Unmarshal(body, &resp)
	return &resp, nil
}
