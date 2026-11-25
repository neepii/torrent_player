package playback

import (
	"bufio"
	"errors"
	"io"
	"net"
	"os"
	"sync"
)

const (
	KiB                   = 1024
	streamWriteBufferKB   = 64
	streamWriteBufferSize = streamWriteBufferKB * KiB
	// streamQueueDepth is how many flushed chunks may wait between the remuxer
	// and the socket writer, so a slow player does not stall fragment
	// production and a fast player does not make the remuxer wait.
	streamQueueDepth = 8
)

// errStreamDone reports a write to a player stream that has already stopped.
var errStreamDone = errors.New("stream stopped")

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
//
// Remuxing and socket delivery run on separate goroutines joined by a bounded
// queue, so a fragment reaches the player as soon as the remuxer flushes it
// instead of after the whole source has been read.
func (a *Adapter) Stream(l net.Listener, src io.ReadSeeker) error {
	conn, err := l.Accept()
	if err != nil {
		return errf("accept player connection: %w", err)
	}
	defer conn.Close()

	queue := make(chan []byte, streamQueueDepth)
	done := make(chan struct{})
	var doneOnce sync.Once
	stop := func() { doneOnce.Do(func() { close(done) }) }

	var firstErr firstError
	fail := func(err error) {
		firstErr.set(err)
		stop()
	}

	writer := &queueWriter{ch: queue, done: done}
	w := bufio.NewWriterSize(conn, streamWriteBufferSize)

	var wg sync.WaitGroup
	wg.Add(2)
	// Producer: only this goroutine closes queue.
	go func() {
		defer wg.Done()
		defer close(queue)
		if err := a.Remuxer.WriteTo(writer, src); err != nil {
			fail(err)
			return
		}
		if err := writer.Flush(); err != nil {
			fail(err)
		}
	}()
	// Consumer: hands every queued chunk to the player immediately.
	go func() {
		defer wg.Done()
		for chunk := range queue {
			if _, err := w.Write(chunk); err != nil {
				fail(errf("write to player: %w", err))
				return
			}
			if err := w.Flush(); err != nil {
				fail(errf("flush to player: %w", err))
				return
			}
		}
	}()
	wg.Wait()

	return firstErr.get()
}

// firstError keeps the first error reported by either stream goroutine.
type firstError struct {
	mu  sync.Mutex
	err error
}

// set records err unless an error was recorded before.
func (e *firstError) set(err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.err == nil {
		e.err = err
	}
}

// get returns the recorded error, or nil if there was none.
func (e *firstError) get() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.err
}

// queueWriter buffers what the remuxer writes and hands the buffered bytes to
// the socket writer goroutine on Flush, one queue entry per flush.
//
// Every Flush is preceded by the remuxer writing a complete fragment, so a
// queue entry is at least a fragment in size.
type queueWriter struct {
	ch   chan<- []byte
	done <-chan struct{}
	buf  []byte
}

// Write appends p to the pending bytes. It fails once the stream has stopped so
// that the producer goroutine always terminates.
func (w *queueWriter) Write(p []byte) (int, error) {
	select {
	case <-w.done:
		return 0, errf("write to player: %w", errStreamDone)
	default:
	}
	w.buf = append(w.buf, p...)
	return len(p), nil
}

// Flush hands the pending bytes to the socket writer goroutine and resets the
// buffer. The handed-off slice is never written again by this writer.
func (w *queueWriter) Flush() error {
	if len(w.buf) == 0 {
		return nil
	}
	chunk := w.buf
	w.buf = nil
	select {
	case w.ch <- chunk:
		return nil
	case <-w.done:
		return errf("write to player: %w", errStreamDone)
	}
}
