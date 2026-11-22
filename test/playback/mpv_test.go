package playback_test

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/neepii/torrent_player/src/playback"
)

// TestPlayerPlaysStreamWithMPV is the end-to-end check: mpv connects to the
// unix socket, reads the fragmented MP4 our adapter writes, and reports the
// end of the stream on its stdout.
//
// mpv runs without a window, without audio output and without the user's
// configuration, so its only output is the playback report.
func TestPlayerPlaysStreamWithMPV(t *testing.T) {
	if _, err := exec.LookPath("mpv"); err != nil {
		t.Skip("mpv is not installed")
	}
	if testing.Short() {
		t.Skip("skipping mpv integration test in short mode")
	}

	socketPath := filepath.Join(t.TempDir(), "stream.sock")
	a := &playback.Adapter{SocketPath: socketPath}
	listener, err := a.Listen()
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	streamErr := make(chan error, 1)
	go func() { streamErr <- a.Stream(listener, openSource(t, samplePath)) }()

	var stdout, stderr bytes.Buffer
	p := &playback.Player{
		SocketPath: socketPath,
		Args:       []string{"--no-config", "--vo=null", "--ao=null"},
		Stdout:     &stdout,
		Stderr:     &stderr,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd, err := p.Start(ctx)
	if err != nil {
		listener.Close()
		t.Fatalf("start mpv: %v", err)
	}
	waitErr := cmd.Wait()

	if err := <-streamErr; err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			t.Fatalf("mpv exited with %v\nstdout:\n%s\nstderr:\n%s", waitErr, stdout.String(), stderr.String())
		}
		t.Fatalf("wait for mpv: %v", waitErr)
	}

	// mpv reports the end of the stream on stdout, which means it consumed the
	// whole fragmented MP4 and did not fail to demux it.
	if !strings.Contains(stdout.String(), "End of file") {
		t.Errorf("mpv stdout does not report the end of the stream:\n%s", stdout.String())
	}
	if strings.Contains(stdout.String(), "Failed to recognize file format") {
		t.Errorf("mpv could not demux the stream:\n%s", stdout.String())
	}
}
