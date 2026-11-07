package link

import (
	"sort"
	"strconv"
	"strings"
	"unsafe"
)

// Windows COM ports, the parts that are plain data: the port settings, port
// names and the registry entries that list ports. They have no build tag so
// every platform's tests check them; port_windows.go and ports_windows.go
// make the system calls.

// dcb is the Win32 DCB structure (the port's line settings).
type dcb struct {
	DCBlength  uint32
	BaudRate   uint32
	Flags      uint32 // the bit fields fBinary … fAbortOnError
	wReserved  uint16
	XonLim     uint16
	XoffLim    uint16
	ByteSize   byte
	Parity     byte
	StopBits   byte
	XonChar    byte
	XoffChar   byte
	ErrorChar  byte
	EofChar    byte
	EvtChar    byte
	wReserved1 uint16
}

// DCB bit fields.
const (
	dcbBinary         = 1 << 0
	dcbParity         = 1 << 1
	dcbOutxCtsFlow    = 1 << 2
	dcbOutxDsrFlow    = 1 << 3
	dcbDtrControlMask = 3 << 4
	dcbDtrControlOn   = 1 << 4 // DTR_CONTROL_ENABLE
	dcbDsrSensitivity = 1 << 6
	dcbTXContinueXoff = 1 << 7
	dcbOutX           = 1 << 8
	dcbInX            = 1 << 9
	dcbErrorChar      = 1 << 10
	dcbNull           = 1 << 11
	dcbRtsControlMask = 3 << 12
	dcbRtsControlOn   = 1 << 12 // RTS_CONTROL_ENABLE
	dcbAbortOnError   = 1 << 14
	noParity          = 0
	oneStopBit        = 0
	maxDword          = 0xFFFFFFFF
)

// makeRawDCB sets d to what the boards need, as the macOS and Linux code
// does it: 8N1 at baud, binary, no flow control (neither RTS/CTS, DSR/DTR
// nor XON/XOFF), nothing replaced or dropped, and DTR and RTS on, as opening
// a tty leaves them (boards with native USB only send while DTR is on).
func makeRawDCB(d *dcb, baud int) {
	d.DCBlength = uint32(unsafe.Sizeof(*d))
	d.BaudRate = uint32(baud)
	f := d.Flags
	f &^= dcbParity | dcbOutxCtsFlow | dcbOutxDsrFlow | dcbDtrControlMask | dcbDsrSensitivity |
		dcbOutX | dcbInX | dcbErrorChar | dcbNull | dcbRtsControlMask | dcbAbortOnError
	f |= dcbBinary | dcbTXContinueXoff | dcbDtrControlOn | dcbRtsControlOn
	d.Flags = f
	d.ByteSize, d.Parity, d.StopBits = 8, noParity, oneStopBit
}

// commTimeouts is the Win32 COMMTIMEOUTS structure.
type commTimeouts struct {
	ReadIntervalTimeout         uint32
	ReadTotalTimeoutMultiplier  uint32
	ReadTotalTimeoutConstant    uint32
	WriteTotalTimeoutMultiplier uint32
	WriteTotalTimeoutConstant   uint32
}

// portTimeouts makes a read wait until at least one byte has arrived and
// then return what is there, like a tty with VMIN 1, VTIME 0. Both
// MAXDWORD fields with a constant below MAXDWORD mean exactly that; the
// constant caps the wait at about 49 days, after which a read returns
// nothing (Go says io.EOF) and the deck opens the port again. A constant of
// 0 instead would make every read return at once with nothing, which Go
// also turns into io.EOF. Writes have no timeout, as on the tty.
func portTimeouts() commTimeouts {
	return commTimeouts{
		ReadIntervalTimeout:        maxDword,
		ReadTotalTimeoutMultiplier: maxDword,
		ReadTotalTimeoutConstant:   maxDword - 1,
	}
}

// comName is the port's name as Windows lists it ("COM5"), from what the
// user registered: "COM5", "com5" or the device path `\\.\COM5`.
func comName(path string) string {
	name := strings.TrimPrefix(strings.TrimPrefix(path, `\\.\`), `//./`)
	return strings.ToUpper(name)
}

// devicePath is the path CreateFile takes: COM10 and above only open as
// `\\.\COM10`, and the prefix works for every port.
func devicePath(path string) string { return `\\.\` + comName(path) }

// serialEntry is one value of HKLM\HARDWARE\DEVICEMAP\SERIALCOMM: the
// driver's device object (`\Device\USBSER000`) and its port name (COM5).
// Windows lists only ports that are there right now.
type serialEntry struct{ Device, Name string }

// usbIdentity is a port's USB device as "USB vid:pid" (lowercase hex, as
// lsusb prints it), from its hardware key under
// HKLM\SYSTEM\CurrentControlSet\Enum\<bus>: "VID_303A&PID_1001&MI_00" on
// USB, "VID_0403+PID_6001+<serial>" on FTDIBUS. The serial number is left
// out on purpose: the port name is shown, the board's own id stays private.
// "" if the key names no vendor and product.
func usbIdentity(bus, key string) string {
	sep := "&"
	if strings.EqualFold(bus, "FTDIBUS") {
		sep = "+"
	}
	var vid, pid string
	for _, part := range strings.Split(strings.ToUpper(key), sep) {
		switch {
		case strings.HasPrefix(part, "VID_") && isHex4(part[4:]):
			vid = strings.ToLower(part[4:])
		case strings.HasPrefix(part, "PID_") && isHex4(part[4:]):
			pid = strings.ToLower(part[4:])
		}
	}
	if vid == "" || pid == "" {
		return ""
	}
	return "USB " + vid + ":" + pid
}

func isHex4(s string) bool {
	if len(s) != 4 {
		return false
	}
	_, err := strconv.ParseUint(s, 16, 16)
	return err == nil
}

// pickPorts lists the ports that may be boards: every port with a USB
// device (ids, by upper-case port name), and every other port except the
// PC's own UARTs (`\Device\Serial…`) and Bluetooth links (`\Device\BthModem…`).
// A USB adapter whose driver keeps its port name somewhere else than the
// usual "Device Parameters" key has no identity: it is still listed, by its
// COM name alone. Sorted by number (COM9 before COM10).
func pickPorts(entries []serialEntry, ids map[string]string) []PortInfo {
	var out []PortInfo
	seen := map[string]bool{}
	for _, e := range entries {
		name := comName(e.Name)
		if name == "" || seen[name] {
			continue
		}
		about := ids[name]
		dev := strings.ToLower(e.Device)
		if about == "" && (strings.HasPrefix(dev, `\device\serial`) || strings.HasPrefix(dev, `\device\bthmodem`)) {
			continue
		}
		seen[name] = true
		out = append(out, PortInfo{Path: name, About: about})
	}
	sort.Slice(out, func(i, j int) bool { return comLess(out[i].Path, out[j].Path) })
	return out
}

// comLess orders port names by their prefix, then by their number.
func comLess(a, b string) bool {
	pa, na := splitNumber(a)
	pb, nb := splitNumber(b)
	if pa != pb {
		return pa < pb
	}
	if na != nb {
		return na < nb
	}
	return a < b
}

func splitNumber(s string) (string, int) {
	i := len(s)
	for i > 0 && s[i-1] >= '0' && s[i-1] <= '9' {
		i--
	}
	n, _ := strconv.Atoi(s[i:])
	return s[:i], n
}
