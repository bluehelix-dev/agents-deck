package link

import (
	"bytes"
	"math/rand"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/brushknight/agents-deck/backend/internal/link/linkpb"
)

func TestCRC16(t *testing.T) {
	// The CRC-16/CCITT-FALSE check value.
	if got := CRC16([]byte("123456789")); got != 0x29B1 {
		t.Fatalf("crc %04x", got)
	}
}

func TestCOBS(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	cases := [][]byte{{}, {0}, {0, 0}, {1}, bytes.Repeat([]byte{7}, 254), bytes.Repeat([]byte{7}, 255), bytes.Repeat([]byte{7}, 600)}
	for range 200 {
		b := make([]byte, r.Intn(1200))
		r.Read(b)
		for i := range b {
			if r.Intn(5) == 0 {
				b[i] = 0
			}
		}
		cases = append(cases, b)
	}
	for _, c := range cases {
		enc := COBSEncode(c)
		if bytes.IndexByte(enc, 0) >= 0 {
			t.Fatalf("zero in encoding of %d bytes", len(c))
		}
		dec, err := COBSDecode(enc)
		if err != nil || !bytes.Equal(dec, c) {
			t.Fatalf("round trip of %d bytes: %v", len(c), err)
		}
	}
}

func hello(id string) *linkpb.Frame {
	return &linkpb.Frame{Seq: 1, Body: &linkpb.Frame_Hello{Hello: &linkpb.Hello{Protocol: 1, BoardId: id, Type: "oled-tile", Firmware: "1.0.0"}}}
}

func TestDecoderBinaryJSONAndNoise(t *testing.T) {
	bin, _ := Encode(hello("24:6F:28:3F:A9:C1"))
	js, _ := EncodeJSON(hello("json-board"))
	// A frame whose payload contains '\n' and zero bytes.
	tricky := &linkpb.Frame{Seq: 10, Body: &linkpb.Frame_Answer{Answer: &linkpb.Answer{AgentId: "a\nb", PromptId: "p\x00", Key: "1"}}}
	trickyBin, _ := Encode(tricky)

	var stream []byte
	stream = append(stream, "ets Jun  8 2016 00:22:57\r\nrst:0x1 (POWERON_RESET),boot:0x13\r\n"...) // ESP32 boot log
	stream = append(stream, bin...)
	stream = append(stream, "I (312) app: started\n"...)
	stream = append(stream, js...)
	stream = append(stream, trickyBin...)
	stream = append(stream, 0x55, 0x66) // garbage without delimiter…
	stream = append(stream, bin...)     // …then a frame: the garbage is skipped

	// Feed byte by byte and in one go: same result.
	for _, step := range []int{1, 7, len(stream)} {
		d := &Decoder{}
		var got []Decoded
		for i := 0; i < len(stream); i += step {
			got = append(got, d.Feed(stream[i:min(i+step, len(stream))])...)
		}
		if len(got) != 4 {
			t.Fatalf("step %d: %d frames, bad %d", step, len(got), d.Bad)
		}
		if got[0].Mode != Binary || got[0].Frame.GetHello().GetBoardId() != "24:6F:28:3F:A9:C1" {
			t.Errorf("step %d: first %+v", step, got[0])
		}
		if got[1].Mode != JSON || got[1].Frame.GetHello().GetBoardId() != "json-board" {
			t.Errorf("step %d: json %+v", step, got[1])
		}
		if !proto.Equal(got[2].Frame, tricky) {
			t.Errorf("step %d: tricky frame %v", step, got[2].Frame)
		}
		if got[3].Frame.GetHello() == nil {
			t.Errorf("step %d: frame after garbage lost", step)
		}
	}
}

func TestDecoderRejectsCorruption(t *testing.T) {
	bin, _ := Encode(hello("x"))
	bad := append([]byte{}, bin...)
	bad[len(bad)/2] ^= 0x40 // flip a bit
	d := &Decoder{}
	if got := d.Feed(bad); len(got) != 0 || d.Bad != 1 {
		t.Fatalf("corrupt frame: %d frames, bad %d", len(got), d.Bad)
	}
	if got := d.Feed(bin); len(got) != 1 {
		t.Fatal("decoder didn't recover after a corrupt frame")
	}
	if got := d.Feed([]byte("{\"seq\": oops}\n")); len(got) != 0 || d.Bad != 2 {
		t.Fatalf("bad json: %d frames, bad %d", len(got), d.Bad)
	}
}

// A binary frame whose COBS code byte is 0x0A starts with '\n': it is still a
// frame, not the end of an empty text line, whole or byte by byte, after a
// JSON line too.
func TestDecoderCodeByteNewline(t *testing.T) {
	f := &linkpb.Frame{Seq: 119, Body: &linkpb.Frame_Subscribe{Subscribe: &linkpb.Subscribe{Fields: 1}}}
	w, err := Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	if w[0] != '\n' {
		t.Fatalf("the test needs a frame whose code byte is 0x0A, got % x", w)
	}
	j, err := EncodeJSON(&linkpb.Frame{Seq: 7, Body: &linkpb.Frame_Ping{Ping: &linkpb.Ping{TimeMs: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	var whole Decoder
	if got := whole.Feed(append(append([]byte{}, j...), w...)); len(got) != 2 || got[1].Frame.Seq != 119 || got[1].Mode != Binary {
		t.Fatalf("after a JSON line: %+v", got)
	}
	var bytewise Decoder
	var got []Decoded
	for _, c := range w {
		got = append(got, bytewise.Feed([]byte{c})...)
	}
	if len(got) != 1 || got[0].Frame.Seq != 119 || bytewise.Bad != 0 {
		t.Fatalf("byte by byte: %+v (bad %d)", got, bytewise.Bad)
	}
}
