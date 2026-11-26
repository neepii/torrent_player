package download_test

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/neepii/torrent_player/src/download"
	"github.com/neepii/torrent_player/src/playback"
)

// sliceSource serves prepared pieces in order.
type sliceSource struct {
	pieces [][]byte
}

func (s *sliceSource) ReadPiece() ([]byte, error) {
	if len(s.pieces) == 0 {
		return nil, io.EOF
	}
	p := s.pieces[0]
	s.pieces = s.pieces[1:]
	return p, nil
}

func (s *sliceSource) Close() error { return nil }

func newSliceReader(t *testing.T, size int64, pieces ...string) *download.Reader {
	t.Helper()
	src := &sliceSource{}
	for _, p := range pieces {
		src.pieces = append(src.pieces, []byte(p))
	}
	r, err := download.NewReader(src, size)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	return r
}

func TestReaderReadsAcrossPieces(t *testing.T) {
	r := newSliceReader(t, 9, "abc", "def", "ghi")

	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != "abcdefghi" {
		t.Errorf("ReadAll = %q, want %q", got, "abcdefghi")
	}

	if _, err := r.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Errorf("Read after end = %v, want io.EOF", err)
	}
}

func TestReaderReadsShortBuffer(t *testing.T) {
	r := newSliceReader(t, 6, "abc", "def")

	buf := make([]byte, 4)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if n != 4 || string(buf[:n]) != "abcd" {
		t.Errorf("Read = %q (%d), want %q", buf[:n], n, "abcd")
	}
}

func TestReaderSeekBackwardsOverDownloadedPieces(t *testing.T) {
	r := newSliceReader(t, 9, "abc", "def", "ghi")

	if _, err := r.Seek(7, io.SeekStart); err != nil {
		t.Fatalf("Seek(7): %v", err)
	}
	buf := make([]byte, 2)
	if n, err := r.Read(buf); err != nil || n != 2 || string(buf) != "hi" {
		t.Fatalf("Read at 7 = %q (%d), %v; want %q", buf, n, err, "hi")
	}

	if _, err := r.Seek(1, io.SeekStart); err != nil {
		t.Fatalf("Seek(1): %v", err)
	}
	if n, err := r.Read(buf); err != nil || n != 2 || string(buf) != "bc" {
		t.Fatalf("Read at 1 = %q (%d), %v; want %q", buf, n, err, "bc")
	}
}

func TestReaderSeekDoesNotDownload(t *testing.T) {
	src := &sliceSource{pieces: [][]byte{[]byte("abc"), []byte("def")}}
	r, err := download.NewReader(src, 6)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	if _, err := r.Seek(4, io.SeekStart); err != nil {
		t.Fatalf("Seek: %v", err)
	}
	if len(src.pieces) != 2 {
		t.Fatalf("Seek downloaded pieces, %d left", len(src.pieces))
	}

	buf := make([]byte, 1)
	if n, err := r.Read(buf); err != nil || n != 1 || string(buf) != "e" {
		t.Fatalf("Read = %q (%d), %v; want %q", buf, n, err, "e")
	}
}

func TestReaderSeekFromCurrentAndEnd(t *testing.T) {
	r := newSliceReader(t, 9, "abc", "def", "ghi")

	if _, err := r.Seek(2, io.SeekStart); err != nil {
		t.Fatalf("Seek(2, Start): %v", err)
	}
	if pos, err := r.Seek(3, io.SeekCurrent); err != nil || pos != 5 {
		t.Fatalf("Seek(3, Current) = %d, %v; want 5", pos, err)
	}
	if pos, err := r.Seek(-2, io.SeekEnd); err != nil || pos != 7 {
		t.Fatalf("Seek(-2, End) = %d, %v; want 7", pos, err)
	}
}

func TestReaderRejectsInvalidSeek(t *testing.T) {
	r := newSliceReader(t, 6, "abc", "def")

	if _, err := r.Seek(-1, io.SeekStart); err == nil {
		t.Error("Seek(-1, Start) succeeded")
	}
	if _, err := r.Seek(7, io.SeekStart); err == nil {
		t.Error("Seek beyond the end succeeded")
	}
	if _, err := r.Seek(0, 42); err == nil {
		t.Error("Seek with invalid whence succeeded")
	}
}

func TestReaderReportsTruncatedSource(t *testing.T) {
	r := newSliceReader(t, 9, "abc", "def")

	buf := make([]byte, 9)
	if _, err := io.ReadFull(r, buf); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("ReadFull = %v, want io.ErrUnexpectedEOF", err)
	}
}

// TestReaderFeedsRemuxer checks that lazy piece downloads through the reader
// produce the same fragmented MP4 as remuxing the complete file. It exercises
// the seek pattern the remuxer uses: read the moov, skip the mdat, then seek
// back and read sample data.
func TestReaderFeedsRemuxer(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/swarm/sample.mp4")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	const pieceLength = 1024
	src := &sliceSource{}
	for off := 0; off < len(raw); off += pieceLength {
		end := off + pieceLength
		if end > len(raw) {
			end = len(raw)
		}
		src.pieces = append(src.pieces, raw[off:end])
	}
	r, err := download.NewReader(src, int64(len(raw)))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}

	var streamed bytes.Buffer
	remuxer := playback.Remuxer{}
	if err := remuxer.WriteTo(&streamed, r); err != nil {
		t.Fatalf("WriteTo over piece reader: %v", err)
	}

	var direct bytes.Buffer
	file, err := os.Open("../../testdata/swarm/sample.mp4")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer file.Close()
	if err := remuxer.WriteTo(&direct, file); err != nil {
		t.Fatalf("WriteTo over file: %v", err)
	}

	if !bytes.Equal(streamed.Bytes(), direct.Bytes()) {
		t.Errorf("streamed %d bytes differ from direct remux of %d bytes",
			streamed.Len(), direct.Len())
	}
}
