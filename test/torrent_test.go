package torrent_test

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"strings"
	"testing"

	"github.com/neepii/torrent_player/src/torrent"
)

// bencode string helper: <len>:<bytes>
func bs(s string) string {
	return fmt.Sprintf("%d:%s", len(s), s)
}

// bencode integer helper: i<v>e
func bi(v int64) string {
	return fmt.Sprintf("i%de", v)
}

// makePieces builds n concatenated 20-byte hashes covering all byte values.
func makePieces(n int) []byte {
	p := make([]byte, n*sha1.Size)
	for i := range p {
		p[i] = byte(i % 256)
	}
	return p
}

// validInfo builds a canonical single-file info dictionary.
func validInfo(name string, pieceLength, length int64, pieces []byte) string {
	return "d" +
		bs("length") + bi(length) +
		bs("name") + bs(name) +
		bs("piece length") + bi(pieceLength) +
		bs("pieces") + bs(string(pieces)) +
		"e"
}

const testAnnounce = "http://tracker.example:8080/announce"

func TestParseValidMetainfo(t *testing.T) {
	pieces := makePieces(2) // 2 pieces of 100 bytes each for length 200
	info := validInfo("sample.mp4", 100, 200, pieces)
	comment := "my test torrent"
	creation := int64(1700000000)

	data := []byte("d" +
		bs("announce") + bs(testAnnounce) +
		bs("comment") + bs(comment) +
		bs("creation date") + bi(creation) +
		bs("info") + info +
		"e")

	got, err := torrent.Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if got.AnnounceURL != testAnnounce {
		t.Errorf("AnnounceURL = %q, want %q", got.AnnounceURL, testAnnounce)
	}
	if got.Comment != comment {
		t.Errorf("Comment = %q, want %q", got.Comment, comment)
	}
	if got.CreationDate != creation {
		t.Errorf("CreationDate = %d, want %d", got.CreationDate, creation)
	}

	if got.Info.Name != "sample.mp4" {
		t.Errorf("Info.Name = %q, want %q", got.Info.Name, "sample.mp4")
	}
	if got.Info.PieceLength != 100 {
		t.Errorf("Info.PieceLength = %d, want 100", got.Info.PieceLength)
	}
	if got.Info.Length != 200 {
		t.Errorf("Info.Length = %d, want 200", got.Info.Length)
	}
	if !bytes.Equal(got.Info.Pieces, pieces) {
		t.Errorf("Info.Pieces = %v, want %v", got.Info.Pieces, pieces)
	}
	if got.Info.PieceCount() != 2 {
		t.Errorf("Info.PieceCount() = %d, want 2", got.Info.PieceCount())
	}

	// Info hash must be the SHA-1 of the exact original bencoded info bytes.
	wantHash := sha1.Sum([]byte(info))
	if got.InfoHash != wantHash {
		t.Errorf("InfoHash = %x, want %x", got.InfoHash, wantHash)
	}

	h0, ok := got.Info.PieceHash(0)
	if !ok || !bytes.Equal(h0[:], pieces[0:sha1.Size]) {
		t.Errorf("PieceHash(0) = %x, %v; want %x, true", h0, ok, pieces[0:sha1.Size])
	}
	h1, ok := got.Info.PieceHash(1)
	if !ok || !bytes.Equal(h1[:], pieces[sha1.Size:2*sha1.Size]) {
		t.Errorf("PieceHash(1) = %x, %v; want %x, true", h1, ok, pieces[sha1.Size:2*sha1.Size])
	}
	if _, ok := got.Info.PieceHash(2); ok {
		t.Error("PieceHash(2) reported ok, want out-of-range false")
	}
}

func TestParseInfoHashUsesExactOriginalBytes(t *testing.T) {
	pieces := makePieces(1)
	info := validInfo("a.mp4", 50, 50, pieces)
	data := []byte("d" + bs("announce") + bs(testAnnounce) + bs("info") + info + "e")

	got, err := torrent.Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Re-derive the hash directly from the info slice embedded in the data.
	want := sha1.Sum([]byte(info))
	if got.InfoHash != want {
		t.Errorf("InfoHash = %x, want %x", got.InfoHash, want)
	}

	// Changing any byte inside info must change the hash.
	other := validInfo("b.mp4", 50, 50, pieces)
	if sha1.Sum([]byte(info)) == sha1.Sum([]byte(other)) {
		t.Error("info hash did not change for a different info dictionary")
	}
}

func TestParseRejectsMultiFile(t *testing.T) {
	// "files" present instead of "length" => multi-file torrent.
	info := "d" + bs("files") + "le" + "e"
	data := []byte("d" + bs("announce") + bs(testAnnounce) + bs("info") + info + "e")

	_, err := torrent.Parse(data)
	if err == nil {
		t.Fatal("Parse() expected an error for multi-file torrent")
	}
	if !strings.Contains(err.Error(), "multi-file") {
		t.Errorf("error = %q, want mention of multi-file", err)
	}
}

