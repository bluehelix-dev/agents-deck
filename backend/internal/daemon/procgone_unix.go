//go:build !windows

package daemon

import (
	"errors"
	"syscall"
)

// processGone reports whether no process with pid exists any more.
func processGone(pid int) bool {
	err := syscall.Kill(pid, 0)
	return errors.Is(err, syscall.ESRCH)
}
