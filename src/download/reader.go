package download

import (
	"errors"
	"fmt"
	"io"

	"github.com/neepii/torrent_player/src/pipeline"
)

// Reader adapts a sequential piece source to an io.ReadSeeker over the
// torrent's file bytes.
//
// Pieces are requested on demand and kept in memory, so the remuxer can seek
// back over data that has already been downloaded.
type Reader struct {
	source pipeline.PieceSource
	size   int64
	data   []byte
	eof    bool
	pos    int64
}

var _ io.ReadSeeker = (*Reader)(nil)

// NewReader returns a reader over size bytes served by source.
func NewReader(source pipeline.PieceSource, size int64) (*Reader, error) {
	if source == nil {
		return nil, fmt.Errorf("download: nil piece source")
	}
	if size < 0 {
		return nil, fmt.Errorf("download: negative file length %d", size)
	}
	return &Reader{source: source, size: size}, nil
}

// Read returns the next bytes of the file, downloading pieces as needed.
func (r *Reader) Read(p []byte) (int, error) {
	if r.pos >= r.size {
		return 0, io.EOF
	}
	if len(p) == 0 {
		return 0, nil
	}

	end := r.pos + int64(len(p))
	if end > r.size {
		end = r.size
	}
	if err := r.fill(end); err != nil {
		return 0, err
	}

	n := copy(p, r.data[r.pos:end])
	r.pos += int64(n)
	return n, nil
}

// fill requests pieces until data holds at least end bytes.
func (r *Reader) fill(end int64) error {
	for int64(len(r.data)) < end && !r.eof {
		piece, err := r.source.ReadPiece()
		if errors.Is(err, io.EOF) {
			r.eof = true
			break
		}
		if err != nil {
			return fmt.Errorf("download: read piece: %w", err)
		}
		if len(piece) == 0 {
			return fmt.Errorf("download: piece source returned an empty piece")
		}
		if int64(len(r.data))+int64(len(piece)) > r.size {
			return fmt.Errorf("download: piece data exceeds file length %d", r.size)
		}
		r.data = append(r.data, piece...)
	}

	if int64(len(r.data)) < end {
		return fmt.Errorf("download: piece source ended at %d of %d bytes: %w",
			len(r.data), r.size, io.ErrUnexpectedEOF)
	}
	return nil
}

// Seek moves the read position inside the file. Seeking does not download
// anything; the pieces covering the position are requested by the next Read.
func (r *Reader) Seek(offset int64, whence int) (int64, error) {
	var pos int64
	switch whence {
	case io.SeekStart:
		pos = offset
	case io.SeekCurrent:
		pos = r.pos + offset
	case io.SeekEnd:
		pos = r.size + offset
	default:
		return 0, fmt.Errorf("download: invalid whence %d", whence)
	}
	if pos < 0 {
		return 0, fmt.Errorf("download: negative position %d", pos)
	}
	if pos > r.size {
		return 0, fmt.Errorf("download: position %d is beyond file length %d", pos, r.size)
	}
	r.pos = pos
	return pos, nil
}
