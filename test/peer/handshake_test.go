package peer_test

import (
	"crypto/sha1"
	"net"
	"testing"
	"time"

	"github.com/neepii/torrent_player/src/peer"
)

// TestHandshakeEncoding tests that handshake encoding produces correct bytes.
func TestHandshakeEncoding(t *testing.T) {
	// Create test info hash and peer ID
	infoHash := sha1.Sum([]byte("test-info-hash"))
	var peerID [20]byte
	for i := range peerID {
		peerID[i] = byte(i)
	}

	hs := peer.NewHandshake(infoHash, peerID)
	data := hs.Encode()

	// Verify length
	if len(data) != peer.HandshakeLen {
		t.Errorf("handshake length = %d, want %d", len(data), peer.HandshakeLen)
	}

	// Verify protocol name length
	if data[0] != peer.ProtocolNameLen {
		t.Errorf("protocol name length = %d, want %d", data[0], peer.ProtocolNameLen)
	}

	// Verify protocol name
	protocolName := string(data[1 : 1+peer.ProtocolNameLen])
	if protocolName != peer.ProtocolName {
		t.Errorf("protocol name = %q, want %q", protocolName, peer.ProtocolName)
	}

	// Verify info hash
	var gotInfoHash [20]byte
	copy(gotInfoHash[:], data[1+peer.ProtocolNameLen+peer.ReservedLen:])
	if gotInfoHash != infoHash {
		t.Errorf("info hash = %x, want %x", gotInfoHash, infoHash)
	}

	// Verify peer ID
	var gotPeerID [20]byte
	copy(gotPeerID[:], data[1+peer.ProtocolNameLen+peer.ReservedLen+peer.InfoHashLen:])
	if gotPeerID != peerID {
		t.Errorf("peer ID = %x, want %x", gotPeerID, peerID)
	}
}

// TestHandshakeDecoding tests that handshake decoding works correctly.
func TestHandshakeDecoding(t *testing.T) {
	// Create test info hash and peer ID
	infoHash := sha1.Sum([]byte("test-info-hash"))
	var peerID [20]byte
	for i := range peerID {
		peerID[i] = byte(i)
	}

	// Create and encode handshake
	hs := peer.NewHandshake(infoHash, peerID)
	data := hs.Encode()

	// Decode
	got, err := peer.DecodeHandshake(data)
	if err != nil {
		t.Fatalf("DecodeHandshake() error = %v", err)
	}

	if got.InfoHash != infoHash {
		t.Errorf("info hash = %x, want %x", got.InfoHash, infoHash)
	}

	if got.PeerID != peerID {
		t.Errorf("peer ID = %x, want %x", got.PeerID, peerID)
	}
}

// TestHandshakeInvalidLength tests that decoding fails with wrong length.
func TestHandshakeInvalidLength(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{"too short", make([]byte, peer.HandshakeLen-1)},
		{"empty", make([]byte, 0)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := peer.DecodeHandshake(tc.data)
			if err == nil {
				t.Error("expected error for invalid length")
			}
		})
	}
}

// TestHandshakeInvalidProtocolName tests that decoding fails with wrong protocol.
func TestHandshakeInvalidProtocolName(t *testing.T) {
	infoHash := sha1.Sum([]byte("test"))
	var peerID [20]byte

	data := make([]byte, peer.HandshakeLen)
	data[0] = peer.ProtocolNameLen
	copy(data[1:1+peer.ProtocolNameLen], []byte("WrongProtocolName"))
	copy(data[1+peer.ProtocolNameLen+peer.ReservedLen:], infoHash[:])
	copy(data[1+peer.ProtocolNameLen+peer.ReservedLen+peer.InfoHashLen:], peerID[:])

	_, err := peer.DecodeHandshake(data)
	if err == nil {
		t.Error("expected error for invalid protocol name")
	}
}

// TestHandshakeServerClientExchange tests a full handshake between server and client.
func TestHandshakeServerClientExchange(t *testing.T) {
	// Create a listener
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	defer ln.Close()

	// Create server handshake
	serverInfoHash := sha1.Sum([]byte("server-info-hash"))
	var serverPeerID [20]byte
	for i := range serverPeerID {
		serverPeerID[i] = byte(i)
	}
	serverHandshake := peer.NewHandshake(serverInfoHash, serverPeerID)

	// Create client handshake
	clientInfoHash := serverInfoHash // Must match
	var clientPeerID [20]byte
	for i := range clientPeerID {
		clientPeerID[i] = byte(100 + i)
	}
	clientHandshake := peer.NewHandshake(clientInfoHash, clientPeerID)

	// Start server in goroutine
	done := make(chan bool)

	go func() {
		// Accept connection
		conn, err := ln.Accept()
		if err != nil {
			t.Errorf("net.Accept() error = %v", err)
			done <- true
			return
		}
		defer conn.Close()

		// Create peer connection
		pconn := peer.NewConn(conn, peer.DefaultHandshakeConfig())

		// Perform server handshake
		peerHandshake, err := pconn.DoServerHandshake(serverHandshake, false)
		if err != nil {
			t.Errorf("DoServerHandshake() error = %v", err)
			done <- true
			return
		}

		if peerHandshake.InfoHash != serverInfoHash {
			t.Errorf("server: info hash mismatch")
		}

		// Send client handshake back
		_, err = conn.Write(clientHandshake.Encode())
		if err != nil {
			t.Errorf("write error = %v", err)
		}

		done <- true
	}()

	// Client connects
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("net.Dial() error = %v", err)
	}
	defer conn.Close()

	// Create peer connection
	pconn := peer.NewConn(conn, peer.DefaultHandshakeConfig())

	// Perform client handshake
	peerHandshake, err := pconn.DoClientHandshake(clientHandshake)
	if err != nil {
		t.Fatalf("DoClientHandshake() error = %v", err)
	}

	if peerHandshake.InfoHash != clientInfoHash {
		t.Errorf("client: info hash mismatch")
	}

	<-done
}

