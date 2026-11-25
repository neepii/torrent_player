// Package cli implements the torrent_player command-line interface.
package cli

import (
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/neepii/torrent_player/src/torrent"
)

const (
	ExitSuccess = iota
	ExitInternalFail
	ExitCliFail
)

// DefaultSocketPath is the unix domain socket used when -socket is not given.
const DefaultSocketPath = "/tmp/torrent_stream.sock"

const usageMsg = `Usage: torrent_player [options] <file.torrent>

Stream a single-file v1 .torrent to an FFmpeg-based player over a unix socket.

Options:
  -h                show this help and exit
  -socket <path>    unix domain socket for the player (default /tmp/torrent_stream.sock)
`

// Config is the command-line configuration.
type Config struct {
	// TorrentFile is the .torrent file to stream.
	TorrentFile string
	// SocketFile is the unix domain socket the player connects to.
	SocketFile string
}

// Run parses the command line into a Config and reports the exit code to use
// when no Config is returned. Output goes to stdout and diagnostics to stderr;
// both are injected for testability.
func Run(args []string, stdout io.Writer, stderr io.Writer) (*Config, int) {
	fs := flag.NewFlagSet("torrent_player", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { usage(stderr) }
	help := fs.Bool("h", false, "show usage and exit")
	socket := fs.String("socket", DefaultSocketPath, "unix domain socket for the player")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, ExitSuccess
		}
		return nil, ExitCliFail
	}
	if *help {
		usage(stdout)
		return nil, ExitSuccess
	}
	if fs.NArg() != 1 {
		usage(stderr)
		return nil, ExitCliFail
	}
	if *socket == "" {
		fmt.Fprintln(stderr, "torrent_player: -socket must not be empty")
		return nil, ExitCliFail
	}

	return &Config{TorrentFile: fs.Arg(0), SocketFile: *socket}, ExitSuccess
}

func usage(w io.Writer) {
	fmt.Fprintf(w, usageMsg)
}

func PrintDict(w io.Writer, t *torrent.TorrentData) {
	fmt.Fprintf(w, "announce: %s\n", t.AnnounceURL)
	if t.Comment != "" {
		fmt.Fprintf(w, "comment: %s\n", t.Comment)
	}
	fmt.Fprintf(w, "creation date: %d\n", t.CreationDate)
	fmt.Fprintf(w, "info hash: %s\n", hex.EncodeToString(t.InfoHash[:]))
	fmt.Fprintln(w, "info:")
	fmt.Fprintf(w, "  name: %s\n", t.Info.Name)
	fmt.Fprintf(w, "  piece length: %d\n", t.Info.PieceLength)
	fmt.Fprintf(w, "  length: %d\n", t.Info.Length)
	fmt.Fprintf(w, "  pieces: %d (%d bytes)\n", t.Info.PieceCount(), len(t.Info.Pieces))
}
