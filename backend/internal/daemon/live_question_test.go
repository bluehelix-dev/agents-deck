package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/claude"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/store"
	"github.com/brushknight/agents-deck/backend/internal/tmux"
)

// A watched session that stops at a question shows the question and its
// choices on the deck, waiting; once it is answered in its own terminal the
// deck moves on. A tool call it stops at while Claude Code reports it idle
// is a permission prompt; while busy, a tool that is simply running.
func TestWatchedSessionQuestionAndPermission(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGENTSTERM_HOME", filepath.Join(root, "home"))
	// The default account's transcripts live under ~/.claude: a home of our own.
	t.Setenv("USERPROFILE", filepath.Join(root, "user"))
	t.Setenv("HOME", filepath.Join(root, "user"))
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	_ = os.MkdirAll(filepath.Join(root, "home"), 0o700)
	_ = os.MkdirAll(filepath.Join(root, "user"), 0o700)
	old := tmux.Socket
	tmux.Socket = fmt.Sprintf("agentsdeck-test-q%d", os.Getpid())
	t.Cleanup(func() { tmux.Socket = old })

	id := "8a7b8c90-1111-4222-8333-944455556666"
	work := filepath.Join(root, "work")
	_ = os.MkdirAll(work, 0o700)
	dir := claude.ProjectDirFor(work)
	_ = os.MkdirAll(dir, 0o700)
	tr := filepath.Join(dir, id+".jsonl")
	write := func(lines ...string) {
		f, _ := os.OpenFile(tr, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		for _, l := range lines {
			fmt.Fprintln(f, l)
		}
		f.Close()
	}
	// What `claude agents --json` says, switchable between busy and idle.
	fake := filepath.Join(root, "claude")
	if runtime.GOOS == "windows" {
		fake += ".cmd" // no shebangs: a batch file instead
	}
	listing := func(status string) {
		cwd := work
		if runtime.GOOS == "windows" {
			cwd = filepath.ToSlash(work)
		}
		list := `[{"id":"ab12cd34","cwd":"` + cwd + `","kind":"interactive","startedAt":1791446002609,"sessionId":"` + id + `","name":"the app","pid":4242,"status":"` + status + `"}]`
		script := "#!/bin/sh\necho '" + list + "'\n"
		if runtime.GOOS == "windows" {
			script = "@echo off\r\necho " + list + "\r\n"
		}
		if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	listing("busy")
	write(`{"type":"user","cwd":"`+work+`","message":{"role":"user","content":"make it blue"}}`,
		`{"type":"assistant","message":{"id":"m1","model":"claude-opus-5-5","content":[{"type":"tool_use","id":"t1","name":"AskUserQuestion","input":{"questions":[{"question":"Which blue?","header":"Colour","options":[{"label":"sky","description":"light"},{"label":"navy"}]}]}}],"usage":{"input_tokens":10,"output_tokens":5}}}`)

	d := &Daemon{Store: store.New("", "test", "0"), Cfg: Config{LiveAutoAdd: true}}
	d.live.claudeBin = fake
	d.live.reportedAt = d.live.reportedAt.Add(-1) // no cache
	d.syncLive()
	e, ok := d.Store.Get(liveAgentID(id))
	if !ok {
		t.Fatal("not on the deck")
	}
	if e.A.Status != model.Waiting || e.A.Waiting == nil || e.A.Waiting.Kind != "question" {
		t.Fatalf("question not shown: %s %+v (transcript %s, wrote %s)", e.A.Status, e.A.Waiting, e.Transcript, tr)
	}
	if e.A.Waiting.Title != "which blue?" || len(e.A.Waiting.Options) != 2 || e.A.Waiting.Options[0].Label != "sky — light" {
		t.Fatalf("question prompt: %+v", e.A.Waiting)
	}
	first := e.A.Waiting.ID
	d.syncLive()
	if e, _ := d.Store.Get(liveAgentID(id)); e.A.Waiting == nil || e.A.Waiting.ID != first {
		t.Fatal("the same question should keep its prompt id")
	}

	// Answered at the terminal: Claude goes on with a tool, busy.
	write(`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"sky"}]}}`,
		`{"type":"assistant","message":{"id":"m2","model":"claude-opus-5-5","content":[{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"npm test"}}],"usage":{"input_tokens":20,"output_tokens":5}}}`)
	d.syncLive()
	if e, _ := d.Store.Get(liveAgentID(id)); e.A.Status != model.Running || e.A.Waiting != nil {
		t.Fatalf("a running tool while busy: %s %+v", e.A.Status, e.A.Waiting)
	}

	// Still at that call, but Claude Code says idle: it waits for permission.
	listing("idle")
	d.live.reportedAt = d.live.reportedAt.Add(-11e9)
	d.syncLive()
	if e, _ := d.Store.Get(liveAgentID(id)); e.A.Status != model.Waiting || e.A.Waiting == nil || e.A.Waiting.Kind != "permission" || e.A.Waiting.Detail != "npm test" {
		t.Fatalf("permission not shown: %s %+v", e.A.Status, e.A.Waiting)
	}

	// Allowed and finished: idle, nothing pending.
	write(`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t2","content":"ok"}]}}`,
		`{"type":"assistant","message":{"id":"m3","model":"claude-opus-5-5","stop_reason":"end_turn","content":[{"type":"text","text":"done"}],"usage":{"input_tokens":30,"output_tokens":5}}}`)
	d.live.reportedAt = d.live.reportedAt.Add(-11e9)
	d.syncLive()
	if e, _ := d.Store.Get(liveAgentID(id)); e.A.Status != model.Idle || e.A.Waiting != nil {
		t.Fatalf("after the turn: %s %+v", e.A.Status, e.A.Waiting)
	}
}
