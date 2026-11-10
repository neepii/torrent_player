package torrent_test

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/neepii/torrent_player/src/cli"
)

func writeTorrentFile(t *testing.T, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sample.torrent")
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestCLIHelpFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"-h"}, &stdout, &stderr); code != 0 {
		t.Fatalf("Run(-h) = %d, want 0", code)
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
	if code := cli.Run(nil, &stdout, &stderr); code != 2 {
		t.Fatalf("Run() = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "Usage: torrent_player") {
		t.Errorf("stderr = %q, want usage text", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
}

func TestCLIFileNotFound(t *testing.T) {
	var stdout, stderr bytes.Buffer
	missing := filepath.Join(t.TempDir(), "missing.torrent")
	if code := cli.Run([]string{missing}, &stdout, &stderr); code != 1 {
		t.Fatalf("Run() = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "torrent:") {
		t.Errorf("stderr = %q, want torrent: prefix", stderr.String())
	}
}

func TestCLIParsesAndPrintsDict(t *testing.T) {
	pieces := makePieces(2)
	info := validInfo("sample.mp4", 100, 200, pieces)
	comment := "my test torrent"
	creation := int64(1700000000)
	data := "d" + bs("announce") + bs(testAnnounce) + bs("comment") + bs(comment) +
		bs("creation date") + bi(creation) + bs("info") + info + "e"
	path := writeTorrentFile(t, data)

	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{path}, &stdout, &stderr); code != 0 {
		t.Fatalf("Run() = %d, want 0; stderr = %q", code, stderr.String())
	}

	sum := sha1.Sum([]byte(info))
	wantHash := hex.EncodeToString(sum[:])
	for _, want := range []string{
		"announce: " + testAnnounce,
		"comment: " + comment,
		"creation date: 1700000000",
		"info hash: " + wantHash,
		"name: sample.mp4",
		"piece length: 100",
		"length: 200",
		"pieces: 2 (40 bytes)",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout = %q, want it to contain %q", stdout.String(), want)
		}
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

func TestCLIInvalidTorrent(t *testing.T) {
	path := writeTorrentFile(t, "this is not bencode")
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{path}, &stdout, &stderr); code != 1 {
		t.Fatalf("Run() = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "torrent:") {
		t.Errorf("stderr = %q, want torrent: prefix", stderr.String())
	}
}
