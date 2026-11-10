// Package torrent parses and validates v1 single-file .torrent metainfo.
package torrent

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"unicode/utf8"

	"github.com/jackpal/bencode-go"
)

// sha1Length is the byte length of a SHA-1 hash, as used by BEP 3 "pieces".
const sha1Length = 20

// errPrefix is prepended to every error returned by this package.
const errPrefix = "torrent:"

// errf returns a new error whose message is prefixed with errPrefix.
func errf(format string, args ...any) error {
	return fmt.Errorf(errPrefix+" "+format, args...)
}

// TorrentData is the parsed v1 single-file metainfo ("TorrentData" in the
// architecture diagram). InfoHash is the SHA-1 of the exact original bencoded
// "info" value.
type TorrentData struct {
	AnnounceURL  string
	Comment      string
	CreationDate int64
	InfoHash     [sha1.Size]byte
	Info         TorrentInfoDict
}

// TorrentInfoDict is the parsed "info" dictionary of a single-file torrent.
type TorrentInfoDict struct {
	Name        string
	PieceLength int64
	Length      int64
	Pieces      []byte // concatenated 20-byte SHA-1 hashes; len(Pieces)%20 == 0
}

// PieceCount returns the number of pieces described by the info dictionary.
func (t *TorrentInfoDict) PieceCount() int {
	return len(t.Pieces) / sha1Length
}

// PieceHash returns the SHA-1 hash for the piece at index. The second return
// value reports whether the index is in range.
func (t *TorrentInfoDict) PieceHash(index int) ([sha1.Size]byte, bool) {
	var h [sha1.Size]byte
	if index < 0 || index >= t.PieceCount() {
		return h, false
	}
	copy(h[:], t.Pieces[index*sha1Length:(index+1)*sha1Length])
	return h, true
}

// rawMetaInfo mirrors the top-level metainfo dictionary. Info is captured as a
// RawMessage so the info hash can be computed from the exact original bytes.
type rawMetaInfo struct {
	Announce     string             `bencode:"announce"`
	Comment      string             `bencode:"comment"`
	CreationDate int64              `bencode:"creation date"`
	Info         bencode.RawMessage `bencode:"info"`
}

// strictOptions enables BEP 3 canonical bencoding validation with the library's
// default resource limits.
func strictOptions() bencode.Options {
	opts := bencode.DefaultOptions()
	opts.Strict = true
	return opts
}

// Parse decodes and validates a v1 single-file .torrent metainfo.
//
// It rejects malformed or truncated bencode, non-canonical encodings,
// multi-file torrents, invalid text fields and piece hashes whose length or
// count does not match the file length and piece length.
func Parse(data []byte) (*TorrentData, error) {
	if len(data) == 0 {
		return nil, errf("empty metainfo")
	}
	if data[0] != 'd' {
		return nil, errf("metainfo must be a bencoded dictionary")
	}

	var raw rawMetaInfo
	if err := bencode.UnmarshalWithOptions(bytes.NewReader(data), &raw, strictOptions()); err != nil {
		return nil, errf("invalid bencode: %w", err)
	}

	if raw.Info == nil {
		return nil, errf("missing info dictionary")
	}
	if raw.Announce == "" {
		return nil, errf("missing or empty announce URL")
	}
	if !utf8.ValidString(raw.Announce) {
		return nil, errf("announce URL is not valid UTF-8")
	}
	if raw.Comment != "" && !utf8.ValidString(raw.Comment) {
		return nil, errf("comment is not valid UTF-8")
	}

	info, err := parseInfoDict(raw.Info)
	if err != nil {
		return nil, err
	}

	return &TorrentData{
		AnnounceURL:  raw.Announce,
		Comment:      raw.Comment,
		CreationDate: raw.CreationDate,
		InfoHash:     sha1.Sum(raw.Info),
		Info:         *info,
	}, nil
}

func parseInfoDict(raw bencode.RawMessage) (*TorrentInfoDict, error) {
	decoded, err := bencode.DecodeWithOptions(bytes.NewReader(raw), strictOptions())
	if err != nil {
		return nil, errf("invalid info dictionary: %w", err)
	}
	m, ok := decoded.(map[string]any)
	if !ok {
		return nil, errf("info is not a dictionary")
	}

	if _, multi := m["files"]; multi {
		return nil, errf("multi-file torrents are not supported")
	}

	length, err := intField(m, "length")
	if err != nil {
		return nil, err
	}
	if length < 0 {
		return nil, errf("info length must be non-negative")
	}

	name, err := stringField(m, "name")
	if err != nil {
		return nil, err
	}
	if name == "" {
		return nil, errf("info name must not be empty")
	}
	if !utf8.ValidString(name) {
		return nil, errf("info name is not valid UTF-8")
	}

	pieceLength, err := intField(m, "piece length")
	if err != nil {
		return nil, err
	}
	if pieceLength <= 0 {
		return nil, errf("piece length must be positive")
	}

	pieces, err := stringField(m, "pieces")
	if err != nil {
		return nil, err
	}
	if len(pieces) == 0 {
		return nil, errf("info pieces must not be empty")
	}
	if len(pieces)%sha1Length != 0 {
		return nil, errf("pieces length %d is not a multiple of %d", len(pieces), sha1Length)
	}

	// BEP 3: the piece hash count must equal ceil(length / piece length).
	expected := length / pieceLength
	if length%pieceLength != 0 {
		expected++
	}
	if int64(len(pieces)/sha1Length) != expected {
		return nil, errf(
			"%d piece hashes for %d bytes at %d bytes/piece (expected %d)",
			len(pieces)/sha1Length, length, pieceLength, expected,
		)
	}

	return &TorrentInfoDict{
		Name:        name,
		PieceLength: pieceLength,
		Length:      length,
		Pieces:      []byte(pieces),
	}, nil
}

func intField(m map[string]any, key string) (int64, error) {
	v, ok := m[key]
	if !ok {
		return 0, errf("missing %q field", key)
	}
	n, ok := v.(int64)
	if !ok {
		return 0, errf("%q must be an integer", key)
	}
	return n, nil
}

func stringField(m map[string]any, key string) (string, error) {
	v, ok := m[key]
	if !ok {
		return "", errf("missing %q field", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", errf("%q must be a string", key)
	}
	return s, nil
}
