package store

import (
	"path/filepath"
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/model"
)

// Mirrored agents are never in the registry, but where they sat is
// remembered: after a restart (or a stop and a start of the session) they
// go back to their slot, unless something else took it meanwhile.
func TestMirroredAgentsKeepTheirSlots(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	ext := func(id string) *Entry {
		return &Entry{External: true, A: model.Agent{ID: id, Tool: "claude", External: true}}
	}
	s := New(path, "deck", "0")
	s.Add(ext("l1"))
	s.Add(ext("l2"))
	if !s.Place("l1", 7) {
		t.Fatal("place")
	}
	s.Flush()

	// Gone and back within the same run: the same slot.
	s.Remove("l1")
	s.Add(ext("l1"))
	if e, _ := s.Get("l1"); e.A.Slot != 7 {
		t.Fatalf("back in the same run: slot %d", e.A.Slot)
	}

	// A restart: the registry has no mirrored agents, slots.json knows them.
	s2 := New(path, "deck", "0")
	s2.Load()
	s2.Add(ext("l2"))
	s2.Add(ext("l1"))
	e1, _ := s2.Get("l1")
	e2, _ := s2.Get("l2")
	if e1.A.Slot != 7 || e2.A.Slot != 1 {
		t.Fatalf("after a restart: l1 %d (want 7), l2 %d (want 1)", e1.A.Slot, e2.A.Slot)
	}

	// Its slot taken by then: the first free one, and that is remembered next.
	s3 := New(path, "deck", "0")
	s3.Load()
	s3.Add(&Entry{A: model.Agent{ID: "own", Tool: "claude", Slot: 0}})
	s3.Place("own", 7)
	s3.Add(ext("l1"))
	if e, _ := s3.Get("l1"); e.A.Slot != 0 {
		t.Fatalf("slot taken: got %d, want 0", e.A.Slot)
	}
	s3.Flush()
	s4 := New(path, "deck", "0")
	s4.Load()
	s4.Add(ext("l1"))
	if e, _ := s4.Get("l1"); e.A.Slot != 0 {
		t.Fatalf("remembered the new slot: got %d, want 0", e.A.Slot)
	}

	// Agents the deck started are not affected: they are in the registry.
	if _, ok := s4.Get("own"); !ok {
		t.Fatal("the deck's own agent should be loaded from the registry")
	}
}
