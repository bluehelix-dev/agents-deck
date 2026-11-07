//go:build windows && go1.25

package link

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// The port is opened for overlapped I/O, and Go 1.25 is the first whose
// os.NewFile takes such a handle (older ones get port_other.go). Overlapped
// matters twice over:
//   - On a handle opened without it, Windows runs one call at a time: a
//     write waits until the pending read returns, that is until the board
//     sends something, and the board waits for the deck.
//   - os.File.Close waits for a pending read to finish; with overlapped I/O
//     it cancels the read instead, so the daemon lets go of the port (and
//     shuts down) at once.
var (
	kernel32            = syscall.NewLazyDLL("kernel32.dll")
	procGetCommState    = kernel32.NewProc("GetCommState")
	procSetCommState    = kernel32.NewProc("SetCommState")
	procSetCommTimeouts = kernel32.NewProc("SetCommTimeouts")
	procSetupComm       = kernel32.NewProc("SetupComm")
	procPurgeComm       = kernel32.NewProc("PurgeComm")
)

const (
	purgeTxClear        = 0x0004
	purgeRxClear        = 0x0008
	errSharingViolation = syscall.Errno(32)
)

func openPort(path string, baud int) (*os.File, error) {
	if baud <= 0 {
		return nil, fmt.Errorf("baud %d: must be above 0", baud)
	}
	h, err := openHandle(devicePath(path))
	if err != nil {
		if errors.Is(err, syscall.ERROR_ACCESS_DENIED) || errors.Is(err, errSharingViolation) {
			// Windows lets one program at a time have a COM port.
			return nil, fmt.Errorf("%s: %w", comName(path), ErrPortBusy)
		}
		return nil, fmt.Errorf("%s: %w", comName(path), err)
	}
	if err := configurePort(h, baud); err != nil {
		syscall.CloseHandle(h)
		return nil, fmt.Errorf("%s: %w", comName(path), err)
	}
	return os.NewFile(uintptr(h), path), nil
}

// openHandle opens the device for reading and writing, for this process
// alone and for overlapped I/O. The nil security attributes make the handle
// not inheritable: programs the daemon starts (tmux, CLIs) must not hold the
// port open.
func openHandle(path string) (syscall.Handle, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return syscall.InvalidHandle, err
	}
	return syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil,
		syscall.OPEN_EXISTING, syscall.FILE_FLAG_OVERLAPPED, 0)
}

func configurePort(h syscall.Handle, baud int) error {
	var d dcb
	d.DCBlength = uint32(unsafe.Sizeof(d))
	if r, _, err := procGetCommState.Call(uintptr(h), uintptr(unsafe.Pointer(&d))); r == 0 {
		return fmt.Errorf("reading the port settings: %w", err)
	}
	makeRawDCB(&d, baud)
	if r, _, err := procSetCommState.Call(uintptr(h), uintptr(unsafe.Pointer(&d))); r == 0 {
		return fmt.Errorf("baud %d: %w", baud, err)
	}
	// Bigger driver buffers than the default; a driver that can't is fine.
	procSetupComm.Call(uintptr(h), 4096, 4096)
	t := portTimeouts()
	if r, _, err := procSetCommTimeouts.Call(uintptr(h), uintptr(unsafe.Pointer(&t))); r == 0 {
		return fmt.Errorf("setting the port's timeouts: %w", err)
	}
	// Drop what arrived before the deck opened the port (boot messages).
	procPurgeComm.Call(uintptr(h), purgeRxClear|purgeTxClear)
	return nil
}
