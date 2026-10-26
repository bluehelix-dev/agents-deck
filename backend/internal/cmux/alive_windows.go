//go:build windows

package cmux

import "os"

// alive reports whether a process with pid exists. On Windows os.FindProcess
// opens a handle to it, which fails when there is none.
func alive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	p.Release()
	return true
}
