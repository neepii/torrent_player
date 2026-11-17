package tracker_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackpal/bencode-go"
	"github.com/neepii/torrent_player/src/tracker"
)

func validRequest() tracker.AnnounceRequest {
	var infoHash [20]byte
	var peerID [20]byte
	for i := range infoHash {
		infoHash[i] = byte(i)
		peerID[i] = byte(20 + i)
	}
	return tracker.AnnounceRequest{
		InfoHash:   infoHash,
		PeerID:     peerID,
		Port:       6881,
		Uploaded:   1234,
		Downloaded: 5678,
		Left:       91011,
		Event:      tracker.EventStarted,
	}
}

func newServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

// marshalBencode encodes v using bencode, failing the test on error.
func marshalBencode(t *testing.T, v any) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := bencode.Marshal(&buf, v); err != nil {
		t.Fatalf("bencode.Marshal() error = %v", err)
	}
	return buf.Bytes()
}

func writeBencode(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "text/plain")
	if err := bencode.Marshal(w, v); err != nil {
		t.Errorf("bencode.Marshal() error = %v", err)
	}
}

// percentEncode is the expected binary URL encoding: every byte escaped.
func percentEncode(b []byte) string {
	const hex = "0123456789ABCDEF"
	var sb strings.Builder
	for _, c := range b {
		sb.WriteByte('%')
		sb.WriteByte(hex[c>>4])
		sb.WriteByte(hex[c&0x0f])
	}
	return sb.String()
}

func compactIPv4(t *testing.T, ip string, port uint16) []byte {
	t.Helper()
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		t.Fatalf("netip.ParseAddr(%q) error = %v", ip, err)
	}
	if !addr.Is4() {
		t.Fatalf("%q is not an IPv4 address", ip)
	}
	a := addr.As4()
	b := make([]byte, 6)
	copy(b, a[:])
	binary.BigEndian.PutUint16(b[4:], port)
	return b
}

func compactIPv6(t *testing.T, ip string, port uint16) []byte {
	t.Helper()
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		t.Fatalf("netip.ParseAddr(%q) error = %v", ip, err)
	}
	if !addr.Is6() || addr.Is4In6() {
		t.Fatalf("%q is not an IPv6 address", ip)
	}
	a := addr.As16()
	b := make([]byte, 18)
	copy(b, a[:])
	binary.BigEndian.PutUint16(b[16:], port)
	return b
}

func TestAnnounceSendsRequiredParameters(t *testing.T) {
	infoHash := [20]byte{0x00, 0x20, 0x25, 0x2b, 0x7f, 0x80, 0xff}
	for i := len(infoHash) - 1; i >= 7; i-- {
		infoHash[i] = byte(i * 7)
	}
	peerID := [20]byte{0x00, 0x01, 0x20, 0x2b, 0x25, 0xfe, 0xff}
	for i := len(peerID) - 1; i >= 7; i-- {
		peerID[i] = byte(i * 13)
	}

	req := tracker.AnnounceRequest{
		InfoHash:   infoHash,
		PeerID:     peerID,
		Port:       51413,
		Uploaded:   111,
		Downloaded: 222,
		Left:       333,
		Event:      tracker.EventStarted,
		NumWant:    50,
	}

	type captured struct {
		rawQuery  string
		query     url.Values
		userAgent string
	}
	got := make(chan captured, 1)

	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		got <- captured{
			rawQuery:  r.URL.RawQuery,
			query:     r.URL.Query(),
			userAgent: r.UserAgent(),
		}
		writeBencode(t, w, map[string]any{
			"interval":     int64(1800),
			"min interval": int64(900),
			"complete":     int64(5),
			"incomplete":   int64(2),
			"peers":        string(compactIPv4(t, "192.0.2.10", 6001)),
		})
	})

	resp, err := tracker.NewClient().Announce(context.Background(), srv.URL+"/announce?passkey=abc123", req)
	if err != nil {
		t.Fatalf("Announce() error = %v", err)
	}
	c := <-got

	// Binary values must be percent-encoded byte by byte, with no "+".
	if want := "info_hash=" + percentEncode(infoHash[:]); !strings.Contains(c.rawQuery, want) {
		t.Errorf("raw query %q does not contain %q", c.rawQuery, want)
	}
	if want := "peer_id=" + percentEncode(peerID[:]); !strings.Contains(c.rawQuery, want) {
		t.Errorf("raw query %q does not contain %q", c.rawQuery, want)
	}
	if strings.Contains(c.rawQuery, "+") {
		t.Errorf("raw query %q contains form-style space encoding", c.rawQuery)
	}

	wantParams := map[string]string{
		"info_hash":  string(infoHash[:]),
		"peer_id":    string(peerID[:]),
		"port":       "51413",
		"uploaded":   "111",
		"downloaded": "222",
		"left":       "333",
		"event":      "started",
		"compact":    "1",
		"numwant":    "50",
		"passkey":    "abc123",
	}
	for key, want := range wantParams {
		if value := c.query.Get(key); value != want {
			t.Errorf("query %q = %q, want %q", key, value, want)
		}
	}
	if c.userAgent == "" {
		t.Error("User-Agent header is empty, want non-empty")
	}

	if resp.Interval != 30*time.Minute {
		t.Errorf("Interval = %v, want 30m", resp.Interval)
	}
	if resp.MinInterval != 15*time.Minute {
		t.Errorf("MinInterval = %v, want 15m", resp.MinInterval)
	}
	if resp.Complete != 5 || resp.Incomplete != 2 {
		t.Errorf("Complete, Incomplete = %d, %d; want 5, 2", resp.Complete, resp.Incomplete)
	}
	if len(resp.Peers) != 1 {
		t.Fatalf("len(Peers) = %d, want 1", len(resp.Peers))
	}
	if got := resp.Peers[0]; got.Addr.String() != "192.0.2.10" || got.Port != 6001 {
		t.Errorf("Peers[0] = %v, want 192.0.2.10:6001", got)
	}
}

