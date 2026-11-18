package peer_test

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"

	"github.com/neepii/torrent_player/src/peer"
)

type byteReader struct{ r io.Reader }

func (r byteReader) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return r.r.Read(p)
}

func TestDecoderFragmentedInput(t *testing.T) {
	original := peer.NewHave(42)
	got, err := peer.NewDecoder(byteReader{bytes.NewReader(original.Encode())}).Decode()
	if err != nil {
		t.Fatal(err)
	}
	have, ok := got.(*peer.Have)
	if !ok || have.PieceIndex != 42 {
		t.Fatalf("unexpected message: %#v", got)
	}
}

func TestClientRejectsUnexpectedPeerID(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	var hash, local, remote, expected [20]byte
	remote[0] = 1
	expected[0] = 2
	go func() {
		buf := make([]byte, peer.HandshakeLen)
		if _, err := io.ReadFull(b, buf); err == nil {
			b.Write(peer.NewHandshake(hash, remote).Encode())
		}
	}()
	conn := peer.NewConn(a, &peer.HandshakeConfig{Timeout: time.Second, ExpectedPeerID: &expected})
	if _, err := conn.DoClientHandshake(peer.NewHandshake(hash, local)); err == nil {
		t.Fatal("expected peer ID mismatch")
	}
}
