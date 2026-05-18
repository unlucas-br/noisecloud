package weave

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
)

type FrameType byte

const (
	FrameTypeData   FrameType = 0
	FrameTypeRescue FrameType = 1
)

// Header is the fixed 32-byte metadata block stored before every frame payload.
type Header struct {
	FrameType      FrameType
	BlockSize      uint16
	FrameIndex     uint32
	TotalFrames    uint32
	DataSize       uint16
	RescueIndex    uint16
	TotalDataBytes uint64
	CRC32          uint32
}

// Frame is one systematic Weave unit. A transport may store it directly or
// map its marshalled bytes into pixels, packets, symbols, or another medium.
type Frame struct {
	Header  Header
	Payload []byte
}

// MarshalBinary serializes the frame as [32-byte header][payload].
// CRC32 and DataSize are derived from Payload.
func (f Frame) MarshalBinary() ([]byte, error) {
	if len(f.Payload) > int(^uint16(0)) {
		return nil, fmt.Errorf("%w: payload too large for frame header", ErrInvalidConfig)
	}
	h := f.Header
	h.DataSize = uint16(len(f.Payload))
	h.CRC32 = crc32.ChecksumIEEE(f.Payload)

	out := make([]byte, HeaderSize+len(f.Payload))
	copy(out[0:4], []byte(Magic))
	out[4] = Version
	out[5] = byte(h.FrameType)
	binary.BigEndian.PutUint16(out[6:8], h.BlockSize)
	binary.BigEndian.PutUint32(out[8:12], h.FrameIndex)
	binary.BigEndian.PutUint32(out[12:16], h.TotalFrames)
	binary.BigEndian.PutUint16(out[16:18], h.DataSize)
	binary.BigEndian.PutUint16(out[18:20], h.RescueIndex)
	binary.BigEndian.PutUint64(out[20:28], h.TotalDataBytes)
	binary.BigEndian.PutUint32(out[28:32], h.CRC32)
	copy(out[HeaderSize:], f.Payload)
	return out, nil
}

// ParseFrame parses a marshalled frame and ignores trailing transport padding.
func ParseFrame(packet []byte) (Frame, error) {
	var f Frame
	if len(packet) < HeaderSize {
		return f, fmt.Errorf("weave packet too small")
	}
	if string(packet[0:4]) != Magic {
		return f, fmt.Errorf("invalid weave magic: %v", packet[0:4])
	}
	if packet[4] != Version {
		return f, fmt.Errorf("unsupported weave version: %d", packet[4])
	}

	h := Header{
		FrameType:      FrameType(packet[5]),
		BlockSize:      binary.BigEndian.Uint16(packet[6:8]),
		FrameIndex:     binary.BigEndian.Uint32(packet[8:12]),
		TotalFrames:    binary.BigEndian.Uint32(packet[12:16]),
		DataSize:       binary.BigEndian.Uint16(packet[16:18]),
		RescueIndex:    binary.BigEndian.Uint16(packet[18:20]),
		TotalDataBytes: binary.BigEndian.Uint64(packet[20:28]),
		CRC32:          binary.BigEndian.Uint32(packet[28:32]),
	}
	if h.FrameType != FrameTypeData && h.FrameType != FrameTypeRescue {
		return f, fmt.Errorf("invalid weave frame type: %d", h.FrameType)
	}
	if HeaderSize+int(h.DataSize) > len(packet) {
		return f, fmt.Errorf("weave payload size exceeds packet length")
	}

	payload := make([]byte, h.DataSize)
	copy(payload, packet[HeaderSize:HeaderSize+int(h.DataSize)])
	if crc32.ChecksumIEEE(payload) != h.CRC32 {
		return f, fmt.Errorf("weave crc mismatch")
	}

	return Frame{Header: h, Payload: payload}, nil
}
