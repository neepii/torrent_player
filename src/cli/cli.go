// Package cli implements the torrent_player command-line interface.
package cli

import (
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/neepii/torrent_player/src/torrent"
)

// Run executes the CLI with the given arguments and returns the process exit
// code. Output goes to stdout and diagnostics to stderr; both are injected for
// testability.
func Run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("torrent_player", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { usage(stderr) }
	help := fs.Bool("h", false, "show usage and exit")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *help {
		usage(stdout)
		return 0
	}
	if fs.NArg() != 1 {
		usage(stderr)
		return 2
	}

	data, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "torrent: %v\n", err)
		return 1
	}

	t, err := torrent.Parse(data)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	printDict(stdout, t)
	return 0
}

func usage(w io.Writer) {
	fmt.Fprintf(w, `Usage: torrent_player [options] <file.torrent>

Parse a v1 single-file .torrent and print its metadata dictionary.

Options:
  -h    show this help and exit
`)
}

func printDict(w io.Writer, t *torrent.TorrentData) {
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
