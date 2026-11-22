package playback_test

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"testing"

	"github.com/Eyevinn/mp4ff/mp4"
)

const (
	// samplePath is the local-swarm fixture: one keyframe, 20 samples, 2 s.
	samplePath = "../../testdata/swarm/sample.mp4"
	// multiKeyframePath is the same content re-encoded with a keyframe every
	// 0.5 s, i.e. four chunks of five samples.
	multiKeyframePath = "../../testdata/playback/multikeyframe.mp4"
)

// openSource opens a fixture file as an io.ReadSeeker.
func openSource(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

// topLevelBoxTypes walks the box headers of a byte stream and returns the
// type of every top-level box in order.
func topLevelBoxTypes(t *testing.T, data []byte) []string {
	t.Helper()
	var types []string
	for off := 0; off < len(data); {
		if off+8 > len(data) {
			t.Fatalf("truncated box header at offset %d of %d bytes", off, len(data))
		}
		size := int(binary.BigEndian.Uint32(data[off : off+4]))
		typ := string(data[off+4 : off+8])
		if size < 8 {
			t.Fatalf("invalid size %d for box %q at offset %d", size, typ, off)
		}
		types = append(types, typ)
		off += size
	}
	return types
}

// decodeFragmentCount decodes a fragmented MP4 byte stream and returns the
// number of fragments and the total number of full samples.
func decodeFragmentCount(t *testing.T, data []byte) (fragments, samples int) {
	t.Helper()
	f, err := mp4.DecodeFile(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode fragmented stream: %v", err)
	}
	if !f.IsFragmented() {
		t.Fatal("decoded stream is not fragmented")
	}
	if f.Init == nil || f.Init.Moov == nil || f.Init.Moov.Mvex == nil || len(f.Init.Moov.Mvex.Trexs) == 0 {
		t.Fatal("init segment has no mvex/trex")
	}
	trex := f.Init.Moov.Mvex.Trexs[0]
	for _, seg := range f.Segments {
		for _, frag := range seg.Fragments {
			full, err := frag.GetFullSamples(trex)
			if err != nil {
				t.Fatalf("get full samples: %v", err)
			}
			fragments++
			samples += len(full)
		}
	}
	return fragments, samples
}

// readAll copies all remaining bytes from r.
func readAll(t *testing.T, r io.Reader) []byte {
	t.Helper()
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read all: %v", err)
	}
	return data
}
