// Package link is Deck Link: the agents deck over USB serial for ESP32 boards
// (proto/agentsdeck/link/v1/link.proto, docs/link/protocol.md).
//
// On the wire each message is a Frame, encoded as
//
//	COBS( 0xDE 0xC1 | version 0x01 | protobuf bytes | CRC-16/CCITT-FALSE, big-endian ) 0x00
//
// COBS keeps zero bytes out of the frame, so a reader resynchronises at the
// next 0x00 after any noise (an ESP32 prints its boot log on the same port).
// Legacy mode: one JSON Frame per line (protobuf's JSON mapping), detected per
// line by a leading '{'.
package link

import (
	"bytes"
	"errors"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/brushknight/agents-deck/backend/internal/link/linkpb"
)

const (
	magic0, magic1 = 0xDE, 0xC1
	wireVersion    = 1
	// MaxFrame is the largest encoded frame (before COBS) the deck sends or
	// accepts, unless a board asks for less in Hello.max_frame.
	MaxFrame = 4096
	// MinFrame is the smallest Hello.max_frame the deck honours.
	MinFrame = 512
)

// CRC16 is CRC-16/CCITT-FALSE: poly 0x1021, init 0xFFFF, no reflection, no xor.
func CRC16(b []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, c := range b {
		crc ^= uint16(c) << 8
		for range 8 {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// COBSEncode encodes b so it contains no zero byte (the delimiter is not added).
func COBSEncode(b []byte) []byte {
	out := make([]byte, 1, len(b)+len(b)/254+2)
	code, codeAt := byte(1), 0
	for _, c := range b {
		if c == 0 {
			out[codeAt] = code
			codeAt, code = len(out), 1
			out = append(out, 0)
			continue
		}
		out = append(out, c)
		code++
		if code == 0xFF {
			out[codeAt] = code
			codeAt, code = len(out), 1
			out = append(out, 0)
		}
	}
	out[codeAt] = code
	return out
}

// COBSDecode reverses COBSEncode.
func COBSDecode(b []byte) ([]byte, error) {
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); {
		code := int(b[i])
		if code == 0 || i+code > len(b) {
			return nil, errors.New("cobs: bad code")
		}
		out = append(out, b[i+1:i+code]...)
		i += code
		if code < 0xFF && i < len(b) {
			out = append(out, 0)
		}
	}
	return out, nil
}

// Encode turns a Frame into wire bytes, delimiter included.
func Encode(f *linkpb.Frame) ([]byte, error) {
	pb, err := proto.Marshal(f)
	if err != nil {
		return nil, err
	}
	raw := make([]byte, 0, len(pb)+5)
	raw = append(raw, magic0, magic1, wireVersion)
	raw = append(raw, pb...)
	crc := CRC16(raw)
	raw = append(raw, byte(crc>>8), byte(crc))
	return append(COBSEncode(raw), 0), nil
}

// EncodedSize is the frame's size before COBS (what MaxFrame limits).
func EncodedSize(f *linkpb.Frame) int { return proto.Size(f) + 5 }

var jsonOut = protojson.MarshalOptions{UseProtoNames: false}
var jsonIn = protojson.UnmarshalOptions{DiscardUnknown: true}

// EncodeJSON turns a Frame into one legacy JSON line, newline included.
func EncodeJSON(f *linkpb.Frame) ([]byte, error) {
	b, err := jsonOut.Marshal(f)
	if err != nil {
		return nil, err
	}
	// protojson's output may contain random spaces; keep it on one line.
	var c bytes.Buffer
	if err := compactJSON(&c, b); err != nil {
		return nil, err
	}
	return append(c.Bytes(), '\n'), nil
}

var errNoFrame = errors.New("not a frame")

// decodeBinary checks magic, version and CRC of a COBS-decoded chunk.
func decodeBinary(chunk []byte) (*linkpb.Frame, error) {
	raw, err := COBSDecode(chunk)
	if err != nil || len(raw) < 5 || raw[0] != magic0 || raw[1] != magic1 {
		return nil, errNoFrame
	}
	if raw[2] != wireVersion {
		return nil, errors.New("unsupported wire version")
	}
	body, sum := raw[:len(raw)-2], raw[len(raw)-2:]
	if CRC16(body) != uint16(sum[0])<<8|uint16(sum[1]) {
		return nil, errors.New("crc mismatch")
	}
	f := &linkpb.Frame{}
	if err := proto.Unmarshal(body[3:], f); err != nil {
		return nil, err
	}
	return f, nil
}

// Decoder splits a byte stream into frames: binary (zero-delimited) and
// legacy JSON lines; anything else (a boot log) is noise and skipped.
//
// The magic 0xDE 0xC1 never occurs in UTF-8 text (0xC1 isn't a continuation
// byte), so a segment holding it is a binary frame, even if it contains '\n'.
type Decoder struct {
	buf []byte
	// Bad counts chunks that looked like frames but failed (CRC, JSON).
	Bad int
}

// Mode says how a frame arrived.
type Mode int

const (
	Binary Mode = iota
	JSON
)

// Decoded is one frame out of the stream.
type Decoded struct {
	Frame *linkpb.Frame
	Mode  Mode
}

var magicSeq = []byte{magic0, magic1, wireVersion}

// Feed adds bytes and returns the complete frames they finish.
func (d *Decoder) Feed(b []byte) []Decoded {
	d.buf = append(d.buf, b...)
	var out []Decoded
	for {
		i := bytes.IndexAny(d.buf, "\x00\n")
		if i < 0 {
			if len(d.buf) > 2*MaxFrame+64 { // runaway noise: drop it
				d.buf = d.buf[:0]
			}
			return out
		}
		if d.buf[i] == '\n' {
			// A '\n' right before the magic is a frame's COBS code byte
			// (0x0A), not the end of a line: the frame runs to the next zero.
			rest := d.buf[i+1:]
			if len(rest) < len(magicSeq) && bytes.HasPrefix(magicSeq, rest) && (len(rest) > 0 || i == 0) {
				return out // can't tell yet: wait for the next bytes
			}
			if bytes.HasPrefix(rest, magicSeq) {
				k := bytes.IndexByte(rest, 0)
				if k < 0 {
					if len(d.buf) > 2*MaxFrame+64 {
						d.buf = d.buf[:0]
					}
					return out
				}
				chunk := d.buf[i : i+1+k] // what came before it is noise
				d.buf = d.buf[i+1+k+1:]
				if f, ok := d.binary(chunk); ok {
					out = append(out, Decoded{f, Binary})
				}
				continue
			}
		}
		if d.buf[i] == '\n' && bytes.Contains(d.buf[:i], magicSeq) {
			// A binary frame with '\n' inside: it ends at the next zero.
			k := bytes.IndexByte(d.buf, 0)
			if k < 0 {
				if len(d.buf) > 2*MaxFrame+64 {
					d.buf = d.buf[:0]
				}
				return out
			}
			i = k
		}
		chunk := d.buf[:i]
		zero := d.buf[i] == 0
		d.buf = d.buf[i+1:]
		if !zero {
			line := bytes.TrimSpace(chunk)
			if len(line) > 0 && line[0] == '{' {
				f := &linkpb.Frame{}
				if err := jsonIn.Unmarshal(line, f); err == nil {
					out = append(out, Decoded{f, JSON})
				} else {
					d.Bad++
				}
			}
			continue // otherwise a text line: noise
		}
		if f, ok := d.binary(chunk); ok {
			out = append(out, Decoded{f, Binary})
		}
	}
}

// binary decodes the frame in a zero-terminated chunk, skipping any noise in
// front of it (its COBS code byte sits just before the magic).
func (d *Decoder) binary(chunk []byte) (*linkpb.Frame, bool) {
	tried := false
	for at := 0; ; {
		j := bytes.Index(chunk[at:], magicSeq)
		if j < 0 {
			break
		}
		start := at + j - 1
		at += j + 1
		if start < 0 {
			continue
		}
		tried = true
		if f, err := decodeBinary(chunk[start:]); err == nil {
			return f, true
		}
	}
	if tried {
		d.Bad++
	}
	return nil, false
}
