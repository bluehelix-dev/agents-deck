//go:build windows

package link

import (
	"syscall"
	"unsafe"
)

// The registry, read with the standard library: syscall has RegOpenKeyEx,
// RegEnumKeyEx and RegQueryValueEx; RegEnumValueW comes from advapi32.
var procRegEnumValueW = syscall.NewLazyDLL("advapi32.dll").NewProc("RegEnumValueW")

const (
	serialCommKey = `HARDWARE\DEVICEMAP\SERIALCOMM`
	enumKey       = `SYSTEM\CurrentControlSet\Enum\`

	errNoMoreItems = syscall.Errno(259) // ERROR_NO_MORE_ITEMS
)

// listPorts lists the COM ports there now that may be boards, with their
// USB vendor and product where Windows records them.
func listPorts() []PortInfo { return pickPorts(serialComm(), usbIdentities()) }

// portPresent: Windows lists a COM port in SERIALCOMM while it is plugged
// in. os.Stat can't tell: it doesn't know COM names, and opening the port
// to find out would take it from whoever has it.
func portPresent(path string) bool {
	name := comName(path)
	for _, e := range serialComm() {
		if comName(e.Name) == name {
			return true
		}
	}
	return false
}

// serialComm reads HKLM\HARDWARE\DEVICEMAP\SERIALCOMM (nil if it isn't
// there: a PC with no COM port at all has no such key).
func serialComm() []serialEntry {
	k, err := openKey(serialCommKey)
	if err != nil {
		return nil
	}
	defer syscall.RegCloseKey(k)
	var out []serialEntry
	name := make([]uint16, 256)
	data := make([]uint16, 256)
	for i := uint32(0); ; i++ {
		nameLen, dataLen, typ := uint32(len(name)), uint32(len(data)*2), uint32(0)
		r, _, _ := procRegEnumValueW.Call(uintptr(k), uintptr(i),
			uintptr(unsafe.Pointer(&name[0])), uintptr(unsafe.Pointer(&nameLen)), 0,
			uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&data[0])), uintptr(unsafe.Pointer(&dataLen)))
		if syscall.Errno(r) == errNoMoreItems {
			break
		}
		if r != 0 { // a name or value too long for a port: skip it
			if syscall.Errno(r) == syscall.ERROR_MORE_DATA {
				continue
			}
			break
		}
		if typ != syscall.REG_SZ {
			continue
		}
		out = append(out, serialEntry{
			Device: syscall.UTF16ToString(name[:nameLen]),
			Name:   syscall.UTF16ToString(data[:dataLen/2]),
		})
	}
	return out
}

// usbIdentities maps upper-case COM names to "USB vid:pid". Every device
// Windows has ever seen keeps a key under Enum\<bus>\<hardware id>\<instance>,
// and a serial port's "Device Parameters" subkey holds its PortName: the
// usbser (CDC, boards with native USB), CP210x and CH340 drivers file
// themselves under USB, FTDI's under FTDIBUS. Unplugged devices are there
// too; SERIALCOMM says which ports are present.
func usbIdentities() map[string]string {
	ids := map[string]string{}
	for _, bus := range []string{"USB", "FTDIBUS"} {
		busKey, err := openKey(enumKey + bus)
		if err != nil {
			continue
		}
		for _, hw := range subkeys(busKey) {
			id := usbIdentity(bus, hw)
			if id == "" {
				continue
			}
			hwKey, err := openSubkey(busKey, hw)
			if err != nil {
				continue
			}
			for _, inst := range subkeys(hwKey) {
				params, err := openSubkey(hwKey, inst+`\Device Parameters`)
				if err != nil {
					continue
				}
				if port := stringValue(params, "PortName"); port != "" {
					ids[comName(port)] = id
				}
				syscall.RegCloseKey(params)
			}
			syscall.RegCloseKey(hwKey)
		}
		syscall.RegCloseKey(busKey)
	}
	return ids
}

func openKey(path string) (syscall.Handle, error) {
	return openSubkey(syscall.HKEY_LOCAL_MACHINE, path)
}

func openSubkey(parent syscall.Handle, path string) (syscall.Handle, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var k syscall.Handle
	if err := syscall.RegOpenKeyEx(parent, p, 0, syscall.KEY_READ, &k); err != nil {
		return 0, err
	}
	return k, nil
}

func subkeys(k syscall.Handle) []string {
	var out []string
	buf := make([]uint16, 256) // registry key names are at most 255 characters
	for i := uint32(0); ; i++ {
		n := uint32(len(buf))
		if err := syscall.RegEnumKeyEx(k, i, &buf[0], &n, nil, nil, nil, nil); err != nil {
			break
		}
		out = append(out, syscall.UTF16ToString(buf[:n]))
	}
	return out
}

// stringValue reads a short REG_SZ value ("" if it isn't one).
func stringValue(k syscall.Handle, name string) string {
	p, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return ""
	}
	buf := make([]uint16, 64)
	n, typ := uint32(len(buf)*2), uint32(0)
	if err := syscall.RegQueryValueEx(k, p, nil, &typ, (*byte)(unsafe.Pointer(&buf[0])), &n); err != nil || typ != syscall.REG_SZ {
		return ""
	}
	return syscall.UTF16ToString(buf[:n/2])
}
