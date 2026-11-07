//go:build !windows

package link

import (
	"os"
	"path/filepath"
	"sort"
)

func listPorts() []PortInfo {
	var out []PortInfo
	for _, g := range []string{"/dev/cu.usbmodem*", "/dev/cu.usbserial*", "/dev/cu.SLAB_USBtoUART*", "/dev/cu.wchusbserial*", "/dev/ttyACM*", "/dev/ttyUSB*"} {
		m, _ := filepath.Glob(g)
		for _, p := range m {
			out = append(out, PortInfo{Path: p})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func portPresent(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
