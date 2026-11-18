package peer_test

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"testing"

	"github.com/neepii/torrent_player/src/peer"
)

// TestChokeMessage tests the choke message.
func TestChokeMessage(t *testing.T) {
	msg := &peer.Choke{}

	if msg.Type() != peer.MsgChoke {
		t.Errorf("Type() = %d, want %d", msg.Type(), peer.MsgChoke)
	}

	if msg.Length() != 0 {
		t.Errorf("Length() = %d, want 0", msg.Length())
	}

	data := msg.Encode()

	// Verify length prefix
	if len(data) < 4 {
		t.Fatal("data too short for length prefix")
	}

	// Length should be 1 (message ID only)
	expectedLen := uint32(1)
	actualLen := bytesToUint32(data[0:4])
	if actualLen != expectedLen {
		t.Errorf("length prefix = %d, want %d", actualLen, expectedLen)
	}

	// Message ID
	if data[4] != peer.MsgChoke {
		t.Errorf("message ID = %d, want %d", data[4], peer.MsgChoke)
	}

	// Test decoding
	decoded, _, err := peer.DecodeMessage(data)
	if err != nil {
		t.Fatalf("DecodeMessage() error = %v", err)
	}

	if _, ok := decoded.(*peer.Choke); !ok {
		t.Errorf("decoded message type = %T, want *peer.Choke", decoded)
	}
}

// TestUnchokeMessage tests the unchoke message.
func TestUnchokeMessage(t *testing.T) {
	msg := &peer.Unchoke{}

	if msg.Type() != peer.MsgUnchoke {
		t.Errorf("Type() = %d, want %d", msg.Type(), peer.MsgUnchoke)
	}

	if msg.Length() != 0 {
		t.Errorf("Length() = %d, want 0", msg.Length())
	}

	data := msg.Encode()
	decoded, _, err := peer.DecodeMessage(data)
	if err != nil {
		t.Fatalf("DecodeMessage() error = %v", err)
	}

	if _, ok := decoded.(*peer.Unchoke); !ok {
		t.Errorf("decoded message type = %T, want *peer.Unchoke", decoded)
	}
}

// TestInterestedMessage tests the interested message.
func TestInterestedMessage(t *testing.T) {
	msg := &peer.Interested{}

	if msg.Type() != peer.MsgInterested {
		t.Errorf("Type() = %d, want %d", msg.Type(), peer.MsgInterested)
	}

	data := msg.Encode()
	decoded, _, err := peer.DecodeMessage(data)
	if err != nil {
		t.Fatalf("DecodeMessage() error = %v", err)
	}

	if _, ok := decoded.(*peer.Interested); !ok {
		t.Errorf("decoded message type = %T, want *peer.Interested", decoded)
	}
}

// TestNotInterestedMessage tests the not interested message.
func TestNotInterestedMessage(t *testing.T) {
	msg := &peer.NotInterested{}

	if msg.Type() != peer.MsgNotInterested {
		t.Errorf("Type() = %d, want %d", msg.Type(), peer.MsgNotInterested)
	}

	data := msg.Encode()
	decoded, _, err := peer.DecodeMessage(data)
	if err != nil {
		t.Fatalf("DecodeMessage() error = %v", err)
	}

	if _, ok := decoded.(*peer.NotInterested); !ok {
		t.Errorf("decoded message type = %T, want *peer.NotInterested", decoded)
	}
}

// TestHaveMessage tests the have message.
func TestHaveMessage(t *testing.T) {
	pieceIndex := uint32(42)
	msg := peer.NewHave(pieceIndex)

	if msg.Type() != peer.MsgHave {
		t.Errorf("Type() = %d, want %d", msg.Type(), peer.MsgHave)
	}

	if msg.Length() != 4 {
		t.Errorf("Length() = %d, want 4", msg.Length())
	}

	data := msg.Encode()

	// Verify payload
	var gotIndex uint32
	gotIndex = bytesToUint32(data[5:])
	if gotIndex != pieceIndex {
		t.Errorf("piece index = %d, want %d", gotIndex, pieceIndex)
	}

	// Test decoding
	decoded, _, err := peer.DecodeMessage(data)
	if err != nil {
		t.Fatalf("DecodeMessage() error = %v", err)
	}

	if msg, ok := decoded.(*peer.Have); !ok {
		t.Errorf("decoded message type = %T, want *peer.Have", decoded)
	} else if msg.PieceIndex != pieceIndex {
		t.Errorf("piece index = %d, want %d", msg.PieceIndex, pieceIndex)
	}
}

