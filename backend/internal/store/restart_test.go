package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
)

// A daemon that restarts in the middle of a compaction or of a background
// subagent run picks up where it was: how the compaction began and when, and
// that the session only runs for its subagents.
func TestRunStateSurvivesARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	since := time.Now().Add(-30 * time.Second).UTC().Truncate(time.Second)
	s := New(path, "deck", "0")
	s.Add(&Entry{Background: true, A: model.Agent{ID: "a1", Tool: "claude"}})
	s.Add(&Entry{CompactToIdle: true, CompactSince: since, A: model.Agent{ID: "a2", Tool: "claude"}})
	s.Flush()

	s2 := New(path, "deck", "0")
	s2.Load()
	if e, _ := s2.Get("a1"); !e.Background {
		t.Fatalf("background lost: %+v", e)
	}
	if e, _ := s2.Get("a2"); !e.CompactToIdle || !e.CompactSince.Equal(since) {
		t.Fatalf("compaction lost: %+v", e)
	}
}

// An agents.json that doesn't parse is set aside before the next save
// replaces it; a null entry is skipped.
func TestUnreadableAgentsFileIsKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	if err := os.WriteFile(path, []byte(`[{"A":`), 0o600); err != nil {
		t.Fatal(err)
	}
	s := New(path, "deck", "0")
	s.Load()
	if b, err := os.ReadFile(path + ".bad"); err != nil || string(b) != `[{"A":` {
		t.Fatalf("not set aside: %q %v", b, err)
	}
	if err := os.WriteFile(path, []byte(`[null,{"agent":{"id":"a1","tool":"claude"}}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	s2 := New(path, "deck", "0")
	s2.Load()
	if _, ok := s2.Get("a1"); !ok {
		t.Fatal("entry after a null lost")
	}
}
