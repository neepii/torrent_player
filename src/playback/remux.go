package playback

import (
	"io"
	"time"

	"github.com/Eyevinn/mp4ff/mp4"
)

// Remuxer rewrites a progressive (non-fragmented) MP4 file as fragmented MP4.
//
// The output is the in-process equivalent of
//
//	ffmpeg -i in.mp4 -c copy -movflags frag_keyframe+empty_moov+default_base_moof
//
// that is, an init segment (ftyp + empty moov with mvex/trex) followed by one
// moof/mdat fragment per fragment boundary. Sample payloads are copied, never
// re-encoded.
type Remuxer struct {
	// FragmentDuration, when greater than zero, limits how much media a single
	// fragment may describe: a new fragment starts at the first keyframe at or
	// after this much media has passed. Zero means a new fragment at every
	// keyframe, which matches the frag_keyframe movflag.
	FragmentDuration time.Duration
}

// WriteTo writes src as fragmented MP4 to w.
//
// src must be a progressive MP4 and must stay readable for the whole call,
// since sample data is read on demand. Fragments are written as they are
// produced, so a player can start before the last fragment exists.
func (r *Remuxer) WriteTo(w io.Writer, src io.ReadSeeker) error {
	inFile, err := mp4.DecodeFile(src, mp4.WithDecodeMode(mp4.DecModeLazyMdat))
	if err != nil {
		return errf("decode MP4: %w", err)
	}
	if inFile.IsFragmented() {
		return errf("input is already fragmented")
	}
	if inFile.Moov == nil || inFile.Moov.Mvhd == nil {
		return errf("input has no moov box with an mvhd box")
	}
	if len(inFile.Moov.Traks) != 1 {
		return errf("input has %d tracks, only single-track MP4 is supported", len(inFile.Moov.Traks))
	}

	trak := inFile.Moov.Traks[0]
	if err := checkTrack(trak); err != nil {
		return err
	}

	init, trackID, err := initSegment(inFile, trak)
	if err != nil {
		return err
	}
	if err := init.Encode(w); err != nil {
		return errf("write init segment: %w", err)
	}
	// The player cannot use any fragment before it has the init segment, so
	// hand that out as soon as it exists rather than with the first fragment.
	if f, ok := w.(flusher); ok {
		if err := f.Flush(); err != nil {
			return errf("flush init segment: %w", err)
		}
	}

	bounds, err := r.fragmentBounds(trak)
	if err != nil {
		return err
	}
	for i, b := range bounds {
		frag, err := createFragment(trak, src, trackID, uint32(i+1), b.start, b.end)
		if err != nil {
			return err
		}
		if err := frag.Encode(w); err != nil {
			return errf("write fragment %d: %w", i+1, err)
		}
		// Hand the fragment to the player as soon as it exists instead of
		// keeping it in a buffer until the whole stream has been remuxed.
		if f, ok := w.(flusher); ok {
			if err := f.Flush(); err != nil {
				return errf("flush fragment %d: %w", i+1, err)
			}
		}
	}
	return nil
}

// flusher is a writer that holds back data until it is flushed, such as a
// bufio.Writer, so that the remuxer can push each fragment out on its own.
type flusher interface {
	Flush() error
}

// checkTrack rejects tracks this remuxer cannot copy.
func checkTrack(trak *mp4.TrakBox) error {
	if trak.Mdia == nil || trak.Mdia.Mdhd == nil || trak.Mdia.Hdlr == nil ||
		trak.Mdia.Minf == nil || trak.Mdia.Minf.Stbl == nil {
		return errf("track has no complete mdia/minf/stbl structure")
	}
	if got := trak.Mdia.Hdlr.HandlerType; got != "vide" {
		return errf("track handler %q is not supported, only video is", got)
	}
	stbl := trak.Mdia.Minf.Stbl
	switch {
	case stbl.Stsd == nil || stbl.Stsd.AvcX == nil:
		return errf("track has no AVC sample description")
	case stbl.Stts == nil:
		return errf("track has no stts box")
	case stbl.Stsz == nil:
		return errf("track has no stsz box")
	case stbl.Stsc == nil:
		return errf("track has no stsc box")
	case stbl.Stco == nil && stbl.Co64 == nil:
		return errf("track has no stco or co64 box")
	}
	if err := trak.CheckDataIsSelfContained(); err != nil {
		return errf("track data is not self-contained: %w", err)
	}
	return nil
}

