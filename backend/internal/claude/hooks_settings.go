package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SettingsPath is Claude Code's user settings file: CLAUDE_CONFIG_DIR, else
// ~/.claude, then settings.json. Hooks there run in every session on this
// machine, in any terminal and in the Claude desktop app.
func SettingsPath() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "settings.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "settings.json")
}

func hookCommand(agentctl string) string { return fmt.Sprintf("%q hook", agentctl) }

// ours tells a hook group the deck installed from the user's own: every
// command in it is an `agentctl hook` call.
func ours(group map[string]any) bool {
	hooks, _ := group["hooks"].([]any)
	if len(hooks) == 0 {
		return false
	}
	for _, h := range hooks {
		m, _ := h.(map[string]any)
		cmd, _ := m["command"].(string)
		if !strings.HasSuffix(cmd, `" hook`) || !strings.Contains(cmd, "agentctl") {
			return false
		}
	}
	return true
}

func readSettings(path string) (map[string]any, error) {
	obj := map[string]any{}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return obj, nil
	}
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return obj, nil
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %v", path, err)
	}
	return obj, nil
}

func writeSettings(path string, obj map[string]any) error {
	out, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(out, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// InstallHooks adds `<agentctl> hook` to every event in the settings file at
// path (keeping whatever hooks are there), so sessions started by hand report
// to the deck too. It returns false when every event had it already.
func InstallHooks(path, agentctl string) (bool, error) {
	obj, err := readSettings(path)
	if err != nil {
		return false, err
	}
	hooks, _ := obj["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	cmd := hookCommand(agentctl)
	changed := false
	for _, ev := range Events {
		list, _ := hooks[ev].([]any)
		have := false
		for _, g := range list {
			if m, ok := g.(map[string]any); ok && ours(m) {
				for _, h := range m["hooks"].([]any) {
					if hm, _ := h.(map[string]any); hm["command"] != cmd {
						hm["command"] = cmd // the binary moved: point at this one
						changed = true
					}
				}
				have = true
			}
		}
		if have {
			continue
		}
		group := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": cmd, "timeout": float64(5)}}}
		if ev == "PreToolUse" || ev == "PostToolUse" || ev == "PermissionRequest" {
			group["matcher"] = "*"
		}
		hooks[ev] = append(list, group)
		changed = true
	}
	if !changed {
		return false, nil
	}
	obj["hooks"] = hooks
	return true, writeSettings(path, obj)
}

// RemoveHooks takes the deck's hook groups out again, leaving the user's.
func RemoveHooks(path string) (bool, error) {
	obj, err := readSettings(path)
	if err != nil {
		return false, err
	}
	hooks, _ := obj["hooks"].(map[string]any)
	changed := false
	for ev, v := range hooks {
		list, _ := v.([]any)
		kept := list[:0:0]
		for _, g := range list {
			if m, ok := g.(map[string]any); ok && ours(m) {
				changed = true
				continue
			}
			kept = append(kept, g)
		}
		if len(kept) == 0 {
			delete(hooks, ev)
		} else {
			hooks[ev] = kept
		}
	}
	if !changed {
		return false, nil
	}
	if len(hooks) == 0 {
		delete(obj, "hooks")
	} else {
		obj["hooks"] = hooks
	}
	return true, writeSettings(path, obj)
}

// HooksInstalled lists the events that report to the deck from path.
func HooksInstalled(path string) []string {
	obj, err := readSettings(path)
	if err != nil {
		return nil
	}
	hooks, _ := obj["hooks"].(map[string]any)
	var out []string
	for _, ev := range Events {
		list, _ := hooks[ev].([]any)
		for _, g := range list {
			if m, ok := g.(map[string]any); ok && ours(m) {
				out = append(out, ev)
				break
			}
		}
	}
	return out
}
