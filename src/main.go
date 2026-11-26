package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/neepii/torrent_player/src/cli"
	"github.com/neepii/torrent_player/src/download"
	"github.com/neepii/torrent_player/src/playback"
	"github.com/neepii/torrent_player/src/torrent"
	"github.com/neepii/torrent_player/src/tracker"
)

const (
	standardPort   = 6881
	standardPeerID = "-MY0001-deadbeef01xZ"

	// ignoreIndexOption makes the FFmpeg mov demuxer read large fragmented
	// MP4 streams sequentially. FFmpeg's unix protocol is not marked as a
	// streamed input, so the demuxer otherwise skips the mdat while probing
	// and then cannot seek back on the non-seekable socket.
	ignoreIndexOption = "--demuxer-lavf-o=fflags=+ignidx"
)

func main() {
	cfg, code := cli.Run(os.Args[1:], os.Stdout, os.Stderr)
	if cfg == nil {
		os.Exit(code)
	}
	if err := run(cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(cli.ExitInternalFail)
	}
}

// run connects the parser, tracker, piece source and playback adapter and
// streams the torrent to the player until playback ends.
func run(cfg *cli.Config) error {
	data, err := os.ReadFile(cfg.TorrentFile)
	if err != nil {
		return fmt.Errorf("torrent: %w", err)
	}
	meta, err := torrent.Parse(data)
	if err != nil {
		return err
	}
	cli.PrintDict(os.Stdout, meta)

	var peerID [20]byte
	copy(peerID[:], standardPeerID)

	response, err := tracker.Announce(context.Background(), meta.AnnounceURL, tracker.AnnounceRequest{
		InfoHash: meta.InfoHash,
		PeerID:   peerID,
		Port:     standardPort,
		Left:     meta.Info.Length,
		Event:    tracker.EventStarted,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "tracker: %d peers, interval %s\n", len(response.Peers), response.Interval)
	for _, peer := range response.Peers {
		fmt.Fprintf(os.Stdout, "peer: %s\n", peer)
	}
	if len(response.Peers) == 0 {
		return errors.New("tracker: no peers")
	}

	source, err := download.New(meta, response.Peers, peerID, download.Options{})
	if err != nil {
		return err
	}
	defer source.Close()

	reader, err := download.NewReader(source, meta.Info.Length)
	if err != nil {
		return err
	}

	adapter := playback.Adapter{SocketPath: cfg.SocketFile}
	listener, err := adapter.Listen()
	if err != nil {
		return err
	}
	defer listener.Close()
	defer os.Remove(cfg.SocketFile)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	streamErr := make(chan error, 1)
	go func() { streamErr <- adapter.Stream(listener, reader) }()

	player := playback.Player{
		SocketPath: cfg.SocketFile,
		Args:       []string{ignoreIndexOption},
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
	}
	cmd, err := player.Start(ctx)
	if err != nil {
		listener.Close()
		<-streamErr
		return err
	}

	waitErr := cmd.Wait()
	// The player may exit before connecting or while the stream is running.
	// Closing the listener and waiting for Stream lets both sides finish
	// without leaving a goroutine behind.
	listener.Close()
	streamErrVal := <-streamErr

	if ctx.Err() != nil {
		return nil
	}
	if streamErrVal != nil {
		return streamErrVal
	}
	if waitErr != nil {
		return fmt.Errorf("player: %w", waitErr)
	}
	return nil
}
