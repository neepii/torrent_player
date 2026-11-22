// Package playback remuxes a progressive MP4 stream into fragmented MP4 and
// hands it to an FFmpeg-based player over a unix domain socket.
package playback

import "fmt"

// errPrefix is prepended to every error returned by this package.
const errPrefix = "playback:"

// errf returns a new error whose message is prefixed with errPrefix.
func errf(format string, args ...any) error {
	return fmt.Errorf(errPrefix+" "+format, args...)
}
