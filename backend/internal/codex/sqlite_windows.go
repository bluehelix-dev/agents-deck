//go:build windows

package codex

import "os/exec"

// sqlite3Bin: Windows ships no sqlite3, so it's whichever one is installed
// on PATH ("" when none: Codex sessions then stay off the deck).
func sqlite3Bin() string {
	p, err := exec.LookPath("sqlite3")
	if err != nil {
		return "sqlite3"
	}
	return p
}
