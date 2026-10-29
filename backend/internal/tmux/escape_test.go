package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestEscape(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"", ""},
		{"plain folder", "plain folder"},
		{"#(touch pwned)", "##(touch pwned)"},
		{"#{pane_pid}", "##{pane_pid}"},
		{"a##b", "a####b"},
		{"issue #12 fix", "issue ##12 fix"},
	} {
		if got := Escape(c.in); got != c.want {
			t.Errorf("Escape(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// A folder named like a tmux format must not run anything when an agent
// starts there, and the pane must start in exactly that folder.
func TestNewSessionFolderIsLiteral(t *testing.T) {
	if _, err := exec.LookPath(Bin); err != nil {
		t.Skip("no tmux")
	}
	old := Socket
	Socket = fmt.Sprintf("agentsdeck-esc-%d", os.Getpid())
	t.Cleanup(func() { _, _ = run("kill-server"); Socket = old })
	base := t.TempDir()
	marker := filepath.Join(base, "ran")
	dir := filepath.Join(base, "#(touch "+marker+")")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Skip("this file system does not allow the folder name:", err)
	}
	if err := NewSession("esc", dir, nil, []string{"cat"}); err != nil {
		t.Fatal(err)
	}
	got, err := run("display-message", "-p", "-t", "esc", "#{pane_start_path}")
	if err == nil && got != "" && got != dir {
		t.Errorf("pane started in %q, want %q", got, dir)
	}
	time.Sleep(300 * time.Millisecond) // #() jobs run in the background
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the folder name ran as a command")
	}
}
