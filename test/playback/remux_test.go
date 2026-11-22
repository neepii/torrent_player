package playback_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Eyevinn/mp4ff/mp4"
	"github.com/neepii/torrent_player/src/playback"
)

// TestRemuxerWritesInitSegmentThenOneFragment covers the equivalent of
//
//	ffmpeg -i sample.mp4 -c copy -movflags frag_keyframe+empty_moov+default_base_moof
//
// for a source with a single keyframe: one init segment with an empty moov
// (mvex/trex) followed by exactly one moof/mdat fragment.
func TestRemuxerWritesInitSegmentThenOneFragment(t *testing.T) {
	var out bytes.Buffer
	r := playback.Remuxer{}
	if err := r.WriteTo(&out, openSource(t, samplePath)); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	data := out.Bytes()

	wantBoxes := []string{"ftyp", "moov", "moof", "mdat"}
	if got := topLevelBoxTypes(t, data); !equalStrings(got, wantBoxes) {
		t.Fatalf("boxes = %v, want %v", got, wantBoxes)
	}

	fragments, samples := decodeFragmentCount(t, data)
	if fragments != 1 {
		t.Errorf("fragments = %d, want 1", fragments)
	}
	if samples != 20 {
		t.Errorf("samples = %d, want 20", samples)
	}
}

// TestRemuxerUsesEmptyMoovAndDefaultBaseMoof checks the two movflags that make
// the stream playable while it is still being written.
func TestRemuxerUsesEmptyMoovAndDefaultBaseMoof(t *testing.T) {
	var out bytes.Buffer
	r := playback.Remuxer{}
	if err := r.WriteTo(&out, openSource(t, samplePath)); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	f, err := mp4.DecodeFile(bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	// empty_moov: the init segment keeps no sample tables, only track headers.
	if f.Init.Moov.Traks[0].Mdia.Minf.Stbl.Stsz.GetNrSamples() != 0 {
		t.Error("init segment moov still has a sample size table, want empty moov")
	}
	if len(f.Init.Moov.Mvex.Trexs) != 1 {
		t.Fatalf("trex boxes = %d, want 1", len(f.Init.Moov.Mvex.Trexs))
	}
	if got := f.Init.Moov.Traks[0].Mdia.Hdlr.HandlerType; got != "vide" {
		t.Errorf("init track handler = %q, want %q", got, "vide")
	}

	// default_base_moof: fragment data offsets are relative to the moof.
	tfhd := f.Segments[0].Fragments[0].Moof.Traf.Tfhd
	if !tfhd.DefaultBaseIfMoof() {
		t.Error("tfhd does not set default-base-is-moof")
	}
}

// TestRemuxerFragmentsAtEveryKeyframe checks that fragment boundaries follow
// sync samples, which is what the frag_keyframe movflag does.
func TestRemuxerFragmentsAtEveryKeyframe(t *testing.T) {
	var out bytes.Buffer
	r := playback.Remuxer{}
	if err := r.WriteTo(&out, openSource(t, multiKeyframePath)); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	data := out.Bytes()

	wantBoxes := []string{"ftyp", "moov", "moof", "mdat", "moof", "mdat", "moof", "mdat", "moof", "mdat"}
	if got := topLevelBoxTypes(t, data); !equalStrings(got, wantBoxes) {
		t.Fatalf("boxes = %v, want %v", got, wantBoxes)
	}

	fragments, samples := decodeFragmentCount(t, data)
	if fragments != 4 {
		t.Errorf("fragments = %d, want 4", fragments)
	}
	if samples != 20 {
		t.Errorf("samples = %d, want 20 (every sample must survive remuxing)", samples)
	}

	// Every fragment must start with a sync sample.
	f, err := mp4.DecodeFile(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	trex := f.Init.Moov.Mvex.Trexs[0]
	for i, frag := range f.Segments[0].Fragments {
		full, err := frag.GetFullSamples(trex)
		if err != nil {
			t.Fatalf("fragment %d: get full samples: %v", i, err)
		}
		if flags := mp4.DecodeSampleFlags(full[0].Flags); flags.SampleIsNonSync {
			t.Errorf("fragment %d starts with a non-sync sample", i)
		}
	}
}

// TestRemuxerPreservesSampleData checks that the payload written to the player
// is the source payload, sample by sample.
func TestRemuxerPreservesSampleData(t *testing.T) {
	var out bytes.Buffer
	r := playback.Remuxer{}
	if err := r.WriteTo(&out, openSource(t, multiKeyframePath)); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	want := sourceSampleData(t, multiKeyframePath)
	got := fragmentedSampleData(t, out.Bytes())
	if len(got) != len(want) {
		t.Fatalf("streamed %d samples, want %d", len(got), len(want))
	}
	for i := range want {
		if !bytes.Equal(got[i], want[i]) {
			t.Errorf("sample %d data differs from the source", i)
		}
	}
}

// TestRemuxerRejectsFragmentedInput checks that already-fragmented input is
// reported instead of silently producing a broken stream.
func TestRemuxerRejectsFragmentedInput(t *testing.T) {
	var first bytes.Buffer
	r := playback.Remuxer{}
	if err := r.WriteTo(&first, openSource(t, samplePath)); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	var second bytes.Buffer
	err := r.WriteTo(&second, bytes.NewReader(first.Bytes()))
	if err == nil {
		t.Fatal("WriteTo() accepted already-fragmented input")
	}
	if !strings.Contains(err.Error(), "fragmented") {
		t.Errorf("error = %q, want it to mention fragmented input", err)
	}
}

// sourceSampleData returns the sample payloads of a progressive MP4.
func sourceSampleData(t *testing.T, path string) [][]byte {
	t.Helper()
	src := openSource(t, path)
	f, err := mp4.DecodeFile(src)
	if err != nil {
		t.Fatalf("decode source: %v", err)
	}
	trak := f.Moov.Traks[0]
	var data [][]byte
	for nr := uint32(1); nr <= trak.GetNrSamples(); nr++ {
		samples, err := trak.GetSampleData(nr, nr)
		if err != nil {
			t.Fatalf("sample %d: %v", nr, err)
		}
		ranges, err := trak.GetRangesForSampleInterval(nr, nr)
		if err != nil {
			t.Fatalf("sample %d ranges: %v", nr, err)
		}
		var buf []byte
		for _, r := range ranges {
			if _, err := src.Seek(int64(r.Offset), 0); err != nil {
				t.Fatalf("seek: %v", err)
			}
			chunk := make([]byte, r.Size)
			if _, err := src.Read(chunk); err != nil {
				t.Fatalf("read: %v", err)
			}
			buf = append(buf, chunk...)
		}
		data = append(data, buf[:samples[0].Size])
	}
	return data
}

// fragmentedSampleData returns the sample payloads of a fragmented MP4.
func fragmentedSampleData(t *testing.T, data []byte) [][]byte {
	t.Helper()
	f, err := mp4.DecodeFile(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode fragmented stream: %v", err)
	}
	trex := f.Init.Moov.Mvex.Trexs[0]
	var samples [][]byte
	for _, seg := range f.Segments {
		for _, frag := range seg.Fragments {
			full, err := frag.GetFullSamples(trex)
			if err != nil {
				t.Fatalf("get full samples: %v", err)
			}
			for _, s := range full {
				samples = append(samples, s.Data)
			}
		}
	}
	return samples
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
