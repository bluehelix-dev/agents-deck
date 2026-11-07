//go:build !windows

package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// runtimeDir is the per-user private directory a socket may fall back to:
// $XDG_RUNTIME_DIR (Linux desktops, 0700), else the temp dir, which macOS gives
// each user privately ($TMPDIR) and Linux shares (/tmp): SocketDirOK refuses
// the latter.
func runtimeDir() string {
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return d
	}
	return os.TempDir()
}

// SocketDirOK says whether the daemon's socket may live at sock: its folder
// is the current user's own and nobody else can write there. In a shared
// folder (Linux's /tmp) another user could put a socket of that name first
// and speak for the deck, so the daemon refuses to start there and the
// clients refuse to connect, each with this error's advice.
func SocketDirOK(sock string) error {
	dir := filepath.Dir(sock)
	st, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if sys, ok := st.Sys().(*syscall.Stat_t); ok && int(sys.Uid) != os.Getuid() {
		return fmt.Errorf("%s belongs to another user: the deck's socket can't live there (set AGENTSTERM_HOME to a short folder of your own)", dir)
	}
	if st.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%s is writable by other users: the deck's socket can't live there (set AGENTSTERM_HOME to a short folder of your own, or XDG_RUNTIME_DIR)", dir)
	}
	return nil
}
