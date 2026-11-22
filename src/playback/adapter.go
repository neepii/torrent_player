package playback

import (
	"bufio"
	"io"
	"net"
	"os"
)

const (
	KiB = 1024
	streamWriteBufferKB = 64
	streamWriteBufferSize = streamWriteBufferKB * KiB
)

// Adapter streams a progressive MP4 to a player over a unix domain socket.
//
// The adapter is the socket side of the player integration: it creates the
// socket the FFmpeg-based player connects to, accepts exactly one connection
// and writes the fragmented MP4 stream to it.
type Adapter struct {
	// SocketPath is the unix domain socket the player connects to.
	SocketPath string
	// Remuxer produces the fragmented MP4 written to the player.
	Remuxer Remuxer
}

// Listen creates the unix domain socket at SocketPath and starts listening.
//
// It is separate from Serve so that callers can start the player once the
// socket exists; Serve accepts the connection afterwards.
func (a *Adapter) Listen() (net.Listener, error) {
	if a.SocketPath == "" {
		return nil, errf("socket path is required")
	}
	l, err := net.Listen("unix", a.SocketPath)
	if err != nil {
		return nil, errf("listen on %s: %w", a.SocketPath, err)
	}
	return l, nil
}

// Serve creates the socket, waits for the player to connect, writes src as
// fragmented MP4 and removes the socket again.
func (a *Adapter) Serve(src io.ReadSeeker) error {
	l, err := a.Listen()
	if err != nil {
		return err
	}
	defer func() {
		l.Close()
		os.Remove(a.SocketPath)
	}()
	return a.Stream(l, src)
}

// Stream accepts one player connection on l and writes src to it as fragmented
// MP4. It returns when the stream is complete or the player goes away.
func (a *Adapter) Stream(l net.Listener, src io.ReadSeeker) error {
	conn, err := l.Accept()
	if err != nil {
		return errf("accept player connection: %w", err)
	}
	defer conn.Close()

	w := bufio.NewWriterSize(conn, streamWriteBufferSize)
	if err := a.Remuxer.WriteTo(w, src); err != nil {
		return err
	}
	if err := w.Flush(); err != nil {
		return errf("flush to player: %w", err)
	}
	return nil
}
