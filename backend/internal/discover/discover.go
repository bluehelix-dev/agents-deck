// Package discover lets a device on the same network find the deck without
// knowing its address: the daemon listens on a multicast group for a short
// query and answers it with the listener's addresses and the deck's
// certificate fingerprints.
//
// The answer is public: everything in it (the deck's name, where its
// listener is, the fingerprints of certificates the listener presents to
// anyone who connects) is what a device learns by connecting anyway. The
// answer proves nothing: a device trusts only the deck whose CA it paired
// with, and checks that over TLS as before. Discovery just says where to try.
package discover

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"strconv"
	"sync"
	"time"
)

const (
	// Group and Port are where the daemon listens: an administratively
	// scoped IPv4 multicast address (RFC 2365) that never leaves the LAN,
	// next to the listener's 7341.
	Group = "239.255.77.73"
	Port  = 7342

	// Query is what a device sends, as the whole datagram (a trailing
	// newline is fine).
	Query = "agents-deck?"

	maxPerSecond = 20
	refresh      = 30 * time.Second
)

// Info is what the deck says about itself.
type Info struct {
	Name    string // the deck's name, the computer's
	Port    int    // the listener's port
	CA      string // sha256:… of the deck's CA certificate
	Server  string // sha256:… of the listener's certificate
	Version string
}

// Reply is the answer's JSON: the listener's URLs, the one on the network
// the query came in on first.
type Reply struct {
	Deck    string   `json:"deck"`
	URLs    []string `json:"urls"`
	CA      string   `json:"ca"`
	Server  string   `json:"server"`
	Version string   `json:"version"`
}

// Serve answers queries on every network interface that has an IPv4
// address, picking up interfaces that come and go, until ctx ends. It
// never returns an error: a network that refuses multicast only means the
// deck isn't found there.
func Serve(ctx context.Context, info Info, logf func(string, ...any)) {
	r := &responder{info: info, logf: logf, conns: map[string]*net.UDPConn{}, told: map[string]bool{}}
	group := &net.UDPAddr{IP: net.ParseIP(Group), Port: Port}
	t := time.NewTicker(refresh)
	defer t.Stop()
	for {
		r.refresh(group)
		select {
		case <-ctx.Done():
			r.mu.Lock()
			for _, c := range r.conns {
				c.Close()
			}
			r.mu.Unlock()
			return
		case <-t.C:
		}
	}
}

type responder struct {
	info Info
	logf func(string, ...any)

	mu     sync.Mutex
	conns  map[string]*net.UDPConn // by interface name
	told   map[string]bool         // sources answered, for the log
	second int64                   // the token bucket: this second…
	count  int                     // …and the answers in it
}

// refresh joins the group on interfaces that appeared and drops the ones
// that went.
func (r *responder) refresh(group *net.UDPAddr) {
	ifs, _ := net.Interfaces()
	live := map[string][]string{}
	for _, ifi := range ifs {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagMulticast == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		var ips []string
		addrs, _ := ifi.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil && !ipn.IP.IsLinkLocalUnicast() {
				ips = append(ips, ipn.IP.String())
			}
		}
		if len(ips) > 0 {
			live[ifi.Name] = ips
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for name, c := range r.conns {
		if _, ok := live[name]; !ok {
			c.Close()
			delete(r.conns, name)
		}
	}
	for name, ips := range live {
		if _, ok := r.conns[name]; ok {
			continue
		}
		ifi, err := net.InterfaceByName(name)
		if err != nil {
			continue
		}
		c, err := net.ListenMulticastUDP("udp4", ifi, group)
		if err != nil {
			continue
		}
		r.conns[name] = c
		if len(r.conns) == 1 {
			r.logf("discovery: answering %q on udp %s:%d", Query, Group, Port)
		}
		go r.serveConn(c, ips, allIPv4)
	}
}

// serveConn answers queries on one socket; ips are the addresses of its
// interface, others lists the rest of the computer's.
func (r *responder) serveConn(c *net.UDPConn, ips []string, others func() []string) {
	buf := make([]byte, 512)
	for {
		n, src, err := c.ReadFromUDP(buf)
		if err != nil {
			return
		}
		if !isQuery(buf[:n]) || src.IP.To4() == nil || !r.allow() {
			continue
		}
		_, _ = c.WriteToUDP(r.reply(ips, others()), src)
		r.mu.Lock()
		if !r.told[src.IP.String()] && len(r.told) < 64 {
			r.told[src.IP.String()] = true
			r.logf("discovery: told %s where the deck is", src.IP)
		}
		r.mu.Unlock()
	}
}

// isQuery accepts the query alone, with optional trailing whitespace.
func isQuery(b []byte) bool {
	return string(bytes.TrimRight(b, " \r\n\t\x00")) == Query
}

// allow is a token bucket of maxPerSecond answers.
func (r *responder) allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().Unix()
	if now != r.second {
		r.second, r.count = now, 0
	}
	r.count++
	return r.count <= maxPerSecond
}

// reply is the answer for a query that came in on an interface with ips.
func (r *responder) reply(ips, others []string) []byte {
	seen := map[string]bool{}
	var urls []string
	for _, ip := range append(append([]string{}, ips...), others...) {
		if seen[ip] {
			continue
		}
		seen[ip] = true
		urls = append(urls, "https://"+net.JoinHostPort(ip, strconv.Itoa(r.info.Port)))
	}
	b, _ := json.Marshal(Reply{Deck: r.info.Name, URLs: urls, CA: r.info.CA, Server: r.info.Server, Version: r.info.Version})
	return b
}

// allIPv4 lists the computer's IPv4 addresses, without loopback and the
// 169.254.x.x an adapter gives itself when no network gave it one.
func allIPv4() []string {
	var ips []string
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLoopback() && !ipn.IP.IsLinkLocalUnicast() && ipn.IP.To4() != nil {
			ips = append(ips, ipn.IP.String())
		}
	}
	return ips
}
