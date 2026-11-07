package daemon

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/codex"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

// Hiding a thread while the poll runs is safe: the poll reads its own copy of
// the hidden list (go test -race reports it otherwise).
func TestCodexHideDuringSync(t *testing.T) {
	d := &Daemon{Store: store.New("", "test", "0")}
	threads := []codex.Thread{{ID: "t1", RolloutPath: filepath.Join(t.TempDir(), "rollout.jsonl")}}
	hide := func(id string) {
		d.hideExternal(store.Entry{SessionID: id, A: model.Agent{ID: codex.AgentID(id)}})
	}
	hide("t0")
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 200 {
			hide(fmt.Sprintf("h%d", i))
		}
	}()
	for range 200 {
		d.syncCodex(threads)
	}
	<-done
}

// A failed read of Codex's database (locked for a moment) leaves its tiles
// as they are; only a read that worked takes a thread off.
func TestCodexReadFailureKeepsTiles(t *testing.T) {
	was := recentThreads
	t.Cleanup(func() { recentThreads = was })
	recentThreads = func(time.Duration) ([]codex.Thread, error) { return nil, errors.New("database is locked") }

	d := &Daemon{Store: store.New("", "test", "0")}
	id := codex.AgentID("t1")
	d.Store.Add(&store.Entry{External: true, A: model.Agent{ID: id, Tool: "codex", External: true, Status: model.Idle}})
	logged := false
	d.pollCodex(&logged)
	if _, ok := d.Store.Get(id); !ok {
		t.Fatal("a failed read took the Codex tile off")
	}
	if !logged {
		t.Fatal("the failure isn't logged")
	}
	recentThreads = func(time.Duration) ([]codex.Thread, error) { return nil, nil }
	d.pollCodex(&logged)
	if _, ok := d.Store.Get(id); ok || logged {
		t.Fatalf("a read that worked: tile kept %v, logged %v", ok, logged)
	}
}
