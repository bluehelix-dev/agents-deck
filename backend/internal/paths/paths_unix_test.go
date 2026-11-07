//go:build !windows

package paths

import (
	"os"
	"path/filepath"
	"testing"
)

// A socket may live in a folder of the user's own that nobody else can write
// to; a shared folder like /tmp is refused with advice.
func TestSocketDirOK(t *testing.T) {
	own := t.TempDir()
	if err := os.Chmod(own, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := SocketDirOK(filepath.Join(own, "agentd.sock")); err != nil {
		t.Fatalf("own 0700 folder: %v", err)
	}
	shared := filepath.Join(own, "shared")
	if err := os.Mkdir(shared, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(shared, 0o1777); err != nil {
		t.Fatal(err)
	}
	if err := SocketDirOK(filepath.Join(shared, "agentd.sock")); err == nil {
		t.Fatal("a world-writable folder was accepted")
	}
	if err := SocketDirOK(filepath.Join(own, "missing", "agentd.sock")); err == nil {
		t.Fatal("a missing folder was accepted")
	}
}

// With a deep data dir the socket moves to the runtime dir, XDG_RUNTIME_DIR
// first.
func TestSocketFallsBackToRuntimeDir(t *testing.T) {
	t.Setenv("AGENTSTERM_HOME", "/x/"+string(make([]byte, 0))+filepath.Join(repeat("d", 110)))
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	if got := Socket(); filepath.Dir(got) != "/run/user/1000" || filepath.Ext(got) != ".sock" {
		t.Fatalf("socket: %s", got)
	}
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("TMPDIR", "/var/folders/zz/T")
	if got := Socket(); filepath.Dir(got) != "/var/folders/zz/T" {
		t.Fatalf("socket: %s", got)
	}
}

func repeat(s string, n int) string {
	out := ""
	for range n {
		out += s
	}
	return out
}
