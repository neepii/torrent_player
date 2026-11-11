package torrent_test

import (
	"bytes"
	"errors"
	"io"
	"net/netip"
	"testing"

	"github.com/neepii/torrent_player/src/pipeline"
	"github.com/neepii/torrent_player/src/torrent"
)

type fakeParser struct {
	torrent *torrent.TorrentData
	err     error
}

func (f fakeParser) Parse([]byte) (*torrent.TorrentData, error) { return f.torrent, f.err }

type fakeTracker struct {
	peers []pipeline.PeerData
	err   error
}

func (f fakeTracker) Announce(*torrent.TorrentData) ([]pipeline.PeerData, error) {
	return f.peers, f.err
}

type fakePieceSource struct {
	pieces [][]byte
	closed bool
}

func (f *fakePieceSource) ReadPiece() ([]byte, error) {
	if len(f.pieces) == 0 {
		return nil, io.EOF
	}
	p := f.pieces[0]
	f.pieces = f.pieces[1:]
	return p, nil
}

func (f *fakePieceSource) Close() error {
	f.closed = true
	return nil
}

type fakePlayback struct {
	buf bytes.Buffer
}

func (f *fakePlayback) Write(p []byte) (int, error) { return f.buf.Write(p) }
func (f *fakePlayback) Close() error                { return nil }

// Compile-time checks that the fakes satisfy the pipeline interfaces.
var (
	_ pipeline.Parser          = fakeParser{}
	_ pipeline.Tracker         = fakeTracker{}
	_ pipeline.PieceSource     = (*fakePieceSource)(nil)
	_ pipeline.PlaybackAdapter = (*fakePlayback)(nil)
)

func TestPipelineIntegrationPointsCanBeFaked(t *testing.T) {
	meta := &torrent.TorrentData{
		AnnounceURL: "http://tracker.example/announce",
		Info:        torrent.TorrentInfoDict{Name: "sample.mp4"},
	}

	parser := fakeParser{torrent: meta}
	tracker := fakeTracker{peers: []pipeline.PeerData{
		{AddrPort: netip.AddrPortFrom(netip.AddrFrom4([4]byte{127,0,0,1}), 6881)},
	}}
	source := &fakePieceSource{pieces: [][]byte{[]byte("piece0"), []byte("piece1")}}
	playback := &fakePlayback{}

	got, err := parser.Parse(nil)
	if err != nil {
		t.Fatalf("fake parser: %v", err)
	}
	if got != meta {
		t.Fatal("fake parser did not return the configured torrent")
	}

	peers, err := tracker.Announce(got)
	if err != nil {
		t.Fatalf("fake tracker: %v", err)
	}
	expected_ip := netip.AddrFrom4([4]byte{127, 0, 0, 1})
	expected_port := uint16(6881)
	if len(peers) != 1 || peers[0].AddrPort.Addr() != expected_ip || peers[0].AddrPort.Port() != expected_port {
		t.Errorf("fake tracker peers = %+v", peers)
	}

	for {
		piece, err := source.ReadPiece()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("fake piece source: %v", err)
		}
		if _, err := playback.Write(piece); err != nil {
			t.Fatalf("fake playback: %v", err)
		}
	}
	if got := playback.buf.String(); got != "piece0piece1" {
		t.Errorf("playback buffer = %q, want %q", got, "piece0piece1")
	}
	if err := source.Close(); err != nil || !source.closed {
		t.Errorf("fake piece source Close = %v, closed = %v", err, source.closed)
	}
}
