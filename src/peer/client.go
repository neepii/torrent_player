package peer

import (
	"fmt"
	"net"
	"time"
)

// Client connects to a BitTorrent peer and handles the handshake and message exchange.
type Client struct {
	Addr     string
	InfoHash [InfoHashLen]byte
	PeerID   [PeerIDLen]byte
	cfg      *HandshakeConfig
}

// ClientConfig holds configuration for the client.
type ClientConfig struct {
	Handshake *HandshakeConfig
}

// DefaultClientConfig returns default client configuration.
func DefaultClientConfig() *ClientConfig {
	return &ClientConfig{
		Handshake: DefaultHandshakeConfig(),
	}
}

// NewClient creates a new BitTorrent peer client.
func NewClient(addr string, infoHash [InfoHashLen]byte, peerID [PeerIDLen]byte, cfg *ClientConfig) *Client {
	if cfg == nil {
		cfg = DefaultClientConfig()
	}
	if cfg.Handshake == nil {
		cfg.Handshake = DefaultHandshakeConfig()
	}

	return &Client{
		Addr:     addr,
		InfoHash: infoHash,
		PeerID:   peerID,
		cfg:      cfg.Handshake,
	}
}

// Connect establishes a connection to the peer and performs the handshake.
func (c *Client) Connect() (*Conn, error) {
	// Create TCP connection
	conn, err := net.DialTimeout("tcp", c.Addr, c.cfg.Timeout)
	if err != nil {
		return nil, fmt.Errorf("peer: dial failed: %w", err)
	}

	// Wrap in peer connection
	pconn := NewConn(conn, c.cfg)

	// Create our handshake
	ourHandshake := NewHandshake(c.InfoHash, c.PeerID)

	// Perform client handshake
	_, err = pconn.DoClientHandshake(ourHandshake)
	if err != nil {
		conn.Close()
		return nil, err
	}

	return pconn, nil
}

// Dial connects to a peer at the given address.
func Dial(addr string, infoHash [InfoHashLen]byte, peerID [PeerIDLen]byte, cfg *ClientConfig) (*Conn, error) {
	client := NewClient(addr, infoHash, peerID, cfg)
	return client.Connect()
}

// DialTimeout is like Dial but uses the given timeout.
func DialTimeout(addr string, infoHash [InfoHashLen]byte, peerID [PeerIDLen]byte, timeout time.Duration) (*Conn, error) {
	cfg := &ClientConfig{
		Handshake: &HandshakeConfig{Timeout: timeout},
	}
	return Dial(addr, infoHash, peerID, cfg)
}

// Peer represents a connected peer.
type Peer struct {
	Conn     *Conn
	InfoHash [InfoHashLen]byte
	PeerID   [PeerIDLen]byte
	Addr     string
}

// NewPeer creates a new peer from a connection.
func NewPeer(conn *Conn, peerHandshake *Handshake, addr string) *Peer {
	return &Peer{
		Conn:     conn,
		InfoHash: peerHandshake.InfoHash,
		PeerID:   peerHandshake.PeerID,
		Addr:     addr,
	}
}

// MessageHandler handles incoming messages.
type MessageHandler func(msg Message) error

// HandlerChain chains multiple message handlers.
func HandlerChain(handlers ...MessageHandler) MessageHandler {
	return func(msg Message) error {
		for _, h := range handlers {
			if err := h(msg); err != nil {
				return err
			}
		}
		return nil
	}
}

// ErrorHandler creates a handler that stops on error.
func ErrorHandler(h MessageHandler) MessageHandler {
	return func(msg Message) error {
		if msg == nil {
			return nil // Keep-alive
		}
		return h(msg)
	}
}
