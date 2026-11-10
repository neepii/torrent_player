// Package pipeline defines the data types and interfaces that connect the
// torrent parser, tracker, piece source and playback adapter.
package pipeline

import (
	"io"
	"net"

	"github.com/neepii/torrent_player/src/torrent"
)

// PeerData is a peer address reported by the tracker ("PeerData" in the
// architecture diagram).
type PeerData struct {
	IP   net.IP
	Port uint16
}

// PeerConnection is an open TCP connection to a peer ("PeerConnection" in the
// architecture diagram).
type PeerConnection struct {
	Data    PeerData
	TCPConn net.Conn
}

// DataStream carries fMP4 fragments to the player over the unix socket
// ("DataStream" in the architecture diagram).
type DataStream struct {
	UnixConn net.Conn // unix domain socket to the player
	Data     []byte   // fMP4 fragments (H.264)
}

// Parser decodes and validates .torrent metainfo bytes.
type Parser interface {
	Parse(data []byte) (*torrent.TorrentData, error)
}

// Tracker announces the torrent and returns the peer addresses to download
// from.
type Tracker interface {
	Announce(t *torrent.TorrentData) ([]PeerData, error)
}

// PieceSource provides the torrent's pieces in sequential order.
type PieceSource interface {
	// ReadPiece returns the next piece in order. It returns io.EOF after the
	// final piece has been returned.
	ReadPiece() ([]byte, error)
	// Close releases the underlying connections.
	Close() error
}

// PlaybackAdapter converts sequential MP4 bytes into fMP4 fragments and
// streams them to the player over the unix socket.
type PlaybackAdapter interface {
	io.Writer
	Close() error
}
