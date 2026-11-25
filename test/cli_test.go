package torrent_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/neepii/torrent_player/src/cli"
)

func TestCLIHelpFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cfg, code := cli.Run([]string{"-h"}, &stdout, &stderr)
	if code != cli.ExitSuccess || cfg != nil {
		t.Fatalf("Run(-h) = (%v, %d), want (nil, %d)", cfg, code, cli.ExitSuccess)
	}
	if !strings.Contains(stdout.String(), "Usage: torrent_player") {
		t.Errorf("stdout = %q, want usage text", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

func TestCLIMissingArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cfg, code := cli.Run(nil, &stdout, &stderr)
	if code != cli.ExitCliFail || cfg != nil {
		t.Fatalf("Run() = (%v, %d), want (nil, %d)", cfg, code, cli.ExitCliFail)
	}
	if !strings.Contains(stderr.String(), "Usage: torrent_player") {
		t.Errorf("stderr = %q, want usage text", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
}

func TestCLIInputFileAndDefaultSocket(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cfg, code := cli.Run([]string{"sample.torrent"}, &stdout, &stderr)
	if code != cli.ExitSuccess || cfg == nil {
		t.Fatalf("Run() = (%v, %d), want a config and exit code %d", cfg, code, cli.ExitSuccess)
	}
	if cfg.TorrentFile != "sample.torrent" {
		t.Errorf("TorrentFile = %q, want %q", cfg.TorrentFile, "sample.torrent")
	}
	if cfg.SocketFile != cli.DefaultSocketPath {
		t.Errorf("SocketFile = %q, want default %q", cfg.SocketFile, cli.DefaultSocketPath)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Errorf("stdout = %q, stderr = %q, want both empty", stdout.String(), stderr.String())
	}
}

func TestCLIExplicitSocket(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cfg, code := cli.Run([]string{"-socket", "/tmp/custom.sock", "sample.torrent"}, &stdout, &stderr)
	if code != cli.ExitSuccess || cfg == nil {
		t.Fatalf("Run() = (%v, %d), want a config and exit code %d", cfg, code, cli.ExitSuccess)
	}
	if cfg.SocketFile != "/tmp/custom.sock" {
		t.Errorf("SocketFile = %q, want %q", cfg.SocketFile, "/tmp/custom.sock")
	}
}

func TestCLIEmptySocket(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cfg, code := cli.Run([]string{"-socket", "", "sample.torrent"}, &stdout, &stderr)
	if code != cli.ExitCliFail || cfg != nil {
		t.Fatalf("Run() = (%v, %d), want (nil, %d)", cfg, code, cli.ExitCliFail)
	}
	if !strings.Contains(stderr.String(), "-socket") {
		t.Errorf("stderr = %q, want socket error", stderr.String())
	}
}
