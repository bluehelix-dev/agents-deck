package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/claude"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

// A session whose turn ended with a command running in the background shows
// working for it, with the command, until Claude Code reports the command
// back; then it is idle again, as its hooks left it.
func TestBackgroundCommandKeepsASessionWorking(t *testing.T) {
	d := &Daemon{Store: store.New("", "test", "0")}
	tr := filepath.Join(t.TempDir(), "s.jsonl")
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	lines := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"pytest -q tests","run_in_background":true}}]}}
{"type":"user","timestamp":"` + ts + `","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"Command running in background with ID: b1x. Output is being written to: /tmp/b1x.output."}]}}
{"type":"assistant","message":{"stop_reason":"end_turn","content":[{"type":"text","text":"Running; I'll report."}]}}
`
	if err := os.WriteFile(tr, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	e := store.Entry{Transcript: tr, A: model.Agent{ID: "l1", Tool: "claude", Status: model.Idle, Unseen: true}}
	if !d.subagents(&e) || e.A.Status != model.Running || !e.Background || !claude.IsTaskActivity(e.A.Activity) || e.A.Activity.Detail != "in the background · pytest -q tests" {
		t.Fatalf("command in the background: %s %+v", e.A.Status, e.A.Activity)
	}
	if d.subagents(&e) {
		t.Fatal("nothing changed: no change")
	}
	// Reported back (queued for Claude's next turn): idle until its hooks say
	// otherwise.
	notice := `{"type":"attachment","attachment":{"type":"queued_command","prompt":"<task-notification>\n<task-id>b1x</task-id>\n<status>completed</status>\n</task-notification>"}}` + "\n"
	if err := os.WriteFile(tr, []byte(lines+notice), 0o600); err != nil {
		t.Fatal(err)
	}
	if !d.subagents(&e) || e.A.Status != model.Idle || e.Background || e.A.Activity != nil {
		t.Fatalf("reported back: %s %+v background=%v", e.A.Status, e.A.Activity, e.Background)
	}
}
