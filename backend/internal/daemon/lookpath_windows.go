//go:build windows

package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
)

// LookPathLogin resolves a command on Windows: the user's PATH (which a
// scheduled task inherits), then where Claude Code's installer puts its
// binary, %USERPROFILE%\.local\bin.
func LookPathLogin(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	for _, ext := range []string{".exe", ".cmd", ""} {
		p := filepath.Join(home, ".local", "bin", name+ext)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}
