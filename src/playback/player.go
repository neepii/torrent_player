package playback

import (
	"context"
	"io"
	"os/exec"
)

// Player is the external FFmpeg-based player process that plays the stream.
type Player struct {
	// Path is the player executable. It defaults to "mpv".
	Path string
	// SocketPath is the unix domain socket the player connects to.
	SocketPath string
	// Args are extra arguments placed before the stream URL.
	Args []string
	// Stdout and Stderr receive the player output. Nil means discard.
	Stdout io.Writer
	Stderr io.Writer
}

// URL is the media URL the player opens. The ffmpeg:// prefix makes the player
// use its FFmpeg demuxer for the raw stream on the socket.
func (p *Player) URL() string {
	return "ffmpeg://unix:" + p.SocketPath
}

// Start launches the player. The caller must call Wait on the returned command.
// The process is killed when ctx is done.
func (p *Player) Start(ctx context.Context) (*exec.Cmd, error) {
	path := p.Path
	if path == "" {
		path = "mpv"
	}
	args := append(append([]string{}, p.Args...), p.URL())

	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdout = p.Stdout
	cmd.Stderr = p.Stderr
	if err := cmd.Start(); err != nil {
		return nil, errf("start %s: %w", path, err)
	}
	return cmd, nil
}
