package server

import (
	"log"
	"testing"
)

// The network listener's error log drops the TLS handshake noise of devices
// reconnecting, and keeps everything else: a handler's panic, Accept errors.
func TestDeviceErrorLogDropsOnlyHandshakeNoise(t *testing.T) {
	var got []string
	l := log.New(handshakeNoise{next: func(s string) { got = append(got, s) }}, "", 0)
	l.Printf("http: TLS handshake error from 192.0.2.20:51234: EOF")
	l.Printf("http: panic serving 192.0.2.20:51234: boom")
	l.Printf("http: Accept error: too many open files; retrying in 5ms")
	if len(got) != 2 || got[0] != "http: panic serving 192.0.2.20:51234: boom" {
		t.Fatalf("logged: %q", got)
	}
}
