package peer

import (
	"encoding/binary"
	"fmt"
	"io"
)

// KeepAlive is a special message with ID = -1 (0xFFFFFFFF).
const KeepAlive = 0xFFFFFFFF

// Message is the interface implemented by all BitTorrent wire messages.
type Message interface {
	// Type returns the message type ID.
	Type() uint8

	// Encode encodes the message to bytes.
	Encode() []byte

	// Length returns the payload length (excluding the message ID and length prefix).
	Length() uint32
}

// Choke is a peer choke message.
// Message ID: 0
// Description: The peer will not serve requests for pieces for some time.
type Choke struct{}

func (m *Choke) Type() uint8    { return MsgChoke }
func (m *Choke) Length() uint32 { return 0 }
func (m *Choke) Encode() []byte {
	return encodeMessage(MsgChoke, nil)
}

// Unchoke is a peer unchoke message.
// Message ID: 1
// Description: The peer will serve requests for pieces for some time.
type Unchoke struct{}

func (m *Unchoke) Type() uint8    { return MsgUnchoke }
func (m *Unchoke) Length() uint32 { return 0 }
func (m *Unchoke) Encode() []byte {
	return encodeMessage(MsgUnchoke, nil)
}

// Interested is a peer interested message.
// Message ID: 2
// Description: The peer is interested in downloading pieces.
type Interested struct{}

func (m *Interested) Type() uint8    { return MsgInterested }
func (m *Interested) Length() uint32 { return 0 }
func (m *Interested) Encode() []byte {
	return encodeMessage(MsgInterested, nil)
}

// NotInterested is a peer not interested message.
// Message ID: 3
// Description: The peer is not interested in downloading pieces.
type NotInterested struct{}

func (m *NotInterested) Type() uint8    { return MsgNotInterested }
func (m *NotInterested) Length() uint32 { return 0 }
func (m *NotInterested) Encode() []byte {
	return encodeMessage(MsgNotInterested, nil)
}

// Have indicates that the peer has downloaded a specific piece.
// Message ID: 4
// Payload: <piece index>
type Have struct {
	PieceIndex uint32
}

func (m *Have) Type() uint8    { return MsgHave }
func (m *Have) Length() uint32 { return 4 }
func (m *Have) Encode() []byte {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, m.PieceIndex)
	return encodeMessage(MsgHave, payload)
}

// NewHave creates a new Have message.
func NewHave(pieceIndex uint32) *Have {
	return &Have{PieceIndex: pieceIndex}
}

// Bitfield is a bitfield message indicating which pieces the peer has.
// Message ID: 5
// Payload: <bitfield>
// Each bit represents whether the peer has the corresponding piece.
type Bitfield struct {
	Bitmap []byte
}

func (m *Bitfield) Type() uint8    { return MsgBitfield }
func (m *Bitfield) Length() uint32 { return uint32(len(m.Bitmap)) }
func (m *Bitfield) Encode() []byte {
	return encodeMessage(MsgBitfield, m.Bitmap)
}

// NewBitfield creates a new Bitfield from a slice of booleans.
func NewBitfield(have []bool) *Bitfield {
	bitfield := make([]byte, (len(have)+7)/8)
	for i, h := range have {
		if h {
			bitfield[i/8] |= 1 << (7 - (i % 8))
		}
	}
	return &Bitfield{Bitmap: bitfield}
}

// NewBitfieldFromBytes creates a Bitfield from raw bytes.
func NewBitfieldFromBytes(bitmap []byte) *Bitfield {
	return &Bitfield{Bitmap: bitmap}
}

// Request is a piece request message.
// Message ID: 6
// Payload: <piece index><begin offset><length>
type Request struct {
	PieceIndex uint32
	Begin      uint32
	length     uint32
}

func (m *Request) Type() uint8    { return MsgRequest }
func (m *Request) Length() uint32 { return 12 }
func (m *Request) Encode() []byte {
	payload := make([]byte, 12)
	binary.BigEndian.PutUint32(payload[0:4], m.PieceIndex)
	binary.BigEndian.PutUint32(payload[4:8], m.Begin)
	binary.BigEndian.PutUint32(payload[8:12], m.length)
	return encodeMessage(MsgRequest, payload)
}

// RequestLength returns the request length.
func (m *Request) RequestLength() uint32 { return m.length }

// NewRequest creates a new Request message.
func NewRequest(pieceIndex, begin, length uint32) *Request {
	return &Request{
		PieceIndex: pieceIndex,
		Begin:      begin,
		length:     length,
	}
}

// Piece is a piece data message.
// Message ID: 7
// Payload: <piece index><begin offset><block data>
type Piece struct {
	PieceIndex uint32
	Begin      uint32
	Data       []byte
}

func (m *Piece) Type() uint8    { return MsgPiece }
func (m *Piece) Length() uint32 { return 8 + uint32(len(m.Data)) }
func (m *Piece) Encode() []byte {
	payload := make([]byte, 8+len(m.Data))
	binary.BigEndian.PutUint32(payload[0:4], m.PieceIndex)
	binary.BigEndian.PutUint32(payload[4:8], m.Begin)
	copy(payload[8:], m.Data)
	return encodeMessage(MsgPiece, payload)
}

// NewPiece creates a new Piece message.
func NewPiece(pieceIndex, begin uint32, data []byte) *Piece {
	return &Piece{
		PieceIndex: pieceIndex,
		Begin:      begin,
		Data:       data,
	}
}

// Cancel is a piece request cancellation message.
// Message ID: 8
// Payload: <piece index><begin offset><length>
type Cancel struct {
	PieceIndex uint32
	Begin      uint32
	length     uint32
}