func TestAnnounceEvents(t *testing.T) {
	tests := []struct {
		name      string
		event     tracker.Event
		wantParam string
		wantSent  bool
	}{
		{name: "started", event: tracker.EventStarted, wantParam: "started", wantSent: true},
		{name: "regular omits parameter", event: tracker.EventRegular, wantSent: false},
		{name: "stopped", event: tracker.EventStopped, wantParam: "stopped", wantSent: true},
		{name: "completed", event: tracker.EventCompleted, wantParam: "completed", wantSent: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := make(chan url.Values, 1)
			srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
				got <- r.URL.Query()
				writeBencode(t, w, map[string]any{
					"interval": int64(60),
					"peers":    "",
				})
			})

			req := validRequest()
			req.Event = tc.event
			req.NumWant = 0
			if _, err := tracker.NewClient().Announce(context.Background(), srv.URL, req); err != nil {
				t.Fatalf("Announce() error = %v", err)
			}

			query := <-got
			values, present := query["event"]
			if present != tc.wantSent {
				t.Fatalf("event present = %v, want %v (query = %v)", present, tc.wantSent, query)
			}
			if tc.wantSent && (len(values) != 1 || values[0] != tc.wantParam) {
				t.Errorf("event = %v, want %q", values, tc.wantParam)
			}
			if _, present := query["numwant"]; present {
				t.Errorf("numwant present with NumWant = 0 (query = %v)", query)
			}
		})
	}
}

func TestAnnounceParsesCompactPeers(t *testing.T) {
	first := compactIPv4(t, "10.1.2.3", 6881)
	second := compactIPv4(t, "192.0.2.55", 65535)

	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeBencode(t, w, map[string]any{
			"interval": int64(120),
			"peers":    string(append(first, second...)),
		})
	})

	resp, err := tracker.NewClient().Announce(context.Background(), srv.URL, validRequest())
	if err != nil {
		t.Fatalf("Announce() error = %v", err)
	}

	want := []tracker.Peer{
		{Addr: netip.MustParseAddr("10.1.2.3"), Port: 6881},
		{Addr: netip.MustParseAddr("192.0.2.55"), Port: 65535},
	}
	if len(resp.Peers) != len(want) {
		t.Fatalf("len(Peers) = %d, want %d", len(resp.Peers), len(want))
	}
	for i := range want {
		if resp.Peers[i] != want[i] {
			t.Errorf("Peers[%d] = %v, want %v", i, resp.Peers[i], want[i])
		}
	}
}