// TestBitfieldMessage tests the bitfield message.
func TestBitfieldMessage(t *testing.T) {
	have := []bool{true, false, true, false, false, false, false, true}
	msg := peer.NewBitfield(have)

	if msg.Type() != peer.MsgBitfield {
		t.Errorf("Type() = %d, want %d", msg.Type(), peer.MsgBitfield)
	}

	expectedLen := (len(have) + 7) / 8
	if msg.Length() != uint32(expectedLen) {
		t.Errorf("Length() = %d, want %d", msg.Length(), expectedLen)
	}

	data := msg.Encode()

	// Verify bitmap
	// First piece (index 0) should be set: 10000000 = 0x80
	// Third piece (index 2) should be set: 00100000 = 0x20
	// Last piece (index 7) should be set: 00000001 = 0x01
	// Total: 0xA1
	if len(data) < 5+expectedLen {
		t.Fatal("data too short")
	}

	if data[5] != 0xA1 {
		t.Errorf("bitmap[0] = 0x%02x, want 0xa1", data[5])
	}

	// Test decoding
	decoded, _, err := peer.DecodeMessage(data)
	if err != nil {
		t.Fatalf("DecodeMessage() error = %v", err)
	}

	if msg, ok := decoded.(*peer.Bitfield); !ok {
		t.Errorf("decoded message type = %T, want *peer.Bitfield", decoded)
	} else if len(msg.Bitmap) != expectedLen {
		t.Errorf("bitmap length = %d, want %d", len(msg.Bitmap), expectedLen)
	}
}

// TestBitfieldFromBytes tests creating bitfield from bytes.
func TestBitfieldFromBytes(t *testing.T) {
	bitmap := []byte{0xFF, 0x00, 0x80}
	msg := peer.NewBitfieldFromBytes(bitmap)

	if msg.Type() != peer.MsgBitfield {
		t.Errorf("Type() = %d, want %d", msg.Type(), peer.MsgBitfield)
	}

	if !bytes.Equal(msg.Bitmap, bitmap) {
		t.Errorf("bitmap = %v, want %v", msg.Bitmap, bitmap)
	}
}

// TestRequestMessage tests the request message.
func TestRequestMessage(t *testing.T) {
	pieceIndex := uint32(10)
	begin := uint32(1024)
	length := uint32(16384)

	msg := peer.NewRequest(pieceIndex, begin, length)

	if msg.Type() != peer.MsgRequest {
		t.Errorf("Type() = %d, want %d", msg.Type(), peer.MsgRequest)
	}

	if msg.Length() != 12 {
		t.Errorf("Length() = %d, want 12", msg.Length())
	}

	data := msg.Encode()

	// Verify payload
	var gotPieceIndex, gotBegin, gotLength uint32
	gotPieceIndex = bytesToUint32(data[5:9])
	gotBegin = bytesToUint32(data[9:13])
	gotLength = bytesToUint32(data[13:17])

	if gotPieceIndex != pieceIndex {
		t.Errorf("piece index = %d, want %d", gotPieceIndex, pieceIndex)
	}
	if gotBegin != begin {
		t.Errorf("begin = %d, want %d", gotBegin, begin)
	}
	if gotLength != length {
		t.Errorf("length = %d, want %d", gotLength, length)
	}

	// Test decoding
	decoded, _, err := peer.DecodeMessage(data)
	if err != nil {
		t.Fatalf("DecodeMessage() error = %v", err)
	}

	if msg, ok := decoded.(*peer.Request); !ok {
		t.Errorf("decoded message type = %T, want *peer.Request", decoded)
	} else {
		if msg.PieceIndex != pieceIndex {
			t.Errorf("piece index = %d, want %d", msg.PieceIndex, pieceIndex)
		}
		if msg.Begin != begin {
			t.Errorf("begin = %d, want %d", msg.Begin, begin)
		}
		if msg.RequestLength() != length {
			t.Errorf("length = %d, want %d", msg.RequestLength(), length)
		}
	}
}

