package playback_test

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/neepii/torrent_player/src/playback"
)

// TestAdapterServesFragmentedMP4OverUnixSocket is the socket test that runs
// without a player: it creates the unix domain socket, connects a plain client
// and checks the received bytes form a fragmented MP4.
func TestAdapterServesFragmentedMP4OverUnixSocket(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "stream.sock")

	a := &playback.Adapter{SocketPath: socketPath}
	serveErr := make(chan error, 1)
	go func() { serveErr <- a.Serve(openSource(t, samplePath)) }()

	waitForSocket(t, socketPath)

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial %s: %v", socketPath, err)
	}
	data := readAll(t, conn)
	conn.Close()

	if err := <-serveErr; err != nil {
		t.Fatalf("Serve: %v", err)
	}

	wantBoxes := []string{"ftyp", "moov", "moof", "mdat"}
	if got := topLevelBoxTypes(t, data); !equalStrings(got, wantBoxes) {
		t.Fatalf("boxes over socket = %v, want %v", got, wantBoxes)
	}
	if fragments, samples := decodeFragmentCount(t, data); fragments != 1 || samples != 20 {
		t.Errorf("stream over socket = %d fragments / %d samples, want 1 / 20", fragments, samples)
	}

	// The bytes the player receives must equal a direct remux.
	var direct bytes.Buffer
	r := playback.Remuxer{}
	if err := r.WriteTo(&direct, openSource(t, samplePath)); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if !bytes.Equal(data, direct.Bytes()) {
		t.Errorf("socket stream (%d bytes) differs from direct remux (%d bytes)", len(data), direct.Len())
	}

	// The socket file must be cleaned up after Serve returns.
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Errorf("socket file still present after Serve, stat err = %v", err)
	}
}

// TestAdapterListenReportsBadPath checks that an unusable socket path is
// reported instead of failing silently.
func TestAdapterListenReportsBadPath(t *testing.T) {
	a := &playback.Adapter{SocketPath: filepath.Join(t.TempDir(), "missing", "stream.sock")}
	if _, err := a.Listen(); err == nil {
		t.Fatal("Listen() accepted a socket path in a missing directory")
	}
}

// waitForSocket waits until path exists as a socket.
func waitForSocket(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if fi, err := os.Stat(path); err == nil && fi.Mode()&os.ModeSocket != 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("socket %s did not appear within 5s", path)
}
