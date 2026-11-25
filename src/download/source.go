// Package download retrieves verified torrent pieces in order.
package download

import (
	"crypto/sha1"
	"fmt"
	"io"
	"time"

	"github.com/neepii/torrent_player/src/peer"
	"github.com/neepii/torrent_player/src/pipeline"
	"github.com/neepii/torrent_player/src/torrent"
)

const (
	blockSize    = 16 * 1024
	maxPieceSize = 64 * 1024 * 1024
)

// Options controls bounded retries and the deadline for each piece attempt.
type Options struct {
	Timeout  time.Duration
	Attempts int
}

// Source fetches and verifies pieces sequentially. It is not safe for concurrent use.
type Source struct {
	info              torrent.TorrentInfoDict
	hash              [20]byte
	peerID            [20]byte
	peers             []pipeline.PeerData
	options           Options
	index             int
	conn              *peer.Conn
	available         []byte
	availabilityKnown bool
	choked            bool
	closed            bool
}

var _ pipeline.PieceSource = (*Source)(nil)

// New creates a source. Attempts is the maximum number of attempts per piece;
// an attempt uses one peer connection. Zero option values select defaults.
func New(meta *torrent.TorrentData, peers []pipeline.PeerData, peerID [20]byte, opts Options) (*Source, error) {
	if meta == nil || meta.Info.Length <= 0 || meta.Info.PieceLength <= 0 || meta.Info.PieceLength > maxPieceSize || len(meta.Info.Pieces)%sha1.Size != 0 {
		return nil, fmt.Errorf("download: invalid torrent metadata")
	}
	count := meta.Info.Length / meta.Info.PieceLength
	if meta.Info.Length%meta.Info.PieceLength != 0 {
		count++
	}
	if count != int64(len(meta.Info.Pieces)/sha1.Size) || count > 1<<32 {
		return nil, fmt.Errorf("download: invalid piece count")
	}
	if len(peers) == 0 {
		return nil, fmt.Errorf("download: no peers")
	}
	if opts.Timeout == 0 {
		opts.Timeout = 10 * time.Second
	}
	if opts.Attempts == 0 {
		opts.Attempts = 3
	}
	if opts.Timeout < 0 || opts.Attempts < 1 {
		return nil, fmt.Errorf("download: invalid options")
	}
	info := meta.Info
	info.Pieces = append([]byte(nil), info.Pieces...)
	return &Source{info: info, hash: meta.InfoHash, peerID: peerID, peers: append([]pipeline.PeerData(nil), peers...), options: opts}, nil
}

// ReadPiece returns only SHA-1 verified data. On failure the index does not advance.
func (s *Source) ReadPiece() ([]byte, error) {
	if s.closed {
		return nil, fmt.Errorf("download: source closed")
	}
	if s.index == s.info.PieceCount() {
		return nil, io.EOF
	}
	size := s.info.PieceLength
	remaining := s.info.Length - int64(s.index)*s.info.PieceLength
	if remaining < size {
		size = remaining
	}
	var last error
	for attempt := 0; attempt < s.options.Attempts; attempt++ {
		if s.conn == nil {
			p := s.peers[attempt%len(s.peers)]
			cfg := &peer.ClientConfig{Handshake: &peer.HandshakeConfig{Timeout: s.options.Timeout, ExpectedPeerID: p.ID}}
			conn, err := peer.Dial(p.AddrPort.String(), s.hash, s.peerID, cfg)
			if err != nil {
				last = err
				continue
			}
			s.conn = conn
			s.choked = true
			s.available = nil
			s.availabilityKnown = false
			if err = conn.SetDeadline(time.Now().Add(s.options.Timeout)); err == nil {
				err = peer.NewEncoder(conn).Encode(&peer.Interested{})
			}
			if err != nil {
				last = err
				s.disconnect()
				continue
			}
		}
		data, err := s.readPiece(int(size))
		if err == nil {
			expected, _ := s.info.PieceHash(s.index)
			if sha1.Sum(data) == expected {
				s.index++
				return data, nil
			}
			err = fmt.Errorf("piece hash mismatch")
		}
		last = err
		s.disconnect()
	}
	return nil, fmt.Errorf("download: piece %d failed after %d attempts: %w", s.index, s.options.Attempts, last)
}

func (s *Source) readPiece(size int) ([]byte, error) {
	if err := s.conn.SetDeadline(time.Now().Add(s.options.Timeout)); err != nil {
		return nil, err
	}
	decoder := peer.NewDecoder(s.conn)
	encoder := peer.NewEncoder(s.conn)
	data := make([]byte, size)
	offset := 0
	requested := false
	requestedSize := 0
	for offset < size {
		if s.availabilityKnown && (s.index/8 >= len(s.available) || s.available[s.index/8]&(0x80>>uint(s.index%8)) == 0) {
			return nil, fmt.Errorf("peer lacks piece %d", s.index)
		}
		if !s.choked && !requested {
			requestedSize = size - offset
			if requestedSize > blockSize {
				requestedSize = blockSize
			}
			if err := encoder.Encode(peer.NewRequest(uint32(s.index), uint32(offset), uint32(requestedSize))); err != nil {
				return nil, err
			}
			requested = true
		}
		msg, err := decoder.Decode()
		if err != nil {
			return nil, err
		}
		switch m := msg.(type) {
		case nil:
		case *peer.Choke:
			if requested {
				return nil, fmt.Errorf("peer choked during request")
			}
			s.choked = true
		case *peer.Unchoke:
			s.choked = false
		case *peer.Bitfield:
			if len(m.Bitmap) != (s.info.PieceCount()+7)/8 {
				return nil, fmt.Errorf("invalid peer bitfield")
			}
			s.available = append([]byte(nil), m.Bitmap...)
			s.availabilityKnown = true
		case *peer.Have:
			if uint64(m.PieceIndex) >= uint64(s.info.PieceCount()) {
				return nil, fmt.Errorf("invalid have index")
			}
			if s.available == nil {
				s.available = make([]byte, (s.info.PieceCount()+7)/8)
			}
			s.available[m.PieceIndex/8] |= 0x80 >> uint(m.PieceIndex%8)
		case *peer.Piece:
			if !requested || m.PieceIndex != uint32(s.index) || m.Begin != uint32(offset) || len(m.Data) != requestedSize {
				return nil, fmt.Errorf("unexpected piece block")
			}
			copy(data[offset:], m.Data)
			offset += len(m.Data)
			requested = false
		}
	}
	return data, nil
}
func (s *Source) disconnect() {
	if s.conn != nil {
		_ = s.conn.Close()
		s.conn = nil
	}
}

// Close closes the current peer connection.
func (s *Source) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	if s.conn != nil {
		err := s.conn.Close()
		s.conn = nil
		return err
	}
	return nil
}
