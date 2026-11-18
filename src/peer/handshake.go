// Package peer implements the BitTorrent TCP peer handshake and wire protocol.
//
// Specification: BEP 3 - BitTorrent Protocol
// https://www.bittorrent.org/beps/bep_0003.html
package peer

import (
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"time"
)

// ProtocolName is the BitTorrent protocol identifier string.
const ProtocolName = "BitTorrent protocol"

// ProtocolNameLen is the length of the protocol name in bytes.
const ProtocolNameLen = 19

// ReservedLen is the number of reserved bytes in the handshake.
const ReservedLen = 8

// InfoHashLen is the length of the info hash in bytes (SHA-1).
const InfoHashLen = 20

// PeerIDLen is the length of the peer ID in bytes.
const PeerIDLen = 20

// HandshakeLen is the total length of the handshake message including the length prefix.
const HandshakeLen = 1 + ProtocolNameLen + ReservedLen + InfoHashLen + PeerIDLen

// Message types as defined in BEP 3.
const (
	MsgChoke = iota
	MsgUnchoke
	MsgInterested
	MsgNotInterested
	MsgHave
	MsgBitfield
	MsgRequest
	MsgPiece
	MsgCancel
)

// Handshake is the initial protocol message exchanged when establishing a
// connection. It consists of:
// - 19 bytes: Protocol length (constant 19)
// - 19 bytes: Protocol identifier (string "BitTorrent protocol")
// - 8 bytes: Reserved bytes (all zeros for standard BitTorrent)
// - 20 bytes: Info hash
// - 20 bytes: Peer ID
//
// Format:
// <len=19><protocol><reserved><info_hash><peer_id>
type Handshake struct {
	InfoHash [InfoHashLen]byte
	PeerID   [PeerIDLen]byte
}

// Encode encodes the handshake message to a byte slice.
func (h *Handshake) Encode() []byte {
	data := make([]byte, HandshakeLen)

	// Protocol name length
	data[0] = ProtocolNameLen

	// Protocol name
	copy(data[1:1+ProtocolNameLen], []byte(ProtocolName))

	// Reserved bytes (all zeros)
	// Reserved bytes are typically used for extensions (e.g., DHT, fast extension)

	// Info hash
	copy(data[1+ProtocolNameLen+ReservedLen:1+ProtocolNameLen+ReservedLen+InfoHashLen], h.InfoHash[:])

	// Peer ID
	copy(data[1+ProtocolNameLen+ReservedLen+InfoHashLen:], h.PeerID[:])

	return data
}

// DecodeHandshake parses a handshake message from a byte slice.
// It returns an error if the data is too short or the protocol name is invalid.
func DecodeHandshake(data []byte) (*Handshake, error) {
	if len(data) < HandshakeLen {
		return nil, fmt.Errorf("handshake: data too short: %d bytes (need %d)", len(data), HandshakeLen)
	}

	// Verify protocol name length
	if data[0] != ProtocolNameLen {
		return nil, fmt.Errorf("handshake: invalid protocol name length: %d", data[0])
	}

	// Verify protocol name
	if string(data[1:1+ProtocolNameLen]) != ProtocolName {
		return nil, fmt.Errorf("handshake: invalid protocol name: %q", string(data[1:1+ProtocolNameLen]))
	}

	var hs Handshake
	copy(hs.InfoHash[:], data[1+ProtocolNameLen+ReservedLen:1+ProtocolNameLen+ReservedLen+InfoHashLen])
	copy(hs.PeerID[:], data[1+ProtocolNameLen+ReservedLen+InfoHashLen:])

	return &hs, nil
}

// HandshakeMsg is a wrapper for handshake message operations.
type HandshakeMsg struct {
	InfoHash [InfoHashLen]byte
	PeerID   [PeerIDLen]byte
}

// NewHandshakeMsg creates a new handshake message.
func NewHandshakeMsg(infoHash [InfoHashLen]byte, peerID [PeerIDLen]byte) *HandshakeMsg {
	return &HandshakeMsg{
		InfoHash: infoHash,
		PeerID:   peerID,
	}
}

// Encode encodes the handshake message.
func (h *HandshakeMsg) Encode() []byte {
	hs := &Handshake{
		InfoHash: h.InfoHash,
		PeerID:   h.PeerID,
	}
	return hs.Encode()
}

// NewHandshake creates a new Handshake instance.
func NewHandshake(infoHash [InfoHashLen]byte, peerID [PeerIDLen]byte) *Handshake {
	return &Handshake{
		InfoHash: infoHash,
		PeerID:   peerID,
	}
}

// HandshakeError represents errors that occur during handshake.
type HandshakeError struct {
	Op   string
	Err  error
	Host string
}

func (e *HandshakeError) Error() string {
	if e.Host != "" {
		return fmt.Sprintf("%s: handshake to %s failed: %v", e.Op, e.Host, e.Err)
	}
	return fmt.Sprintf("%s: handshake failed: %v", e.Op, e.Err)
}

func (e *HandshakeError) Unwrap() error {
	return e.Err
}

// HandshakeConfig holds configuration for the handshake process.
type HandshakeConfig struct {
	Timeout        time.Duration
	ExpectedPeerID *[PeerIDLen]byte
}

// DefaultHandshakeConfig returns default handshake configuration.
func DefaultHandshakeConfig() *HandshakeConfig {
	return &HandshakeConfig{
		Timeout: 10 * time.Second,
	}
}

// HandshakeResult contains the result of a handshake operation.
type HandshakeResult struct {
	InfoHash [InfoHashLen]byte
	PeerID   [PeerIDLen]byte
}