// TestPieceMessage tests the piece message.
func TestPieceMessage(t *testing.T) {
	pieceIndex := uint32(10)
	begin := uint32(0)
	data := make([]byte, 16384)
	for i := range data {
		data[i] = byte(i % 256)
	}

	msg := peer.NewPiece(pieceIndex, begin, data)

	if msg.Type() != peer.MsgPiece {
		t.Errorf("Type() = %d, want %d", msg.Type(), peer.MsgPiece)
	}

	expectedLen := uint32(8 + len(data))
	if msg.Length() != expectedLen {
		t.Errorf("Length() = %d, want %d", msg.Length(), expectedLen)
	}

	encoded := msg.Encode()

	// Verify payload
	var gotPieceIndex, gotBegin uint32
	gotPieceIndex = bytesToUint32(encoded[5:9])
	gotBegin = bytesToUint32(encoded[9:13])
	gotData := encoded[13:]

	if gotPieceIndex != pieceIndex {
		t.Errorf("piece index = %d, want %d", gotPieceIndex, pieceIndex)
	}
	if gotBegin != begin {
		t.Errorf("begin = %d, want %d", gotBegin, begin)
	}
	if !bytes.Equal(gotData, data) {
		t.Errorf("data mismatch")
	}

	// Test decoding
	decoded, _, err := peer.DecodeMessage(encoded)
	if err != nil {
		t.Fatalf("DecodeMessage() error = %v", err)
	}

	if msg, ok := decoded.(*peer.Piece); !ok {
		t.Errorf("decoded message type = %T, want *peer.Piece", decoded)
	} else {
		if msg.PieceIndex != pieceIndex {
			t.Errorf("piece index = %d, want %d", msg.PieceIndex, pieceIndex)
		}
		if msg.Begin != begin {
			t.Errorf("begin = %d, want %d", msg.Begin, begin)
		}
		if !bytes.Equal(msg.Data, data) {
			t.Errorf("data mismatch")
		}
	}
}

// TestCancelMessage tests the cancel message.
func TestCancelMessage(t *testing.T) {
	pieceIndex := uint32(10)
	begin := uint32(0)
	length := uint32(16384)

	msg := peer.NewCancel(pieceIndex, begin, length)

	if msg.Type() != peer.MsgCancel {
		t.Errorf("Type() = %d, want %d", msg.Type(), peer.MsgCancel)
	}

	if msg.Length() != 12 {
		t.Errorf("Length() = %d, want 12", msg.Length())
	}

	data := msg.Encode()

	// Verify payload
	var gotPieceIndex, gotBegin, gotLength uint32
	gotPieceIndex = bytesToUint32(data[5:9])
	gotBegin = bytesToUint32(data[9:13])
	gotLength = bytesToUint32(data[13:17])

	if gotPieceIndex != pieceIndex {
		t.Errorf("piece index = %d, want %d", gotPieceIndex, pieceIndex)
	}
	if gotBegin != begin {
		t.Errorf("begin = %d, want %d", gotBegin, begin)
	}
	if gotLength != length {
		t.Errorf("length = %d, want %d", gotLength, length)
	}

	// Test decoding
	decoded, _, err := peer.DecodeMessage(data)
	if err != nil {
		t.Fatalf("DecodeMessage() error = %v", err)
	}

	if msg, ok := decoded.(*peer.Cancel); !ok {
		t.Errorf("decoded message type = %T, want *peer.Cancel", decoded)
	} else {
		if msg.PieceIndex != pieceIndex {
			t.Errorf("piece index = %d, want %d", msg.PieceIndex, pieceIndex)
		}
		if msg.Begin != begin {
			t.Errorf("begin = %d, want %d", msg.Begin, begin)
		}
		if msg.CancelLength() != length {
			t.Errorf("length = %d, want %d", msg.CancelLength(), length)
		}
	}
}

// TestKeepAliveMessage tests the keep-alive message.
func TestKeepAliveMessage(t *testing.T) {
	// Create a keep-alive message (length = 0)
	data := make([]byte, 4) // Just the length prefix

	decoded, remaining, err := peer.DecodeMessage(data)
	if err != nil {
		t.Fatalf("DecodeMessage() error = %v", err)
	}

	if decoded != nil {
		t.Errorf("expected nil for keep-alive, got %T", decoded)
	}

	if len(remaining) != 0 {
		t.Errorf("remaining = %d bytes, want 0", len(remaining))
	}
}

