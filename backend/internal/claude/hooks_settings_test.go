package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallAndRemoveHooks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	// The user's own settings and hooks stay as they are.
	own := `{"model": "opus", "hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "lint.sh"}]}]}}`
	if err := os.WriteFile(path, []byte(own), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := InstallHooks(path, `C:\tools\agentctl.exe`)
	if err != nil || !changed {
		t.Fatalf("install: %v %v", changed, err)
	}
	if got := HooksInstalled(path); len(got) != len(Events) {
		t.Fatalf("installed events: %v", got)
	}
	var obj map[string]any
	b, _ := os.ReadFile(path)
	if err := json.Unmarshal(b, &obj); err != nil {
		t.Fatal(err)
	}
	if obj["model"] != "opus" {
		t.Fatal("other settings lost")
	}
	pre := obj["hooks"].(map[string]any)["PreToolUse"].([]any)
	if len(pre) != 2 || pre[0].(map[string]any)["matcher"] != "Bash" {
		t.Fatalf("the user's PreToolUse hook should come first and stay: %v", pre)
	}
	ourHook := pre[1].(map[string]any)["hooks"].([]any)[0].(map[string]any)
	if cmd := ourHook["command"]; cmd != `"C:/tools/agentctl.exe" hook` {
		t.Fatalf("command %q (bash on Windows needs forward slashes)", cmd)
	}
	// A permission request or a question may wait for the deck's answer; the
	// other hooks return at once.
	if ourHook["timeout"] != float64(120) {
		t.Fatalf("PreToolUse timeout %v", ourHook["timeout"])
	}
	stop := obj["hooks"].(map[string]any)["Stop"].([]any)[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)
	if stop["timeout"] != float64(5) {
		t.Fatalf("Stop timeout %v", stop["timeout"])
	}

	// Again: nothing to do. With the binary elsewhere: the command follows it.
	if changed, err := InstallHooks(path, `C:\tools\agentctl.exe`); err != nil || changed {
		t.Fatalf("second install: %v %v", changed, err)
	}
	if got := HooksOutdated(path); len(got) != 0 {
		t.Fatalf("outdated after install: %v", got)
	}
	// An install from before hooks waited: status names the event, and the
	// timeout is raised in place.
	ourHook["timeout"] = float64(5)
	b, _ = json.Marshal(obj)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := HooksOutdated(path); len(got) != 1 || got[0] != "PreToolUse" {
		t.Fatalf("outdated: %v", got)
	}
	if changed, err := InstallHooks(path, `C:\tools\agentctl.exe`); err != nil || !changed {
		t.Fatalf("old timeout: %v %v", changed, err)
	}
	b, _ = os.ReadFile(path)
	obj = nil
	_ = json.Unmarshal(b, &obj)
	if got := obj["hooks"].(map[string]any)["PreToolUse"].([]any)[1].(map[string]any)["hooks"].([]any)[0].(map[string]any)["timeout"]; got != float64(120) {
		t.Fatalf("timeout after reinstall: %v", got)
	}
	if got := HooksOutdated(path); len(got) != 0 {
		t.Fatalf("outdated after reinstall: %v", got)
	}
	if changed, err := InstallHooks(path, "/usr/local/bin/agentctl"); err != nil || !changed {
		t.Fatalf("moved binary: %v %v", changed, err)
	}

	// Removed: only ours go, the file is otherwise as it was.
	if changed, err := RemoveHooks(path); err != nil || !changed {
		t.Fatalf("remove: %v %v", changed, err)
	}
	if got := HooksInstalled(path); len(got) != 0 {
		t.Fatalf("still installed: %v", got)
	}
	b, _ = os.ReadFile(path)
	obj = nil
	_ = json.Unmarshal(b, &obj)
	hooks := obj["hooks"].(map[string]any)
	if len(hooks) != 1 || len(hooks["PreToolUse"].([]any)) != 1 {
		t.Fatalf("after remove: %v", hooks)
	}
	if changed, _ := RemoveHooks(path); changed {
		t.Fatal("nothing left to remove")
	}

	// No settings file yet: one is made, with just the hooks; removing them
	// leaves an empty object.
	fresh := filepath.Join(t.TempDir(), "settings.json")
	if _, err := InstallHooks(fresh, "/opt/agentctl"); err != nil {
		t.Fatal(err)
	}
	if _, err := RemoveHooks(fresh); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(fresh)
	if string(b) != "{}\n" {
		t.Fatalf("empty settings: %q", b)
	}
}

// A settings.json that links to a file elsewhere stays a link: the hooks go
// into the file it points to.
func TestInstallHooksKeepsSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "dotfiles", "claude-settings.json")
	if err := os.MkdirAll(filepath.Dir(real), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(real, []byte(`{"theme": "dark"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks aren't permitted here: %v", err)
	}
	if _, err := InstallHooks(link, "/usr/local/bin/agentctl"); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("settings.json is no longer a link: %v %v", fi, err)
	}
	if got := HooksInstalled(real); len(got) != len(Events) {
		t.Fatalf("hooks in the linked file: %v", got)
	}
	var obj map[string]any
	b, _ := os.ReadFile(real)
	if json.Unmarshal(b, &obj) != nil || obj["theme"] != "dark" {
		t.Fatalf("the user's settings: %s", b)
	}
	if _, err := os.Stat(real + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temp file left behind: %v", err)
	}
	if _, err := RemoveHooks(link); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(link); fi == nil || fi.Mode()&os.ModeSymlink == 0 || len(HooksInstalled(real)) != 0 {
		t.Fatal("remove: the link was replaced or the hooks stayed")
	}
}
