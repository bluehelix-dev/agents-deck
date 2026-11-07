package link

import (
	"reflect"
	"testing"
	"unsafe"
)

// The structures go to the Win32 API as they are: their sizes must match.
func TestWin32StructSizes(t *testing.T) {
	if n := unsafe.Sizeof(dcb{}); n != 28 {
		t.Errorf("DCB is %d bytes, Win32 has 28", n)
	}
	if n := unsafe.Sizeof(commTimeouts{}); n != 20 {
		t.Errorf("COMMTIMEOUTS is %d bytes, Win32 has 20", n)
	}
}

// Whatever the driver had (flow control, parity, abort on error, 7E2), the
// port ends up 8N1, binary, no flow control, DTR and RTS on.
func TestMakeRawDCB(t *testing.T) {
	d := dcb{Flags: maxDword, ByteSize: 7, Parity: 2, StopBits: 2}
	makeRawDCB(&d, 921600)
	if d.DCBlength != 28 || d.BaudRate != 921600 {
		t.Errorf("length %d, baud %d", d.DCBlength, d.BaudRate)
	}
	if d.ByteSize != 8 || d.Parity != noParity || d.StopBits != oneStopBit {
		t.Errorf("%d%d%d, want 8N1", d.ByteSize, d.Parity, d.StopBits)
	}
	for name, bit := range map[string]uint32{"binary": dcbBinary, "DTR on": dcbDtrControlOn, "RTS on": dcbRtsControlOn} {
		if d.Flags&bit == 0 {
			t.Errorf("%s: off", name)
		}
	}
	off := map[string]uint32{
		"parity check": dcbParity, "CTS flow": dcbOutxCtsFlow, "DSR flow": dcbOutxDsrFlow,
		"DSR sensitivity": dcbDsrSensitivity, "XON/XOFF out": dcbOutX, "XON/XOFF in": dcbInX,
		"error char": dcbErrorChar, "drop NUL": dcbNull, "abort on error": dcbAbortOnError,
	}
	for name, bit := range off {
		if d.Flags&bit != 0 {
			t.Errorf("%s: on", name)
		}
	}
	if dtr, rts := d.Flags&dcbDtrControlMask>>4, d.Flags&dcbRtsControlMask>>12; dtr != 1 || rts != 1 {
		t.Errorf("DTR control %d, RTS control %d: want 1 (enable), not handshake or toggle", dtr, rts)
	}
}

// A read waits for the first byte and returns what is there; it must never
// return at once with nothing (Go reads that as io.EOF: the session ends).
func TestPortTimeouts(t *testing.T) {
	to := portTimeouts()
	if to.ReadIntervalTimeout != maxDword || to.ReadTotalTimeoutMultiplier != maxDword {
		t.Errorf("interval %#x, multiplier %#x: want both MAXDWORD", to.ReadIntervalTimeout, to.ReadTotalTimeoutMultiplier)
	}
	if c := to.ReadTotalTimeoutConstant; c == 0 || c == maxDword {
		t.Errorf("constant %#x: 0 returns at once, MAXDWORD isn't the wait-for-a-byte mode", c)
	}
	if to.WriteTotalTimeoutMultiplier != 0 || to.WriteTotalTimeoutConstant != 0 {
		t.Error("writes time out")
	}
}

func TestComName(t *testing.T) {
	for in, want := range map[string]string{"COM5": "COM5", "com12": "COM12", `\\.\COM3`: "COM3", `\\.\com7`: "COM7"} {
		if got := comName(in); got != want {
			t.Errorf("comName(%q) = %q, want %q", in, got, want)
		}
	}
	if got := devicePath("com10"); got != `\\.\COM10` {
		t.Errorf("devicePath: %q", got)
	}
}

func TestUSBIdentity(t *testing.T) {
	cases := []struct{ bus, key, want string }{
		{"USB", "VID_303A&PID_1001&MI_00", "USB 303a:1001"}, // native USB, one interface of several
		{"USB", "VID_10C4&PID_EA60", "USB 10c4:ea60"},       // CP210x
		{"USB", "vid_1a86&pid_7523", "USB 1a86:7523"},       // CH340
		{"FTDIBUS", "VID_0403+PID_6001+A1B2C3D4A", "USB 0403:6001"},
		{"USB", "ROOT_HUB30", ""},
		{"USB", "VID_303A", ""},
		{"USB", "VID_30&PID_1001", ""},
		{"FTDIBUS", "VID_0403&PID_6001", ""}, // FTDIBUS keys use +
	}
	for _, c := range cases {
		if got := usbIdentity(c.bus, c.key); got != c.want {
			t.Errorf("usbIdentity(%q, %q) = %q, want %q", c.bus, c.key, got, c.want)
		}
	}
}

func TestPickPorts(t *testing.T) {
	entries := []serialEntry{
		{`\Device\Serial0`, "COM1"},    // the PC's own UART: skipped
		{`\Device\BthModem0`, "COM3"},  // Bluetooth: skipped
		{`\Device\USBSER000`, "COM10"}, // a board with native USB
		{`\Device\Silabser0`, "COM9"},  // CP210x, its identity not found: listed by name
		{`\Device\Serial2`, "COM6"},    // a USB adapter whose driver calls itself Serial: listed, it has an identity
		{`\Device\USBSER001`, "COM10"}, // the same name twice: once
	}
	ids := map[string]string{"COM10": "USB 303a:1001", "COM6": "USB 1a86:7523", "COM20": "USB 0403:6001"}
	want := []PortInfo{{"COM6", "USB 1a86:7523"}, {"COM9", ""}, {"COM10", "USB 303a:1001"}}
	if got := pickPorts(entries, ids); !reflect.DeepEqual(got, want) {
		t.Errorf("pickPorts:\n got %v\nwant %v", got, want)
	}
	if got := pickPorts(nil, ids); len(got) != 0 {
		t.Errorf("no SERIALCOMM, still %v", got)
	}
}
