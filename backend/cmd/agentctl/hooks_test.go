package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/claude"
)

// `agentctl hooks status` tells an install from before the hooks could wait
// for the deck, and says to install again, as for a missing event.
func TestHooksStatusOldTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if got := hooksStatus(path); !strings.HasPrefix(got, "not installed") {
		t.Fatalf("no file: %q", got)
	}
	if _, err := claude.InstallHooks(path, "/usr/local/bin/agentctl"); err != nil {
		t.Fatal(err)
	}
	if got := hooksStatus(path); strings.Contains(got, "again") {
		t.Fatalf("fresh install: %q", got)
	}
	b, _ := os.ReadFile(path)
	// As an older agentctl installed them: every hook with 5 seconds.
	old := strings.ReplaceAll(string(b), `"timeout": 120`, `"timeout": 5`)
	if old == string(b) {
		t.Fatal("no 120 s timeout to make old")
	}
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	got := hooksStatus(path)
	if !strings.Contains(got, "PreToolUse") || !strings.Contains(got, "PermissionRequest") ||
		!strings.Contains(got, "old timeout") || !strings.Contains(got, "run `agentctl hooks install` again") {
		t.Fatalf("old timeout: %q", got)
	}
	if !strings.Contains(got, "\nPreToolUse, PermissionRequest installed, but") || strings.Contains(got, "missing") {
		t.Fatalf("only the waiting events are old: %q", got)
	}
}
