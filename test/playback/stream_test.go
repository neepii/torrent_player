package playback_test

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/neepii/torrent_player/src/playback"
)

// gatedSource is an io.ReadSeeker over a file whose reads block once the read
// offset lies inside [from, to) until release is closed. It simulates a source
// that delivers media data later than its headers.
type gatedSource struct {
	rs       io.ReadSeeker
	from, to int64
	release  chan struct{}
}

func (g *gatedSource) Read(p []byte) (int, error) {
	off, err := g.rs.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}
	if off >= g.from && off < g.to {
		<-g.release
	}
	return g.rs.Read(p)
}

func (g *gatedSource) Seek(offset int64, whence int) (int64, error) {
	return g.rs.Seek(offset, whence)
}

// TestAdapterStreamsBeforeSourceIsFullyRead checks that the player receives the
// init segment while the source still withholds its media data, proving the
// stream is not held back until the whole source has been read.
func TestAdapterStreamsBeforeSourceIsFullyRead(t *testing.T) {
	raw, err := os.ReadFile(multiKeyframePath)
	if err != nil {
		t.Fatalf("read %s: %v", multiKeyframePath, err)
	}
	mdatPayload := mdatPayloadOffset(t, raw)

	src := &gatedSource{
		rs:      openSource(t, multiKeyframePath),
		from:    mdatPayload,
		to:      int64(len(raw)),
		release: make(chan struct{}),
	}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(src.release) }) }
	// Keep Serve from blocking forever if the test fails before releasing.
	defer release()

	socketPath := filepath.Join(t.TempDir(), "stream.sock")
	a := &playback.Adapter{SocketPath: socketPath}
	serveErr := make(chan error, 1)
	go func() { serveErr <- a.Serve(src) }()

	waitForSocket(t, socketPath)
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial %s: %v", socketPath, err)
	}
	defer conn.Close()

	data := readUntilInitSegment(t, conn)
	if got := receivedBoxTypes(data); len(got) != 2 || got[0] != "ftyp" || got[1] != "moov" {
		t.Fatalf("boxes received while the source was gated = %v, want [ftyp moov]", got)
	}

	release()
	data = readStream(t, conn, data)

	select {
	case err := <-serveErr:
		if err != nil {
			t.Fatalf("Serve: %v", err)
		}
	case <-time.After(readTimeout):
		t.Fatalf("Serve did not return within %s after the source was released", readTimeout)
	}

	wantBoxes := []string{"ftyp", "moov", "moof", "mdat", "moof", "mdat", "moof", "mdat", "moof", "mdat"}
	if got := topLevelBoxTypes(t, data); !equalStrings(got, wantBoxes) {
		t.Fatalf("boxes over socket = %v, want %v", got, wantBoxes)
	}
	if fragments, samples := decodeFragmentCount(t, data); fragments != 4 || samples != 20 {
		t.Errorf("stream over socket = %d fragments / %d samples, want 4 / 20", fragments, samples)
	}

	var direct bytes.Buffer
	r := playback.Remuxer{}
	if err := r.WriteTo(&direct, openSource(t, multiKeyframePath)); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if !bytes.Equal(data, direct.Bytes()) {
		t.Errorf("socket stream (%d bytes) differs from direct remux (%d bytes)", len(data), direct.Len())
	}
}

// readUntilInitSegment reads conn until the complete ftyp and moov boxes have
// arrived and returns everything read so far.
func readUntilInitSegment(t *testing.T, conn net.Conn) []byte {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	var data []byte
	buf := make([]byte, 512)
	for {
		if types := receivedBoxTypes(data); len(types) >= 2 && types[0] == "ftyp" && types[1] == "moov" {
			return data
		}
		n, err := conn.Read(buf)
		data = append(data, buf[:n]...)
		if err != nil {
			t.Fatalf("read init segment, %d bytes received so far (%v): %v",
				len(data), receivedBoxTypes(data), err)
		}
	}
}

// receivedBoxTypes returns the types of the complete top-level boxes in the
// bytes received so far and stops at the first truncated box.
func receivedBoxTypes(data []byte) []string {
	var types []string
	for off := 0; off+8 <= len(data); {
		size := int(binary.BigEndian.Uint32(data[off : off+4]))
		if size < 8 || off+size > len(data) {
			break
		}
		types = append(types, string(data[off+4:off+8]))
		off += size
	}
	return types
}

// mdatPayloadOffset returns the offset of the first byte of the mdat payload in
// a top-level box stream.
func mdatPayloadOffset(t *testing.T, data []byte) int64 {
	t.Helper()
	for off := 0; off < len(data); {
		if off+8 > len(data) {
			t.Fatalf("truncated box header at offset %d of %d bytes", off, len(data))
		}
		size := int(binary.BigEndian.Uint32(data[off : off+4]))
		if size < 8 {
			t.Fatalf("invalid size %d for top-level box at offset %d", size, off)
		}
		if string(data[off+4:off+8]) == "mdat" {
			return int64(off + 8)
		}
		off += size
	}
	t.Fatal("no mdat box in fixture")
	return 0
}

// TestAdapterStopsWhenPlayerDisconnects checks that a player that goes away
// stops the stream instead of leaving the producer or the consumer blocked.
func TestAdapterStopsWhenPlayerDisconnects(t *testing.T) {
	raw, err := os.ReadFile(multiKeyframePath)
	if err != nil {
		t.Fatalf("read %s: %v", multiKeyframePath, err)
	}
	src := &gatedSource{
		rs:      openSource(t, multiKeyframePath),
		from:    mdatPayloadOffset(t, raw),
		to:      int64(len(raw)),
		release: make(chan struct{}),
	}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(src.release) }) }
	// Keep Serve from blocking forever if the test fails before releasing.
	defer release()

	socketPath := filepath.Join(t.TempDir(), "stream.sock")
	a := &playback.Adapter{SocketPath: socketPath}
	serveErr := make(chan error, 1)
	go func() { serveErr <- a.Serve(src) }()

	waitForSocket(t, socketPath)
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial %s: %v", socketPath, err)
	}
	// Read the init segment, then leave while media data is still pending.
	readUntilInitSegment(t, conn)
	conn.Close()
	release()

	select {
	case err := <-serveErr:
		if err == nil {
			t.Fatal("Serve returned nil after the player disconnected")
		}
	case <-time.After(readTimeout):
		t.Fatal("Serve did not return after the player disconnected")
	}
}
