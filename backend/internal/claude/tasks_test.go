package claude

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A background command (Bash run_in_background) or a Monitor is open from
// the tool result that names its task until the <task-notification> with
// that id, delivered as a message or queued as an attachment; an old launch
// nobody reported on doesn't count.
func TestBackgroundTasks(t *testing.T) {
	dir := t.TempDir()
	tr := filepath.Join(dir, "s.jsonl")
	if tasks := BackgroundTasks(tr, time.Hour); tasks != nil {
		t.Fatalf("no transcript: %v", tasks)
	}
	ts := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	use := `{"type":"assistant","timestamp":"` + ts + `","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"go  test ./...\n","run_in_background":true}}]}}`
	launched := `{"type":"user","timestamp":"` + ts + `","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"Command running in background with ID: b1x. Output is being written to: /tmp/b1x.output. You will be notified when it completes."}]}}`
	final := `{"type":"assistant","message":{"stop_reason":"end_turn","content":[{"type":"text","text":"I'll report when it is done."}]}}`
	write := func(lines ...string) {
		var b []byte
		for _, l := range lines {
			b = append(b, l...)
			b = append(b, '\n')
		}
		if err := os.WriteFile(tr, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(use, launched, final)
	tasks := BackgroundTasks(tr, time.Hour)
	if len(tasks) != 1 || tasks[0].ID != "b1x" || tasks[0].Tool != "Bash" || tasks[0].Detail != "in the background · go test ./..." {
		t.Fatalf("launched: %+v", tasks)
	}
	if TasksDetail(tasks) != tasks[0].Detail {
		t.Fatalf("one task's detail: %q", TasksDetail(tasks))
	}
	// A Monitor on top: its own description, open until it expires.
	mon := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t2","name":"Monitor","input":{"command":"until grep -q done log; do sleep 5; done","description":"Wait for the full run"}}]}}`
	monUp := `{"type":"user","timestamp":"` + ts + `","message":{"content":[{"type":"tool_result","tool_use_id":"t2","content":[{"type":"text","text":"Monitor started (task m2y, expires in 20m unless the source ends first; you get one notice at expiry)."}]}]}}`
	write(use, launched, mon, monUp, final)
	tasks = BackgroundTasks(tr, time.Hour)
	if len(tasks) != 2 || tasks[1].ID != "m2y" || tasks[1].Detail != "watching in the background · Wait for the full run" {
		t.Fatalf("with a monitor: %+v", tasks)
	}
	if TasksDetail(tasks) != "2 commands in the background" {
		t.Fatalf("two tasks: %q", TasksDetail(tasks))
	}
	// The command reports back: as a message for the Monitor, queued as an
	// attachment for the Bash task. Both are closed.
	notice := `{"type":"user","timestamp":"` + ts + `","message":{"content":"<task-notification>\n<task-id>m2y</task-id>\n<status>completed</status>\n</task-notification>"}}`
	queued := `{"type":"attachment","attachment":{"type":"queued_command","prompt":"<task-notification>\n<task-id>b1x</task-id>\n<status>completed</status>\n</task-notification>"}}`
	write(use, launched, mon, monUp, final, notice)
	if tasks = BackgroundTasks(tr, time.Hour); len(tasks) != 1 || tasks[0].ID != "b1x" {
		t.Fatalf("monitor reported: %+v", tasks)
	}
	write(use, launched, mon, monUp, final, notice, queued)
	if tasks = BackgroundTasks(tr, time.Hour); tasks != nil {
		t.Fatalf("all reported: %+v", tasks)
	}
	// A launch older than maxAge is forgotten; a Monitor past its expiry too.
	old := time.Now().Add(-30 * time.Minute).UTC().Format(time.RFC3339Nano)
	oldLaunch := `{"type":"user","timestamp":"` + old + `","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"Command running in background with ID: b1x. Output is being written to: /tmp/b1x.output."}]}}`
	oldMon := `{"type":"user","timestamp":"` + old + `","message":{"content":[{"type":"tool_result","tool_use_id":"t2","content":"Monitor started (task m2y, expires in 20m unless the source ends first)."}]}}`
	write(use, oldLaunch, mon, oldMon, final)
	if tasks = BackgroundTasks(tr, 10*time.Minute); tasks != nil {
		t.Fatalf("stale: %+v", tasks)
	}
	if tasks = BackgroundTasks(tr, time.Hour); len(tasks) != 1 || tasks[0].ID != "b1x" {
		t.Fatalf("the monitor expired, the command is within its age: %+v", tasks)
	}
}
