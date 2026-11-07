package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/accounts"
	"github.com/brushknight/agents-deck/backend/internal/claude"
)

// The hooks go into every account's settings.json, the one in use first and
// each once; without accounts only into the one in use.
func TestHookTargets(t *testing.T) {
	dir := t.TempDir()
	personal := accounts.Account{Name: "personal", Dir: filepath.Join(dir, "claude"), Color: "sky"}
	work := accounts.Account{Name: "work", Dir: filepath.Join(dir, "claude-work"), Color: "yellow"}
	inPersonal := filepath.Join(dir, "claude", "settings.json")
	inWork := filepath.Join(dir, "claude-work", "settings.json")

	if got := hookTargets(inPersonal, nil); len(got) != 1 || got[0] != (hookTarget{path: inPersonal}) {
		t.Fatalf("no accounts: %+v", got)
	}
	got := hookTargets(inWork, []accounts.Account{personal, work})
	want := []hookTarget{{"work", inWork}, {"personal", inPersonal}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("two accounts: %+v, want %+v", got, want)
	}
	other := filepath.Join(dir, "elsewhere", "settings.json") // CLAUDE_CONFIG_DIR outside the accounts
	got = hookTargets(other, []accounts.Account{personal, work})
	want = []hookTarget{{"current", other}, {"personal", inPersonal}, {"work", inWork}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("outside the accounts: %+v, want %+v", got, want)
	}
}

// Install, status and remove go through every account.
func TestHooksEveryAccount(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGENTSTERM_HOME", filepath.Join(dir, "deck"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(dir, "claude"))
	list := []accounts.Account{
		{Name: "personal", Dir: filepath.Join(dir, "claude"), Color: "sky"},
		{Name: "work", Dir: filepath.Join(dir, "claude-work"), Color: "yellow"},
	}
	if err := accounts.Save(list, "personal"); err != nil {
		t.Fatal(err)
	}
	if got := accounts.Configured(); len(got) != 2 {
		t.Fatalf("accounts not saved where the test reads them: %+v", got)
	}
	if err := hooks([]string{"install"}); err != nil {
		t.Fatal(err)
	}
	for _, a := range list {
		if got := claude.HooksInstalled(filepath.Join(a.Path(), "settings.json")); len(got) != len(claude.Events) {
			t.Fatalf("%s: installed %v", a.Name, got)
		}
	}
	if err := hooks([]string{"remove"}); err != nil {
		t.Fatal(err)
	}
	for _, a := range list {
		if got := claude.HooksInstalled(filepath.Join(a.Path(), "settings.json")); len(got) != 0 {
			t.Fatalf("%s: still installed %v", a.Name, got)
		}
	}
}

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
