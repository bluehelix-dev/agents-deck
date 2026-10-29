package main

import (
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/link"
)

func TestPrintable(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"", ""},
		{"panel-v2 1.4.0", "panel-v2 1.4.0"},
		{"192.0.2.23", "192.0.2.23"},
		{"lab · Wi-Fi ✳", "lab · Wi-Fi ✳"},
		{"esp32\x1b[2J\x1b[H", `esp32\x1b[2J\x1b[H`},
		{"\x1b]52;c;ZWNobyBoaQ==\x07", `\x1b]52;c;ZWNobyBoaQ==\x07`},
		{"a\rb\nc\td", `a\x0db\x0ac\x09d`},
		{"del\x7f", `del\x7f`},
		{"c1\u009b31m", `c1\x9b31m`},
		{"name\u202etxt.exe", `name\u202etxt.exe`},
		{"bad\xffbyte", `bad\xffbyte`},
	} {
		if got := printable(c.in); got != c.want {
			t.Errorf("printable(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNetLinePrintable(t *testing.T) {
	if got := netLine(nil); got != "-" {
		t.Errorf("nil: %q", got)
	}
	n := &link.NetInfo{Wifi: "connected\x1b]0;deck\x07", IP: "192.0.2.23\r\n", RSSI: -58, HasCert: true}
	if got, want := netLine(n), `connected\x1b]0;deck\x07 · 192.0.2.23\x0d\x0a · -58 dBm`; got != want {
		t.Errorf("netLine = %q, want %q", got, want)
	}
}