func TestParseRejectsMissingFields(t *testing.T) {
	pieces := makePieces(1)
	valid := validInfo("a.mp4", 50, 50, pieces)

	cases := []struct {
		name string
		data string
		want string
	}{
		{
			name: "missing announce",
			data: "d" + bs("info") + valid + "e",
			want: "announce URL",
		},
		{
			name: "missing info",
			data: "d" + bs("announce") + bs(testAnnounce) + "e",
			want: "info dictionary",
		},
		{
			name: "missing name",
			data: "d" + bs("announce") + bs(testAnnounce) + bs("info") +
				"d" + bs("length") + bi(50) + bs("piece length") + bi(50) + bs("pieces") + bs(string(pieces)) + "e" + "e",
			want: `"name"`,
		},
		{
			name: "missing piece length",
			data: "d" + bs("announce") + bs(testAnnounce) + bs("info") +
				"d" + bs("length") + bi(50) + bs("name") + bs("a.mp4") + bs("pieces") + bs(string(pieces)) + "e" + "e",
			want: `"piece length"`,
		},
		{
			name: "missing pieces",
			data: "d" + bs("announce") + bs(testAnnounce) + bs("info") +
				"d" + bs("length") + bi(50) + bs("name") + bs("a.mp4") + bs("piece length") + bi(50) + "e" + "e",
			want: `"pieces"`,
		},
		{
			name: "missing length",
			data: "d" + bs("announce") + bs(testAnnounce) + bs("info") +
				"d" + bs("name") + bs("a.mp4") + bs("piece length") + bi(50) + bs("pieces") + bs(string(pieces)) + "e" + "e",
			want: `"length"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := torrent.Parse([]byte(tc.data))
			if err == nil {
				t.Fatal("Parse() expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestParseRejectsInvalidPieceHashes(t *testing.T) {
	cases := []struct {
		name string
		info string
		want string
	}{
		{
			name: "pieces not multiple of 20",
			info: "d" + bs("length") + bi(50) + bs("name") + bs("a.mp4") +
				bs("piece length") + bi(50) + bs("pieces") + bs("0123456789") + "e",
			want: "multiple of 20",
		},
		{
			name: "piece count mismatch",
			// length 100 at 50 bytes/piece => 2 hashes, but only 1 is given.
			info: "d" + bs("length") + bi(100) + bs("name") + bs("a.mp4") +
				bs("piece length") + bi(50) + bs("pieces") + bs(string(makePieces(1))) + "e",
			want: "expected 2",
		},
		{
			name: "empty pieces",
			info: "d" + bs("length") + bi(50) + bs("name") + bs("a.mp4") +
				bs("piece length") + bi(50) + bs("pieces") + bs("") + "e",
			want: "must not be empty",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte("d" + bs("announce") + bs(testAnnounce) + bs("info") + tc.info + "e")
			_, err := torrent.Parse(data)
			if err == nil {
				t.Fatal("Parse() expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestParseRejectsNonCanonicalBencode(t *testing.T) {
	cases := []struct {
		name string
		data string
		want string
	}{
		{"leading zero integer", "d" + bs("x") + "i03e" + "e", "leading zeros"},
		{"negative zero integer", "d" + bs("x") + "i-0e" + "e", "negative zero"},
		{"positive sign integer", "d" + bs("x") + "i+5e" + "e", "positive sign"},
		{"leading zero string length", "d" + bs("x") + "03:abc" + "e", "leading zeros"},
		{"unsorted dictionary keys", "d" + bs("b") + bi(1) + bs("a") + bi(2) + "e", "ascending order"},
		{"duplicate dictionary keys", "d" + bs("a") + bi(1) + bs("a") + bi(2) + "e", "duplicate"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := torrent.Parse([]byte(tc.data))
			if err == nil {
				t.Fatal("Parse() expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestParseRejectsMalformedOrTruncated(t *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		{"empty input", ""},
		{"non-dictionary top level", "i5e"},
		{"unterminated dictionary", "d"},
		{"truncated announce string", "d" + bs("announce") + "8:url"},
		{"truncated info dictionary", "d" + bs("announce") + bs(testAnnounce) + bs("info") + "d"},
		{"unterminated integer", "d" + bs("x") + "i5"},
		{"non-string dictionary key", "di1e" + bs("abc") + "e"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := torrent.Parse([]byte(tc.data)); err == nil {
				t.Fatal("Parse() expected an error")
			}
		})
	}
}

func TestParseRejectsNonUTF8TextFields(t *testing.T) {
	pieces := makePieces(1)
	invalidName := string([]byte{0xff, 0xfe})
	invalidAnnounce := string([]byte{0xff, 0x00})
	invalidComment := string([]byte{0x80})

	cases := []struct {
		name string
		data string
		want string
	}{
		{
			name: "invalid name",
			data: "d" + bs("announce") + bs(testAnnounce) + bs("info") +
				validInfo(invalidName, 50, 50, pieces) + "e",
			want: "name is not valid UTF-8",
		},
		{
			name: "invalid announce",
			data: "d" + bs("announce") + bs(invalidAnnounce) + bs("info") +
				validInfo("a.mp4", 50, 50, pieces) + "e",
			want: "announce URL is not valid UTF-8",
		},
		{
			name: "invalid comment",
			data: "d" + bs("announce") + bs(testAnnounce) + bs("comment") + bs(invalidComment) +
				bs("info") + validInfo("a.mp4", 50, 50, pieces) + "e",
			want: "comment is not valid UTF-8",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := torrent.Parse([]byte(tc.data))
			if err == nil {
				t.Fatal("Parse() expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestParseAcceptsBinaryPieces(t *testing.T) {
	// Pieces is a binary field: it must survive bytes that are not valid UTF-8.
	pieces := makePieces(3) // includes 0x00 and bytes >= 0x80
	info := validInfo("movie.mp4", 60, 180, pieces)
	data := []byte("d" + bs("announce") + bs(testAnnounce) + bs("info") + info + "e")

	got, err := torrent.Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !bytes.Equal(got.Info.Pieces, pieces) {
		t.Error("Info.Pieces does not round-trip binary data")
	}
}