func (m *Cancel) Type() uint8    { return MsgCancel }
func (m *Cancel) Length() uint32 { return 12 }
func (m *Cancel) Encode() []byte {
	payload := make([]byte, 12)
	binary.BigEndian.PutUint32(payload[0:4], m.PieceIndex)
	binary.BigEndian.PutUint32(payload[4:8], m.Begin)
	binary.BigEndian.PutUint32(payload[8:12], m.length)
	return encodeMessage(MsgCancel, payload)
}

// CancelLength returns the cancel length.
func (m *Cancel) CancelLength() uint32 { return m.length }

// NewCancel creates a new Cancel message.
func NewCancel(pieceIndex, begin, length uint32) *Cancel {
	return &Cancel{
		PieceIndex: pieceIndex,
		Begin:      begin,
		length:     length,
	}
}

// encodeMessage creates a complete wire message with length prefix.
func encodeMessage(id uint8, payload []byte) []byte {
	msgLen := uint32(len(payload) + 1) // +1 for message ID
	data := make([]byte, 4+1+len(payload))

	// Length prefix (4 bytes, big-endian)
	binary.BigEndian.PutUint32(data[0:4], msgLen)

	// Message ID
	data[4] = id

	// Payload
	copy(data[5:], payload)

	return data
}

// DecodeMessage parses a complete message from bytes.
// Returns the message and any remaining bytes.
func DecodeMessage(data []byte) (Message, []byte, error) {
	if len(data) < 4 {
		return nil, data, fmt.Errorf("message: data too short for length prefix")
	}

	msgLen := binary.BigEndian.Uint32(data[0:4])

	if msgLen == 0 {
		// Keep-alive message
		return nil, data[4:], nil
	}

	if len(data) < 4+int(msgLen) {
		return nil, data, fmt.Errorf("message: incomplete message, need %d bytes, have %d", 4+msgLen, len(data))
	}

	msgData := data[4 : 4+int(msgLen)]
	messageID := msgData[0]
	payload := msgData[1:]

	var msg Message
	var err error

	if messageID <= MsgNotInterested && len(payload) != 0 {
		return nil, nil, fmt.Errorf("message: unexpected payload")
	}
	switch messageID {
	case MsgChoke:
		msg = &Choke{}
	case MsgUnchoke:
		msg = &Unchoke{}
	case MsgInterested:
		msg = &Interested{}
	case MsgNotInterested:
		msg = &NotInterested{}
	case MsgHave:
		msg, err = decodeHave(payload)
	case MsgBitfield:
		msg = &Bitfield{Bitmap: payload}
	case MsgRequest:
		msg, err = decodeRequest(payload)
	case MsgPiece:
		msg, err = decodePiece(payload)
	case MsgCancel:
		msg, err = decodeCancel(payload)
	default:
		err = fmt.Errorf("message: unknown message type %d", messageID)
	}

	if err != nil {
		return nil, nil, err
	}

	return msg, data[4+int(msgLen):], nil
}

func decodeHave(payload []byte) (Message, error) {
	if len(payload) != 4 {
		return nil, fmt.Errorf("have: payload too short")
	}
	return NewHave(binary.BigEndian.Uint32(payload)), nil
}

func decodeRequest(payload []byte) (Message, error) {
	if len(payload) != 12 {
		return nil, fmt.Errorf("request: payload too short")
	}
	return &Request{
		PieceIndex: binary.BigEndian.Uint32(payload[0:4]),
		Begin:      binary.BigEndian.Uint32(payload[4:8]),
		length:     binary.BigEndian.Uint32(payload[8:12]),
	}, nil
}

func decodePiece(payload []byte) (Message, error) {
	if len(payload) < 8 {
		return nil, fmt.Errorf("piece: payload too short for header")
	}
	return &Piece{
		PieceIndex: binary.BigEndian.Uint32(payload[0:4]),
		Begin:      binary.BigEndian.Uint32(payload[4:8]),
		Data:       payload[8:],
	}, nil
}

func decodeCancel(payload []byte) (Message, error) {
	if len(payload) != 12 {
		return nil, fmt.Errorf("cancel: payload too short")
	}
	return &Cancel{
		PieceIndex: binary.BigEndian.Uint32(payload[0:4]),
		Begin:      binary.BigEndian.Uint32(payload[4:8]),
		length:     binary.BigEndian.Uint32(payload[8:12]),
	}, nil
}

// Decoder is a streaming decoder for BitTorrent wire messages.
type Decoder struct {
	r io.Reader
}

// NewDecoder creates a new message decoder.
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r: r}
}

// Decode reads and decodes the next message from the stream.
func (d *Decoder) Decode() (Message, error) {
	// Read length prefix
	lengthBuf := make([]byte, 4)
	if _, err := io.ReadFull(d.r, lengthBuf); err != nil {
		return nil, err
	}

	msgLen := binary.BigEndian.Uint32(lengthBuf)

	if msgLen == 0 {
		// Keep-alive
		return nil, nil
	}

	// Bound memory allocated for untrusted peers.
	if msgLen > 1<<20 {
		return nil, fmt.Errorf("message: length %d exceeds limit", msgLen)
	}
	// Read message body
	msgBuf := make([]byte, msgLen)
	if _, err := io.ReadFull(d.r, msgBuf); err != nil {
		return nil, err
	}

	msg, _, err := DecodeMessage(append(lengthBuf, msgBuf...))
	return msg, err
}

// Encoder is a streaming encoder for BitTorrent wire messages.
type Encoder struct {
	w io.Writer
}

// NewEncoder creates a new message encoder.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w}
}

// Encode writes a message to the stream.
func (e *Encoder) Encode(msg Message) error {
	data := msg.Encode()
	n, err := e.w.Write(data)
	if err == nil && n != len(data) {
		return io.ErrShortWrite
	}
	return err
}