// initSegment builds the fragmented init segment for trak and returns the track
// ID used in the fragments.
func initSegment(inFile *mp4.File, trak *mp4.TrakBox) (*mp4.InitSegment, uint32, error) {
	init := mp4.CreateEmptyInit()
	init.Moov.Mvhd.Timescale = inFile.Moov.Mvhd.Timescale
	init.Moov.Mvex.AddChild(&mp4.MehdBox{FragmentDuration: int64(inFile.Moov.Mvhd.Duration)})

	out := init.AddEmptyTrack(trak.Mdia.Mdhd.Timescale, "video", trak.Mdia.Mdhd.GetLanguage())
	out.Mdia.Minf.Stbl.Stsd.AddChild(trak.Mdia.Minf.Stbl.Stsd.AvcX)

	ftyp, err := init.GenerateFtyp(mp4.FtypOptions{})
	if err != nil {
		return nil, 0, errf("generate ftyp: %w", err)
	}
	init.SetFtyp(ftyp)
	return init, out.Tkhd.TrackID, nil
}

// sampleBounds is an inclusive, one-based sample number interval.
type sampleBounds struct {
	start, end uint32
}

// fragmentBounds splits the track's samples into fragments that start at a
// keyframe.
func (r *Remuxer) fragmentBounds(trak *mp4.TrakBox) ([]sampleBounds, error) {
	stbl := trak.Mdia.Minf.Stbl
	nrSamples := trak.GetNrSamples()
	if nrSamples == 0 {
		return nil, errf("track has no samples")
	}

	var starts []uint32
	var lastDecodeTime uint64
	haveStart := false
	for nr := uint32(1); nr <= nrSamples; nr++ {
		if stbl.Stss != nil && !stbl.Stss.IsSyncSample(nr) {
			continue
		}
		if !haveStart {
			starts = append(starts, nr)
			haveStart = true
			lastDecodeTime, _, _ = stbl.Stts.GetDecodeTime(nr)
			continue
		}
		if r.FragmentDuration <= 0 {
			starts = append(starts, nr)
			continue
		}
		decodeTime, _, err := stbl.Stts.GetDecodeTime(nr)
		if err != nil {
			return nil, errf("decode time of sample %d: %w", nr, err)
		}
		if decodeTime-lastDecodeTime >= r.fragmentDuration(trak) {
			starts = append(starts, nr)
			lastDecodeTime = decodeTime
		}
	}

	bounds := make([]sampleBounds, len(starts))
	for i, start := range starts {
		end := nrSamples
		if i+1 < len(starts) {
			end = starts[i+1] - 1
		}
		bounds[i] = sampleBounds{start: start, end: end}
	}
	return bounds, nil
}

// fragmentDuration converts FragmentDuration to the track's media timescale.
func (r *Remuxer) fragmentDuration(trak *mp4.TrakBox) uint64 {
	timescale := uint64(trak.Mdia.Mdhd.Timescale)
	return uint64(r.FragmentDuration) * timescale / uint64(time.Second)
}

// createFragment builds one moof/mdat fragment for the given sample interval.
func createFragment(trak *mp4.TrakBox, src io.ReadSeeker, trackID, seq uint32, start, end uint32) (*mp4.Fragment, error) {
	stbl := trak.Mdia.Minf.Stbl
	samples, err := trak.GetSampleData(start, end)
	if err != nil {
		return nil, errf("samples %d-%d: %w", start, end, err)
	}
	data, err := readSampleData(trak, src, start, end)
	if err != nil {
		return nil, err
	}

	frag, err := mp4.CreateFragment(seq, trackID)
	if err != nil {
		return nil, errf("create fragment %d: %w", seq, err)
	}

	var pos uint32
	for i, s := range samples {
		decodeTime, _, err := stbl.Stts.GetDecodeTime(start + uint32(i))
		if err != nil {
			return nil, errf("decode time of sample %d: %w", start+uint32(i), err)
		}
		if pos+s.Size > uint32(len(data)) {
			return nil, errf("sample %d runs past the sample data", start+uint32(i))
		}
		frag.AddFullSample(mp4.FullSample{
			Sample:     s,
			DecodeTime: decodeTime,
			Data:       data[pos : pos+s.Size],
		})
		pos += s.Size
	}
	return frag, nil
}

// readSampleData reads the payload of an inclusive sample number interval.
func readSampleData(trak *mp4.TrakBox, src io.ReadSeeker, start, end uint32) ([]byte, error) {
	ranges, err := trak.GetRangesForSampleInterval(start, end)
	if err != nil {
		return nil, errf("ranges for samples %d-%d: %w", start, end, err)
	}
	var data []byte
	for _, r := range ranges {
		if _, err := src.Seek(int64(r.Offset), io.SeekStart); err != nil {
			return nil, errf("seek to %d: %w", r.Offset, err)
		}
		chunk := make([]byte, r.Size)
		if _, err := io.ReadFull(src, chunk); err != nil {
			return nil, errf("read %d bytes at %d: %w", r.Size, r.Offset, err)
		}
		data = append(data, chunk...)
	}
	return data, nil
}