func TestAnnounceParsesNonCompactPeers(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeBencode(t, w, map[string]any{
			"interval": int64(60),
			"peers": []any{
				map[string]any{"peer id": string(bytes.Repeat([]byte{0xaa}, 20)), "ip": "127.0.0.1", "port": int64(6881)},
				map[string]any{"ip": "2001:db8::1", "port": int64(51413)},
			},
		})
	})

	resp, err := tracker.NewClient().Announce(context.Background(), srv.URL, validRequest())
	if err != nil {
		t.Fatalf("Announce() error = %v", err)
	}

	id := [20]byte{}
	copy(id[:], bytes.Repeat([]byte{0xaa}, 20))
	want := []tracker.Peer{
		{Addr: netip.MustParseAddr("127.0.0.1"), Port: 6881, ID: &id},
		{Addr: netip.MustParseAddr("2001:db8::1"), Port: 51413},
	}
	if len(resp.Peers) != len(want) {
		t.Fatalf("len(Peers) = %d, want %d", len(resp.Peers), len(want))
	}
	for i := range want {
		if resp.Peers[i].Addr != want[i].Addr || resp.Peers[i].Port != want[i].Port || (want[i].ID == nil) != (resp.Peers[i].ID == nil) || (want[i].ID != nil && *want[i].ID != *resp.Peers[i].ID) {
			t.Errorf("Peers[%d] = %v, want %v", i, resp.Peers[i], want[i])
		}
	}
}

func TestAnnounceParsesCompactIPv6Peers(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeBencode(t, w, map[string]any{
			"interval": int64(60),
			"peers":    "",
			"peers6":   string(compactIPv6(t, "2001:db8::42", 6881)),
		})
	})

	resp, err := tracker.NewClient().Announce(context.Background(), srv.URL, validRequest())
	if err != nil {
		t.Fatalf("Announce() error = %v", err)
	}
	want := tracker.Peer{Addr: netip.MustParseAddr("2001:db8::42"), Port: 6881}
	if len(resp.Peers) != 1 || resp.Peers[0] != want {
		t.Errorf("Peers = %v, want [%v]", resp.Peers, want)
	}
}

func TestAnnounceZeroPeers(t *testing.T) {
	tests := []struct {
		name  string
		peers any
	}{
		{name: "empty compact", peers: ""},
		{name: "empty list", peers: []any{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
				writeBencode(t, w, map[string]any{
					"interval": int64(60),
					"peers":    tc.peers,
				})
			})

			resp, err := tracker.NewClient().Announce(context.Background(), srv.URL, validRequest())
			if err != nil {
				t.Fatalf("Announce() error = %v", err)
			}
			if len(resp.Peers) != 0 {
				t.Errorf("len(Peers) = %d, want 0", len(resp.Peers))
			}
		})
	}
}

func TestAnnounceFailureReason(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeBencode(t, w, map[string]any{
			"failure reason": "torrent not registered",
		})
	})

	_, err := tracker.NewClient().Announce(context.Background(), srv.URL, validRequest())
	if err == nil {
		t.Fatal("Announce() error = nil, want failure response error")
	}
	if !errors.Is(err, tracker.ErrFailureResponse) {
		t.Errorf("errors.Is(err, ErrFailureResponse) = false, err = %v", err)
	}
	var failure *tracker.FailureError
	if !errors.As(err, &failure) {
		t.Fatalf("errors.As(err, *FailureError) = false, err = %v", err)
	}
	if failure.Reason != "torrent not registered" {
		t.Errorf("FailureError.Reason = %q, want %q", failure.Reason, "torrent not registered")
	}
}

func TestAnnounceHTTPError(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	})

	_, err := tracker.NewClient().Announce(context.Background(), srv.URL, validRequest())
	if err == nil {
		t.Fatal("Announce() error = nil, want HTTP error")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("error = %v, want it to mention HTTP 503", err)
	}
}

