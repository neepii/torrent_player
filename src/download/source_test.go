package download_test

import (
	"bytes"
	"crypto/sha1"
	"io"
	"net"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	"github.com/neepii/torrent_player/src/download"
	"github.com/neepii/torrent_player/src/peer"
	"github.com/neepii/torrent_player/src/pipeline"
	"github.com/neepii/torrent_player/src/torrent"
)

func metadata(data []byte, piece int) *torrent.TorrentData {
	m := &torrent.TorrentData{Info: torrent.TorrentInfoDict{Length: int64(len(data)), PieceLength: int64(piece)}}
	for i := 0; i < len(data); i += piece {
		end := i + piece
		if end > len(data) {
			end = len(data)
		}
		sum := sha1.Sum(data[i:end])
		m.Info.Pieces = append(m.Info.Pieces, sum[:]...)
	}
	return m
}

// startPeer is a controlled loopback seeder. mode controls its first response.
func startPeer(t *testing.T, data []byte, piece int, mode string) (pipeline.PeerData, *atomic.Int32) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	var attempts atomic.Int32
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			n := attempts.Add(1)
			go func() {
				defer c.Close()
				c.SetDeadline(time.Now().Add(3 * time.Second))
				buf := make([]byte, peer.HandshakeLen)
				if _, err := io.ReadFull(c, buf); err != nil {
					return
				}
				hs, err := peer.DecodeHandshake(buf)
				if err != nil {
					return
				}
				if _, err = c.Write(peer.NewHandshake(hs.InfoHash, [20]byte{}).Encode()); err != nil {
					return
				}
				dec, enc := peer.NewDecoder(c), peer.NewEncoder(c)
				msg, err := dec.Decode()
				if err != nil {
					return
				}
				if _, ok := msg.(*peer.Interested); !ok {
					return
				}
				if err = enc.Encode(&peer.Unchoke{}); err != nil {
					return
				}
				for {
					msg, err = dec.Decode()
					if err != nil {
						return
					}
					req, ok := msg.(*peer.Request)
					if !ok {
						return
					}
					if mode == "drop" || (mode == "drop-first" && n == 1) {
						return
					}
					start := int(req.PieceIndex)*piece + int(req.Begin)
					end := start + int(req.RequestLength())
					if start < 0 || end > len(data) {
						return
					}
					block := append([]byte(nil), data[start:end]...)
					if mode == "corrupt" || (mode == "corrupt-first" && n == 1) {
						block[0] ^= 1
					}
					if enc.Encode(peer.NewPiece(req.PieceIndex, req.Begin, block)) != nil {
						return
					}
				}
			}()
		}
	}()
	return pipeline.PeerData{AddrPort: netip.MustParseAddrPort(ln.Addr().String())}, &attempts
}

func TestSequentialVerifiedPiecesAndFinalLength(t *testing.T) {
	data := bytes.Repeat([]byte("abcdef"), 10000)
	const size = 32768
	p, _ := startPeer(t, data, size, "")
	s, err := download.New(metadata(data, size), []pipeline.PeerData{p}, [20]byte{}, download.Options{Timeout: time.Second, Attempts: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var got []byte
	for {
		b, err := s.ReadPiece()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, b...)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("incorrect ordered output")
	}
}

func TestRetriesAndNeverReturnsCorruption(t *testing.T) {
	for _, mode := range []string{"corrupt-first", "drop-first", "corrupt", "drop"} {
		t.Run(mode, func(t *testing.T) {
			data := []byte("verified piece")
			p, attempts := startPeer(t, data, 32, mode)
			s, err := download.New(metadata(data, 32), []pipeline.PeerData{p}, [20]byte{}, download.Options{Timeout: time.Second, Attempts: 2})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			got, err := s.ReadPiece()
			fail := mode == "corrupt" || mode == "drop"
			if fail {
				if err == nil || got != nil {
					t.Fatalf("expected failure with no data, got %q %v", got, err)
				}
			} else if err != nil || !bytes.Equal(got, data) {
				t.Fatalf("got %q %v", got, err)
			}
			if attempts.Load() != 2 {
				t.Fatalf("attempts = %d", attempts.Load())
			}
		})
	}
}
