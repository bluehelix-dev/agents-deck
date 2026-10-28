package daemon

import (
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/claude"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

// A hand-started session with the deck's hooks installed reports by session
// id: its permission prompts show on the deck, and from then on its hooks set
// its status (liveStatus stays out). An event without an agent id for one of
// the deck's own agents is the duplicate through the user settings: dropped.
func TestWatchedSessionHooks(t *testing.T) {
	d := &Daemon{Store: store.New("", "test", "0")}
	sid := "9a7b8c90-1111-4222-8333-944455556666"
	live := &store.Entry{External: true, SessionID: sid, A: model.Agent{ID: "l1", Tool: "claude", External: true, Status: model.Idle, Cwd: "/w/app"}}
	d.Store.Add(live)
	own := &store.Entry{SessionID: "own-" + sid, TmuxName: "t1", A: model.Agent{ID: "o1", Tool: "claude", Status: model.Idle}}
	d.Store.Add(own)

	ev := `{"hook_event_name":"PermissionRequest","session_id":"` + sid + `","cwd":"/w/app","tool_name":"Bash","tool_input":{"command":"npm test"}}`
	if err := d.Hook("", []byte(ev)); err != nil {
		t.Fatal(err)
	}
	e, _ := d.Store.Get("l1")
	if e.A.Status != model.Waiting || e.A.Waiting == nil || e.A.Waiting.Kind != "permission" || e.A.Waiting.Detail != "npm test" {
		t.Fatalf("permission not shown: %s %+v", e.A.Status, e.A.Waiting)
	}
	if e.A.Waiting.Context != "bash · /w/app" {
		t.Fatalf("context: %q", e.A.Waiting.Context)
	}
	if !e.LiveHooks {
		t.Fatal("the session should be marked as reporting through hooks")
	}
	// The transcript-based status leaves it alone now.
	if d.liveStatus(&e, claude.Live{Reported: true, Busy: false}) {
		t.Fatal("liveStatus changed a hooked session")
	}
	// Except for a compaction that never ended: Claude Code compacts idle
	// sessions by itself, and when its PostCompact gets lost the tile would
	// show "compacting" for good. Reported idle for over a minute ends it.
	for _, ev := range []string{
		`{"hook_event_name":"PostToolUse","session_id":"` + sid + `","tool_name":"Bash","tool_input":{"command":"npm test"}}`,
		`{"hook_event_name":"Stop","session_id":"` + sid + `"}`,
		`{"hook_event_name":"PreCompact","session_id":"` + sid + `","trigger":"auto"}`,
	} {
		if err := d.Hook("", []byte(ev)); err != nil {
			t.Fatal(err)
		}
	}
	e, _ = d.Store.Get("l1")
	if e.A.Status != model.Running || e.A.Activity == nil || e.A.Activity.Tool != claude.CompactTool {
		t.Fatalf("compacting: %s %+v", e.A.Status, e.A.Activity)
	}
	if d.liveStatus(&e, claude.Live{Reported: true, Busy: false}) {
		t.Fatal("a compaction that just began is not stale")
	}
	e.CompactSince = time.Now().Add(-2 * time.Minute)
	if !d.liveStatus(&e, claude.Live{Reported: true, Busy: false}) || e.A.Status != model.Idle || e.A.Activity != nil {
		t.Fatalf("stale compaction of an idle session: %s %+v", e.A.Status, e.A.Activity)
	}
	// (The copy above went idle; the stored one ends its compaction by the
	// PostCompact hook, as Claude Code 2.1.3xx sends it.)
	if err := d.Hook("", []byte(`{"hook_event_name":"PostCompact","session_id":"`+sid+`","trigger":"auto"}`)); err != nil {
		t.Fatal(err)
	}
	if e, _ := d.Store.Get("l1"); e.A.Status != model.Idle {
		t.Fatalf("after PostCompact: %s", e.A.Status)
	}
	// A background subagent of the idle session reports through the same
	// hooks: the session shows working for it, and goes idle again once no
	// subagent transcript is being written (none here).
	ev = `{"hook_event_name":"PreToolUse","session_id":"` + sid + `","agent_id":"a1","tool_name":"Bash","tool_input":{"command":"go test"}}`
	if err := d.Hook("", []byte(ev)); err != nil {
		t.Fatal(err)
	}
	e, _ = d.Store.Get("l1")
	if e.A.Status != model.Running || e.A.Activity == nil || e.A.Activity.Tool != claude.SubagentTool || !e.Background {
		t.Fatalf("delegating: %s %+v", e.A.Status, e.A.Activity)
	}
	if !d.subagents(&e) || e.A.Status != model.Idle || e.A.Activity != nil {
		t.Fatalf("no subagent writing: %s %+v", e.A.Status, e.A.Activity)
	}
	// A session running by Claude's own account shows its subagents the same
	// way, and keeps running (as Claude says) once they are done.
	e.SetStatus(model.Running)
	e.A.Activity = &model.Activity{Tool: claude.SubagentTool, Detail: "2 subagents working"}
	if !d.subagents(&e) || e.A.Status != model.Running || e.A.Activity != nil {
		t.Fatalf("subagents done: %s %+v", e.A.Status, e.A.Activity)
	}
	// Back to a turn with a prompt, for the rest of the test.
	ev = `{"hook_event_name":"PermissionRequest","session_id":"` + sid + `","cwd":"/w/app","tool_name":"Bash","tool_input":{"command":"npm test"}}`
	if err := d.Hook("", []byte(ev)); err != nil {
		t.Fatal(err)
	}
	// Allowed: PostToolUse clears the prompt, Stop ends the turn.
	for _, ev := range []string{
		`{"hook_event_name":"PostToolUse","session_id":"` + sid + `","tool_name":"Bash","tool_input":{"command":"npm test"}}`,
		`{"hook_event_name":"Stop","session_id":"` + sid + `"}`,
	} {
		if err := d.Hook("", []byte(ev)); err != nil {
			t.Fatal(err)
		}
	}
	if e, _ := d.Store.Get("l1"); e.A.Status != model.Idle || e.A.Waiting != nil {
		t.Fatalf("after the turn: %s %+v", e.A.Status, e.A.Waiting)
	}

	// The deck's own agent, reported without its id: ignored, not changed.
	dup := `{"hook_event_name":"PermissionRequest","session_id":"own-` + sid + `","tool_name":"Bash","tool_input":{"command":"rm -rf x"}}`
	if err := d.Hook("", []byte(dup)); err != nil {
		t.Fatal(err)
	}
	if e, _ := d.Store.Get("o1"); e.A.Status != model.Idle || e.LiveHooks {
		t.Fatalf("own agent touched by the duplicate: %s %v", e.A.Status, e.LiveHooks)
	}
	// Unknown session: nothing, no error (the hook must never fail a session).
	if err := d.Hook("", []byte(`{"hook_event_name":"Stop","session_id":"nobody"}`)); err != nil {
		t.Fatal(err)
	}
}

// A turn that ends says "ready" until you look at it. Only a terminal the deck
// knows to be in front counts as looking: on Windows it knows none, and a
// watched session has no tmux name, so neither may pass for "seen".
func TestStopLeavesASessionReady(t *testing.T) {
	d := &Daemon{Store: store.New("", "test", "0")}
	sid := "9a7b8c90-1111-4222-8333-944455556667"
	d.Store.Add(&store.Entry{External: true, SessionID: sid, A: model.Agent{ID: "l1", Tool: "claude", External: true, Status: model.Idle}})
	d.Store.Add(&store.Entry{SessionID: "own-" + sid, TmuxName: "t1", A: model.Agent{ID: "o1", Tool: "claude", Status: model.Idle}})
	// A watched session's hooks find it by session; the deck's own agent
	// sends its id.
	turn := func(agent, session string) {
		for _, name := range []string{"UserPromptSubmit", "Stop"} {
			ev := `{"hook_event_name":"` + name + `","session_id":"` + session + `","prompt":"go"}`
			if err := d.Hook(agent, []byte(ev)); err != nil {
				t.Fatal(err)
			}
		}
	}
	turn("", sid)
	if e, _ := d.Store.Get("l1"); e.A.Status != model.Idle || !e.A.Unseen {
		t.Fatalf("watched session, no terminal known in front: want ready, got %s unseen=%v", e.A.Status, e.A.Unseen)
	}
	turn("o1", "own-"+sid)
	if e, _ := d.Store.Get("o1"); !e.A.Unseen {
		t.Fatal("own session, no terminal known in front: want ready")
	}
	// Its tmux window in front (the Mac): you watched it finish.
	d.front = "t1"
	turn("o1", "own-"+sid)
	if e, _ := d.Store.Get("o1"); e.A.Unseen {
		t.Fatal("finished in front of you: want done, not ready")
	}
}