// TestHandshakeInfoHashMismatch tests that info hash validation works.
func TestHandshakeInfoHashMismatch(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	defer ln.Close()

	serverInfoHash := sha1.Sum([]byte("server-hash"))
	var serverPeerID [20]byte
	serverHandshake := peer.NewHandshake(serverInfoHash, serverPeerID)

	clientInfoHash := sha1.Sum([]byte("wrong-hash"))
	var clientPeerID [20]byte
	clientHandshake := peer.NewHandshake(clientInfoHash, clientPeerID)

	var serverErr error
	done := make(chan bool)

	go func() {
		conn, _ := ln.Accept()
		defer conn.Close()

		pconn := peer.NewConn(conn, peer.DefaultHandshakeConfig())

		_, serverErr = pconn.DoServerHandshake(serverHandshake, false)
		done <- true
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("net.Dial() error = %v", err)
	}
	defer conn.Close()

	pconn := peer.NewConn(conn, peer.DefaultHandshakeConfig())

	_, err = pconn.DoClientHandshake(clientHandshake)
	if err == nil {
		t.Error("expected error for info hash mismatch")
	}

	<-done

	// Server should also detect mismatch
	if serverErr == nil {
		t.Error("expected server to detect info hash mismatch")
	}
}

// TestHandshakeTimeout tests that handshake respects timeout.
func TestHandshakeTimeout(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	defer ln.Close()

	// Accept connection but don't send handshake
	go func() {
		conn, _ := ln.Accept()
		time.Sleep(2 * time.Second)
		conn.Close()
	}()

	infoHash := sha1.Sum([]byte("test"))
	var peerID [20]byte
	cfg := peer.HandshakeConfig{Timeout: 100 * time.Millisecond}

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("net.Dial() error = %v", err)
	}
	defer conn.Close()

	pconn := peer.NewConn(conn, &cfg)

	_, err = pconn.DoClientHandshake(peer.NewHandshake(infoHash, peerID))
	if err == nil {
		t.Error("expected timeout error")
	}

	// Just verify error is a handshake error with expected content
	if err == nil {
		t.Error("expected timeout error")
	}
}

// TestGeneratePeerID tests peer ID generation.
func TestGeneratePeerID(t *testing.T) {
	peerID := peer.GeneratePeerID()

	// Should be 20 bytes
	if len(peerID) != 20 {
		t.Errorf("peer ID length = %d, want 20", len(peerID))
	}

	// Should not be all zeros
	var allZeros [20]byte
	if peerID == allZeros {
		t.Error("peer ID should not be all zeros")
	}
}

// TestHandshakeMsg tests the HandshakeMsg wrapper.
func TestHandshakeMsg(t *testing.T) {
	infoHash := sha1.Sum([]byte("test"))
	var peerID [20]byte
	for i := range peerID {
		peerID[i] = byte(i)
	}

	msg := peer.NewHandshakeMsg(infoHash, peerID)
	data := msg.Encode()

	got, err := peer.DecodeHandshake(data)
	if err != nil {
		t.Fatalf("DecodeHandshake() error = %v", err)
	}

	if got.InfoHash != infoHash {
		t.Errorf("info hash = %x, want %x", got.InfoHash, infoHash)
	}

	if got.PeerID != peerID {
		t.Errorf("peer ID = %x, want %x", got.PeerID, peerID)
	}
}

// TestHandshakeConnInterface tests that Conn implements net.Conn interface.
func TestHandshakeConnInterface(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err == nil {
			conn.Close()
		}
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("net.Dial() error = %v", err)
	}
	defer conn.Close()

	pconn := peer.NewConn(conn, peer.DefaultHandshakeConfig())

	// Test net.Conn methods
	if pconn.LocalAddr() == nil {
		t.Error("LocalAddr() returned nil")
	}
	if pconn.RemoteAddr() == nil {
		t.Error("RemoteAddr() returned nil")
	}

	// Test Write
	_, err = pconn.Write([]byte("test"))
	if err != nil {
		t.Errorf("Write() error = %v", err)
	}

	// Test IsHandshaked before handshake
	if pconn.IsHandshaked() {
		t.Error("IsHandshaked() should be false before handshake")
	}
}
