// Package tracker implements BitTorrent HTTP tracker announces (BEP 3) with
// support for compact peer lists (BEP 23) and non-compact peer lists.
package tracker

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackpal/bencode-go"
	"github.com/neepii/torrent_player/src/pipeline"
)

const (
	// DefaultTimeout is the HTTP timeout used by NewClient.
	DefaultTimeout = 15 * time.Second

	// maxResponseSize bounds how much of a tracker response is read.
	maxResponseSize = 4 << 20

	// errPrefix is prepended to every error returned by this package.
	errPrefix = "tracker:"
)

// ErrFailureResponse is wrapped by FailureError so callers can test tracker
// failure responses with errors.Is.
var ErrFailureResponse = errors.New("tracker failure response")

// errf returns a new error whose message is prefixed with errPrefix.
func errf(format string, args ...any) error {
	return fmt.Errorf(errPrefix+" "+format, args...)
}

// Event is the "event" announce parameter. The regular announce is the absence
// of the parameter (EventRegular), as defined by BEP 3.
type Event string

const (
	EventRegular   Event = ""
	EventStarted   Event = "started"
	EventCompleted Event = "completed"
	EventStopped   Event = "stopped"
)

func (e Event) valid() bool {
	switch e {
	case EventRegular, EventStarted, EventCompleted, EventStopped:
		return true
	default:
		return false
	}
}

// AnnounceRequest holds the parameters of a single announce. EventRegular is
// sent without an "event" parameter.
type AnnounceRequest struct {
	InfoHash   [20]byte
	PeerID     [20]byte
	Port       uint16
	Uploaded   int64
	Downloaded int64
	Left       int64
	Event      Event
	// NumWant is the requested peer count. Zero omits the parameter and lets
	// the tracker choose its default.
	NumWant int
}

// Peer is the peer data shared with the download pipeline.
type Peer = pipeline.PeerData

// Response is a successful, parsed announce response.
type Response struct {
	Interval       time.Duration
	MinInterval    time.Duration
	Complete       int
	Incomplete     int
	Peers          []Peer
	WarningMessage string
}

// FailureError reports a tracker "failure reason" response. It unwraps to
// ErrFailureResponse.
type FailureError struct {
	Reason string
}

func (e *FailureError) Error() string {
	return errPrefix + " failure response: " + e.Reason
}

// Unwrap makes errors.Is(err, ErrFailureResponse) report true.
func (e *FailureError) Unwrap() error {
	return ErrFailureResponse
}

// Client performs HTTP tracker announces.
type Client struct {
	// HTTPClient is used for requests. A nil value falls back to
	// http.DefaultClient.
	HTTPClient *http.Client
	// UserAgent is sent when non-empty.
	UserAgent string
}

// NewClient returns a Client with DefaultTimeout.
func NewClient() *Client {
	return &Client{
		HTTPClient: &http.Client{Timeout: DefaultTimeout},
		UserAgent:  "torrent_player/1.0",
	}
}

// Announce performs a single announce and parses the tracker response.
//
// Binary values (info_hash and peer_id) are percent-encoded byte by byte, so
// arbitrary bytes are transmitted unchanged. Existing query parameters in
// announceURL (for example a passkey) are preserved.
func (c *Client) Announce(ctx context.Context, announceURL string, req AnnounceRequest) (*Response, error) {
	u, err := buildAnnounceURL(announceURL, req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, errf("create announce request: %w", err)
	}
	if c.UserAgent != "" {
		httpReq.Header.Set("User-Agent", c.UserAgent)
	}

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		return nil, errf("announce request failed: %w", err)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(httpResp.Body, maxResponseSize+1))
	if err != nil {
		return nil, errf("read announce response: %w", err)
	}
	if len(body) > maxResponseSize {
		return nil, errf("announce response exceeds %d bytes", maxResponseSize)
	}
	if httpResp.StatusCode < 200 || httpResp.StatusCode > 299 {
		return nil, errf("tracker returned HTTP %s", httpResp.Status)
	}

	return parseResponse(body)
}

