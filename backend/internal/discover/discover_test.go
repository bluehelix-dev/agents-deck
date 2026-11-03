package discover

import (
	"encoding/json"
	"net"
	"sync"
	"testing"
	"time"
)

func TestAnswersTheQuery(t *testing.T) {
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	r := &responder{
		info:  Info{Name: "deck-pc", Port: 7341, CA: "sha256:AA", Server: func() string { return "sha256:BB" }, Version: "0.0.0-test"},
		logf:  t.Logf,
		conns: map[string]*net.UDPConn{},
		told:  map[string]bool{},
	}
	// The query came in on the interface with 192.0.2.10; the computer also has a VPN address.
	go r.serveConn(c, func() []string { return []string{"192.0.2.10"} }, func() []string { return []string{"192.0.2.10", "198.51.100.7"} })

	q, err := net.DialUDP("udp4", nil, c.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	_ = q.SetDeadline(time.Now().Add(3 * time.Second))

	// Something else on the port gets no answer.
	if _, err := q.Write([]byte("hello?")); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Write([]byte(Query + "\n")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1024)
	n, err := q.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	var rep Reply
	if err := json.Unmarshal(buf[:n], &rep); err != nil {
		t.Fatalf("%v: %s", err, buf[:n])
	}
	if rep.Deck != "deck-pc" || rep.CA != "sha256:AA" || rep.Server != "sha256:BB" || rep.Version != "0.0.0-test" {
		t.Fatalf("reply: %+v", rep)
	}
	want := []string{"https://192.0.2.10:7341", "https://198.51.100.7:7341"}
	if len(rep.URLs) != len(want) {
		t.Fatalf("urls: %v", rep.URLs)
	}
	for i := range want {
		if rep.URLs[i] != want[i] {
			t.Fatalf("urls: %v", rep.URLs)
		}
	}
	// The "hello?" got nothing: the next read times out.
	_ = q.SetDeadline(time.Now().Add(300 * time.Millisecond))
	if _, err := q.Read(buf); err == nil {
		t.Fatal("answered something that was not the query")
	}
}

func TestRateLimit(t *testing.T) {
	r := &responder{told: map[string]bool{}}
	for i := 0; i < maxPerSecond; i++ {
		if !r.allow() {
			t.Fatalf("refused answer %d", i+1)
		}
	}
	if r.allow() {
		t.Fatal("answered past the limit")
	}
}

func TestIsQuery(t *testing.T) {
	for _, ok := range []string{Query, Query + "\n", Query + "\r\n", Query + " \x00"} {
		if !isQuery([]byte(ok)) {
			t.Fatalf("%q should be the query", ok)
		}
	}
	for _, no := range []string{"", "agents-deck", Query + "x", " " + Query} {
		if isQuery([]byte(no)) {
			t.Fatalf("%q should not be the query", no)
		}
	}
}

// An interface that keeps its name but gets a new address (a new DHCP lease,
// another network) answers with the new one first, not the one it had when
// its socket was opened.
func TestAnswersWithTheCurrentAddress(t *testing.T) {
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	r := &responder{info: Info{Name: "deck-pc", Port: 7341, Server: func() string { return "" }}, logf: t.Logf, told: map[string]bool{}}
	var mu sync.Mutex
	addr := "192.0.2.10"
	go r.serveConn(c, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return []string{addr}
	}, func() []string { return nil })
	q, err := net.DialUDP("udp4", nil, c.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	ask := func() string {
		_ = q.SetDeadline(time.Now().Add(3 * time.Second))
		if _, err := q.Write([]byte(Query)); err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 1024)
		n, err := q.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		var rep Reply
		if err := json.Unmarshal(buf[:n], &rep); err != nil || len(rep.URLs) == 0 {
			t.Fatalf("%v: %s", err, buf[:n])
		}
		return rep.URLs[0]
	}
	if got := ask(); got != "https://192.0.2.10:7341" {
		t.Fatalf("first: %s", got)
	}
	mu.Lock()
	addr = "192.0.2.77"
	mu.Unlock()
	if got := ask(); got != "https://192.0.2.77:7341" {
		t.Fatalf("after the new lease: %s", got)
	}
}