func TestAnnounceMalformedResponses(t *testing.T) {
	tests := []struct {
		name string
		body []byte
	}{
		{name: "not bencode", body: []byte("not bencode")},
		{name: "not a dictionary", body: marshalBencode(t, []any{int64(1)})},
		{name: "failure reason not string", body: marshalBencode(t, map[string]any{"failure reason": int64(1)})},
		{name: "missing interval", body: marshalBencode(t, map[string]any{"peers": ""})},
		{name: "interval not integer", body: marshalBencode(t, map[string]any{"interval": "60", "peers": ""})},
		{name: "zero interval", body: marshalBencode(t, map[string]any{"interval": int64(0), "peers": ""})},
		{name: "missing peers", body: marshalBencode(t, map[string]any{"interval": int64(60)})},
		{name: "peers wrong type", body: marshalBencode(t, map[string]any{"interval": int64(60), "peers": int64(1)})},
		{name: "bad compact length", body: marshalBencode(t, map[string]any{"interval": int64(60), "peers": "abc"})},
		{name: "noncompact ip not string", body: marshalBencode(t, map[string]any{
			"interval": int64(60),
			"peers":    []any{map[string]any{"ip": int64(1), "port": int64(6881)}},
		})},
		{name: "noncompact invalid ip", body: marshalBencode(t, map[string]any{
			"interval": int64(60),
			"peers":    []any{map[string]any{"ip": "not-an-ip", "port": int64(6881)}},
		})},
		{name: "noncompact missing port", body: marshalBencode(t, map[string]any{
			"interval": int64(60),
			"peers":    []any{map[string]any{"ip": "127.0.0.1"}},
		})},
		{name: "noncompact port out of range", body: marshalBencode(t, map[string]any{
			"interval": int64(60),
			"peers":    []any{map[string]any{"ip": "127.0.0.1", "port": int64(70000)}},
		})},
		{name: "peer not dictionary", body: marshalBencode(t, map[string]any{
			"interval": int64(60),
			"peers":    []any{"127.0.0.1"},
		})},
		{name: "bad peers6 length", body: marshalBencode(t, map[string]any{
			"interval": int64(60),
			"peers":    "",
			"peers6":   "abc",
		})},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				_, _ = w.Write(tc.body)
			})

			_, err := tracker.NewClient().Announce(context.Background(), srv.URL, validRequest())
			if err == nil {
				t.Fatal("Announce() error = nil, want malformed response error")
			}
		})
	}
}

func TestAnnounceTimeout(t *testing.T) {
	block := make(chan struct{})
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		<-block
	})
	defer close(block)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := tracker.NewClient().Announce(ctx, srv.URL, validRequest())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Announce() error = nil, want timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("errors.Is(err, context.DeadlineExceeded) = false, err = %v", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("Announce() took %v, want it to respect the context deadline", elapsed)
	}
}

func TestAnnounceClientTimeout(t *testing.T) {
	block := make(chan struct{})
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		<-block
	})
	defer close(block)

	client := &tracker.Client{
		HTTPClient: &http.Client{Timeout: 100 * time.Millisecond},
	}

	start := time.Now()
	_, err := client.Announce(context.Background(), srv.URL, validRequest())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Announce() error = nil, want timeout error")
	}
	if elapsed > 2*time.Second {
		t.Errorf("Announce() took %v, want it to respect the client timeout", elapsed)
	}
}

func TestAnnounceRejectsInvalidRequests(t *testing.T) {
	tests := []struct {
		name string
		url  string
		req  tracker.AnnounceRequest
	}{
		{name: "empty URL", url: "", req: validRequest()},
		{name: "unsupported scheme", url: "udp://tracker.example:80/announce", req: validRequest()},
		{name: "missing host", url: "http:///announce", req: validRequest()},
		{name: "invalid event", url: "http://tracker.example/announce", req: func() tracker.AnnounceRequest {
			req := validRequest()
			req.Event = tracker.Event("paused")
			return req
		}()},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tracker.NewClient().Announce(context.Background(), tc.url, tc.req); err == nil {
				t.Fatal("Announce() error = nil, want error")
			}
		})
	}
}

func TestPeerString(t *testing.T) {
	peer := tracker.Peer{Addr: netip.MustParseAddr("192.0.2.1"), Port: 6881}
	if got, want := peer.String(), "192.0.2.1:6881"; got != want {
		t.Errorf("Peer.String() = %q, want %q", got, want)
	}

	ipv6 := tracker.Peer{Addr: netip.MustParseAddr("2001:db8::1"), Port: 6881}
	if got, want := ipv6.String(), "[2001:db8::1]:6881"; got != want {
		t.Errorf("Peer.String() = %q, want %q", got, want)
	}
}

// TestAnnounceRequestURLShape documents the generated request shape without a
// server, including preservation of an existing query string.
func TestAnnounceRequestURLShape(t *testing.T) {
	req := validRequest()
	req.Port = 6881

	// The URL is unexported, so exercise it through a server that records the
	// raw query.
	rawQuery := make(chan string, 1)
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		rawQuery <- r.URL.RawQuery
		writeBencode(t, w, map[string]any{"interval": int64(60), "peers": ""})
	})

	if _, err := tracker.NewClient().Announce(context.Background(), srv.URL+"/announce?a=1&b=2", req); err != nil {
		t.Fatalf("Announce() error = %v", err)
	}
	query := <-rawQuery
	if !strings.Contains(query, "a=1") || !strings.Contains(query, "b=2") {
		t.Errorf("raw query %q lost existing parameters", query)
	}
}
