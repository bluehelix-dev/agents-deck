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
	cmd := pre[1].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"]
	if cmd != hookCommand(`C:\tools\agentctl.exe`) {
		t.Fatalf("command %q", cmd)
	}

	// Again: nothing to do. With the binary elsewhere: the command follows it.
	if changed, err := InstallHooks(path, `C:\tools\agentctl.exe`); err != nil || changed {
		t.Fatalf("second install: %v %v", changed, err)
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
