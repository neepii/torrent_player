package main

import (
	"os"

	"github.com/neepii/torrent_player/src/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
