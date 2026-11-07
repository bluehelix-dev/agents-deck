package link

import (
	"errors"
	"os"
)

// ErrUnsupported: serial ports on this platform aren't implemented yet.
var ErrUnsupported = errors.New("serial ports aren't supported on this platform yet")

// ErrPortBusy: another program has the port open. Windows lets one program
// at a time have a COM port; macOS and Linux never return it.
var ErrPortBusy = errors.New("another program has the port open")

// OpenPort opens a serial port raw, 8N1, no flow control, at baud.
func OpenPort(path string, baud int) (*os.File, error) { return openPort(path, baud) }

// PortInfo is a serial port that may be a board.
type PortInfo struct {
	Path  string // what `agentctl link add` takes: /dev/cu.usbmodem1101, COM5
	About string // "USB 303a:1001" where the platform tells, else ""
}

// ListPorts lists USB serial ports that look like boards.
func ListPorts() []PortInfo { return listPorts() }

// PortPresent says whether the port is plugged in, without opening it.
func PortPresent(path string) bool { return portPresent(path) }
