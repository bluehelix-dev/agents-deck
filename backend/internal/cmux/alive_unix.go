//go:build !windows

package cmux

import (
	"errors"
	"syscall"
)

// alive reports whether a process with pid exists (signal 0; EPERM still means it's there).
func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