// Announce performs one announce using a default Client.
func Announce(ctx context.Context, announceURL string, req AnnounceRequest) (*Response, error) {
	return NewClient().Announce(ctx, announceURL, req)
}

func buildAnnounceURL(announceURL string, req AnnounceRequest) (string, error) {
	if announceURL == "" {
		return "", errf("empty announce URL")
	}
	u, err := url.Parse(announceURL)
	if err != nil {
		return "", errf("invalid announce URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errf("unsupported announce URL scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return "", errf("announce URL has no host")
	}
	if !req.Event.valid() {
		return "", errf("invalid event %q", req.Event)
	}

	params := make([]string, 0, 8)
	params = append(params,
		"info_hash="+encodeBinary(req.InfoHash[:]),
		"peer_id="+encodeBinary(req.PeerID[:]),
		"port="+strconv.FormatUint(uint64(req.Port), 10),
		"uploaded="+strconv.FormatInt(req.Uploaded, 10),
		"downloaded="+strconv.FormatInt(req.Downloaded, 10),
		"left="+strconv.FormatInt(req.Left, 10),
		"compact=1",
	)
	if req.Event != EventRegular {
		params = append(params, "event="+url.QueryEscape(string(req.Event)))
	}
	if req.NumWant > 0 {
		params = append(params, "numwant="+strconv.Itoa(req.NumWant))
	}

	encoded := strings.Join(params, "&")
	if u.RawQuery != "" {
		u.RawQuery += "&" + encoded
	} else {
		u.RawQuery = encoded
	}
	return u.String(), nil
}

const upperHex = "0123456789ABCDEF"

// encodeBinary percent-encodes every byte. It is used for binary values such as
// info_hash and peer_id, where form-style escaping (spaces as "+") would
// corrupt the value.
func encodeBinary(b []byte) string {
	var sb strings.Builder
	sb.Grow(len(b) * 3)
	for _, c := range b {
		sb.WriteByte('%')
		sb.WriteByte(upperHex[c>>4])
		sb.WriteByte(upperHex[c&0x0f])
	}
	return sb.String()
}

func parseResponse(body []byte) (*Response, error) {
	decoded, err := bencode.Decode(bytes.NewReader(body))
	if err != nil {
		return nil, errf("invalid bencode response: %w", err)
	}
	m, ok := decoded.(map[string]any)
	if !ok {
		return nil, errf("response is not a dictionary")
	}

	if v, ok := m["failure reason"]; ok {
		reason, ok := v.(string)
		if !ok {
			return nil, errf("%q is not a string", "failure reason")
		}
		return nil, &FailureError{Reason: reason}
	}

	interval, err := requiredInt(m, "interval")
	if err != nil {
		return nil, err
	}
	if interval <= 0 || interval > math.MaxInt64/int64(time.Second) {
		return nil, errf("invalid interval %d", interval)
	}

	resp := &Response{Interval: time.Duration(interval) * time.Second}

	if v, ok := m["min interval"]; ok {
		minInterval, ok := v.(int64)
		if !ok {
			return nil, errf("%q is not an integer", "min interval")
		}
		if minInterval < 0 || minInterval > math.MaxInt64/int64(time.Second) {
			return nil, errf("invalid min interval %d", minInterval)
		}
		resp.MinInterval = time.Duration(minInterval) * time.Second
	}
	if resp.Complete, err = optionalCount(m, "complete"); err != nil {
		return nil, err
	}
	if resp.Incomplete, err = optionalCount(m, "incomplete"); err != nil {
		return nil, err
	}
	if v, ok := m["warning message"]; ok {
		warning, ok := v.(string)
		if !ok {
			return nil, errf("%q is not a string", "warning message")
		}
		resp.WarningMessage = warning
	}

	peers, ok := m["peers"]
	if !ok {
		return nil, errf("missing %q field", "peers")
	}
	resp.Peers, err = parsePeers(peers)
	if err != nil {
		return nil, err
	}

	if v, ok := m["peers6"]; ok {
		compact, ok := v.(string)
		if !ok {
			return nil, errf("%q is not a string", "peers6")
		}
		peers6, err := parseCompactPeers([]byte(compact), 16)
		if err != nil {
			return nil, err
		}
		resp.Peers = append(resp.Peers, peers6...)
	}

	return resp, nil
}

func requiredInt(m map[string]any, key string) (int64, error) {
	v, ok := m[key]
	if !ok {
		return 0, errf("missing %q field", key)
	}
	n, ok := v.(int64)
	if !ok {
		return 0, errf("%q is not an integer", key)
	}
	return n, nil
}

func optionalCount(m map[string]any, key string) (int, error) {
	v, ok := m[key]
	if !ok {
		return 0, nil
	}
	n, ok := v.(int64)
	if !ok {
		return 0, errf("%q is not an integer", key)
	}
	if n < 0 {
		return 0, errf("%q must be non-negative", key)
	}
	return int(n), nil
}

func parsePeers(v any) ([]Peer, error) {
	switch peers := v.(type) {
	case string:
		return parseCompactPeers([]byte(peers), 4)
	case []any:
		return parseNonCompactPeers(peers)
	default:
		return nil, errf("malformed peers field: got %T, want string or list", v)
	}
}

// parseCompactPeers parses BEP 23 compact peers: addrLen address bytes followed
// by a big-endian 16-bit port.
func parseCompactPeers(data []byte, addrLen int) ([]Peer, error) {
	entryLen := addrLen + 2
	if len(data)%entryLen != 0 {
		return nil, errf(
			"malformed compact peer list: %d bytes is not a multiple of %d",
			len(data), entryLen,
		)
	}

	peers := make([]Peer, 0, len(data)/entryLen)
	for i := 0; i < len(data); i += entryLen {
		var addr netip.Addr
		if addrLen == 4 {
			var b [4]byte
			copy(b[:], data[i:i+4])
			addr = netip.AddrFrom4(b)
		} else {
			var b [16]byte
			copy(b[:], data[i:i+16])
			addr = netip.AddrFrom16(b)
		}
		port := binary.BigEndian.Uint16(data[i+addrLen : i+entryLen])
		peers = append(peers, Peer{AddrPort: netip.AddrPortFrom(addr, port)})
	}
	return peers, nil
}

func parseNonCompactPeers(list []any) ([]Peer, error) {
	peers := make([]Peer, 0, len(list))
	for i, entry := range list {
		m, ok := entry.(map[string]any)
		if !ok {
			return nil, errf("malformed peer %d: got %T, want dictionary", i, entry)
		}

		ipValue, ok := m["ip"]
		if !ok {
			return nil, errf("malformed peer %d: missing %q field", i, "ip")
		}
		ip, ok := ipValue.(string)
		if !ok {
			return nil, errf("malformed peer %d: %q is not a string", i, "ip")
		}
		addr, err := netip.ParseAddr(ip)
		if err != nil {
			return nil, errf("malformed peer %d: invalid IP %q: %w", i, ip, err)
		}

		portValue, ok := m["port"]
		if !ok {
			return nil, errf("malformed peer %d: missing %q field", i, "port")
		}
		port, ok := portValue.(int64)
		if !ok {
			return nil, errf("malformed peer %d: %q is not an integer", i, "port")
		}
		if port <= 0 || port > math.MaxUint16 {
			return nil, errf("malformed peer %d: invalid port %d", i, port)
		}

		var id *[20]byte
		if value, exists := m["peer id"]; exists {
			raw, ok := value.(string)
			if !ok || len(raw) != 20 {
				return nil, errf("malformed peer %d: invalid peer id", i)
			}
			id = new([20]byte)
			copy(id[:], raw)
		}
		peers = append(peers, Peer{AddrPort: netip.AddrPortFrom(addr, uint16(port)), ID: id})
	}
	return peers, nil
}