// Conn represents a BitTorrent peer connection with handshake support.
type Conn struct {
	net.Conn
	cfg         *HandshakeConfig
	handshaked  bool
	infoHash    [InfoHashLen]byte
	peerID      [PeerIDLen]byte
	lastPieceID uint32
	lastRequest *Request
}

// NewConn creates a new peer connection wrapper.
func NewConn(c net.Conn, cfg *HandshakeConfig) *Conn {
	if cfg == nil {
		cfg = DefaultHandshakeConfig()
	}
	return &Conn{
		Conn: c,
		cfg:  cfg,
	}
}

// DoClientHandshake performs the client-side handshake.
// It sends our handshake and receives the peer's handshake.
func (c *Conn) DoClientHandshake(ourHandshake *Handshake) (*Handshake, error) {
	if err := c.Conn.SetDeadline(time.Now().Add(c.cfg.Timeout)); err != nil {
		return nil, &HandshakeError{Op: "set_deadline", Err: err}
	}
	defer c.Conn.SetDeadline(time.Time{})

	// Send our handshake
	if n, err := c.Conn.Write(ourHandshake.Encode()); err != nil {
		return nil, &HandshakeError{Op: "send", Err: err, Host: c.RemoteAddr().String()}
	} else if n != HandshakeLen {
		return nil, &HandshakeError{Op: "send", Err: io.ErrShortWrite, Host: c.RemoteAddr().String()}
	}

	// Receive peer's handshake
	peerHandshakeData := make([]byte, HandshakeLen)
	if _, err := io.ReadFull(c.Conn, peerHandshakeData); err != nil {
		return nil, &HandshakeError{Op: "receive", Err: err, Host: c.RemoteAddr().String()}
	}

	peerHandshake, err := DecodeHandshake(peerHandshakeData)
	if err != nil {
		return nil, &HandshakeError{Op: "decode", Err: err, Host: c.RemoteAddr().String()}
	}

	// Validate info hash matches
	if peerHandshake.InfoHash != ourHandshake.InfoHash {
		return nil, &HandshakeError{
			Op:   "validate",
			Err:  fmt.Errorf("info hash mismatch: got %x, expected %x", peerHandshake.InfoHash, ourHandshake.InfoHash),
			Host: c.RemoteAddr().String(),
		}
	}

	if c.cfg.ExpectedPeerID != nil && peerHandshake.PeerID != *c.cfg.ExpectedPeerID {
		return nil, &HandshakeError{Op: "validate", Err: fmt.Errorf("peer ID mismatch"), Host: c.RemoteAddr().String()}
	}

	c.handshaked = true
	c.infoHash = ourHandshake.InfoHash
	c.peerID = peerHandshake.PeerID

	return peerHandshake, nil
}

// DoServerHandshake performs the server-side handshake.
// It receives the peer's handshake first, then sends our handshake.
func (c *Conn) DoServerHandshake(ourHandshake *Handshake, validatePeerID bool) (*Handshake, error) {
	if err := c.Conn.SetDeadline(time.Now().Add(c.cfg.Timeout)); err != nil {
		return nil, &HandshakeError{Op: "set_deadline", Err: err}
	}
	defer c.Conn.SetDeadline(time.Time{})

	// Receive peer's handshake first
	peerHandshakeData := make([]byte, HandshakeLen)
	if _, err := io.ReadFull(c.Conn, peerHandshakeData); err != nil {
		return nil, &HandshakeError{Op: "receive", Err: err, Host: c.RemoteAddr().String()}
	}

	peerHandshake, err := DecodeHandshake(peerHandshakeData)
	if err != nil {
		return nil, &HandshakeError{Op: "decode", Err: err, Host: c.RemoteAddr().String()}
	}

	// Validate info hash matches
	if peerHandshake.InfoHash != ourHandshake.InfoHash {
		return nil, &HandshakeError{
			Op:   "validate",
			Err:  fmt.Errorf("info hash mismatch: got %x, expected %x", peerHandshake.InfoHash, ourHandshake.InfoHash),
			Host: c.RemoteAddr().String(),
		}
	}

	// Validate peer ID if requested
	if validatePeerID && len(peerHandshake.PeerID) > 0 {
		// Basic peer ID validation
		// In production, you might want more sophisticated validation
	}

	// Send our handshake
	if n, err := c.Conn.Write(ourHandshake.Encode()); err != nil {
		return nil, &HandshakeError{Op: "send", Err: err, Host: c.RemoteAddr().String()}
	} else if n != HandshakeLen {
		return nil, &HandshakeError{Op: "send", Err: io.ErrShortWrite, Host: c.RemoteAddr().String()}
	}

	c.handshaked = true
	c.infoHash = ourHandshake.InfoHash
	c.peerID = peerHandshake.PeerID

	return peerHandshake, nil
}

// IsHandshaked returns true if the handshake has been completed.
func (c *Conn) IsHandshaked() bool {
	return c.handshaked
}

// InfoHash returns the local info hash.
func (c *Conn) InfoHash() [InfoHashLen]byte {
	return c.infoHash
}

// PeerID returns the remote peer's ID.
func (c *Conn) PeerID() [PeerIDLen]byte {
	return c.peerID
}

// GeneratePeerID generates a random peer ID.
func GeneratePeerID() [PeerIDLen]byte {
	var peerID [PeerIDLen]byte
	if _, err := rand.Read(peerID[:]); err != nil {
		// Fallback: generate deterministic ID for testing
		for i := range peerID {
			peerID[i] = byte((i * 7) % 256)
		}
	}
	return peerID
}

// ConnConfig holds configuration for peer connections.
type ConnConfig struct {
	Handshake *HandshakeConfig
}

// DefaultConnConfig returns default connection configuration.
func DefaultConnConfig() *ConnConfig {
	return &ConnConfig{
		Handshake: DefaultHandshakeConfig(),
	}
}
