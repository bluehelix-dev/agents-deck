//go:build windows

package daemon

import "os"

// processGone reports whether no process with pid exists any more: on
// Windows os.FindProcess opens a handle, which fails once it has exited.
func processGone(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return true
	}
	p.Release()
	return false
}
