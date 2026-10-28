package store

import (
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