// TestDecodeMessageInvalidLength tests decoding with invalid length.
func TestDecodeMessageInvalidLength(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{"too short for length", make([]byte, 3)},
		{"incomplete message", func() []byte {
			// Create a message with length 10 but only 9 bytes total
			data := make([]byte, 9)
			binary.BigEndian.PutUint32(data[0:4], 10)
			return data
		}()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := peer.DecodeMessage(tc.data)
			if err == nil {
				t.Error("expected error")
			}
		})
	}
}

// TestDecodeMessageUnknownType tests decoding with unknown message type.
func TestDecodeMessageUnknownType(t *testing.T) {
	// Create a message with unknown type
	data := make([]byte, 5)
	// Length prefix = 1
	data[0] = 0
	data[1] = 0
	data[2] = 0
	data[3] = 1
	// Unknown type = 99
	data[4] = 99

	_, _, err := peer.DecodeMessage(data)
	if err == nil {
		t.Error("expected error for unknown message type")
	}
}

// TestDecodeMessageIncompletePayload tests decoding with incomplete payload.
func TestDecodeMessageIncompletePayload(t *testing.T) {
	// Create a message with length 5 but only 3 bytes of payload
	data := make([]byte, 8)
	// Length prefix = 5
	data[0] = 0
	data[1] = 0
	data[2] = 0
	data[3] = 5
	// Message ID = 4 (have)
	data[4] = 4
	// Only 3 bytes of payload (should be 4)
	data[5] = 0
	data[6] = 0
	data[7] = 0

	_, _, err := peer.DecodeMessage(data)
	if err == nil {
		t.Error("expected error for incomplete payload")
	}
}

// TestDecoderEncoder tests the streaming decoder and encoder.
func TestDecoderEncoder(t *testing.T) {
	// Use bytes.Buffer for simplicity (synchronous)
	var buf bytes.Buffer

	encoder := peer.NewEncoder(&buf)
	decoder := peer.NewDecoder(&buf)

	// Encode some messages
	msgs := []peer.Message{
		&peer.Choke{},
		peer.NewHave(42),
		peer.NewRequest(10, 0, 16384),
	}

	for _, msg := range msgs {
		if err := encoder.Encode(msg); err != nil {
			t.Fatalf("Encode() error = %v", err)
		}
	}

	// Decode them back
	for i, expected := range msgs {
		decoded, err := decoder.Decode()
		if err != nil {
			t.Fatalf("Decode() error at index %d = %v", i, err)
		}

		// Just verify types match for now
		if decoded == nil || decoded.Type() != expected.Type() {
			t.Errorf("message %d: decoded type mismatch", i)
		}
	}
}

// TestDecodeHandshakeWithVariousLengths tests decoding handshakes with various data lengths.
func TestDecodeHandshakeWithVariousLengths(t *testing.T) {
	infoHash := sha1.Sum([]byte("test"))
	var peerID [20]byte

	data := make([]byte, peer.HandshakeLen)
	data[0] = peer.ProtocolNameLen
	copy(data[1:1+peer.ProtocolNameLen], []byte(peer.ProtocolName))
	copy(data[1+peer.ProtocolNameLen+peer.ReservedLen:], infoHash[:])
	copy(data[1+peer.ProtocolNameLen+peer.ReservedLen+peer.InfoHashLen:], peerID[:])

	cases := []struct {
		name     string
		truncate int
		wantErr  bool
	}{
		{"full length", 0, false},
		{"one byte short", 1, true},
		{"10 bytes short", 10, true},
		{"50 bytes short", 50, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var test []byte
			if tc.truncate == 0 {
				test = data
			} else {
				test = data[:len(data)-tc.truncate]
			}

			_, err := peer.DecodeHandshake(test)
			if tc.wantErr && err == nil {
				t.Error("expected error, got nil")
			} else if !tc.wantErr && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

// bytesToUint32 converts 4 bytes to uint32 (big-endian).
func bytesToUint32(b []byte) uint32 {
	return (uint32(b[0]) << 24) |
		(uint32(b[1]) << 16) |
		(uint32(b[2]) << 8) |
		uint32(b[3])
}
