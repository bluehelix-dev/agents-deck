package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWorkflow(t *testing.T) {
	dir := t.TempDir()
	tr := filepath.Join(dir, "0b5c2f4e-1111-4222-8333-944455556666.jsonl")
	if wf := Workflow(tr, time.Minute); wf != nil {
		t.Fatalf("no run: %+v", wf)
	}
	// The script with its meta, and the call to it in the transcript.
	script := filepath.Join(dir, "bughunt.mjs")
	if err := os.WriteFile(script, []byte(`export const meta = {
  name: 'bughunt',
  description: 'x',
  phases: [
    { title: 'Scout', detail: 'one' },
    { title: 'Suche', detail: 'many' },
    { title: 'Jury', detail: 'three each' },
    { title: 'Kritiker' },
  ],
}
const DIMENSIONS = [{ title: 'not a phase' }]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	call := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Workflow","input":{"scriptPath":` + jsonString(script) + `,"args":{}}}]}}`
	if err := os.WriteFile(tr, []byte(`{"type":"user","message":{"content":"hunt"}}`+"\n"+call+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run := filepath.Join(strings.TrimSuffix(tr, ".jsonl"), "subagents", "workflows", "wf_1")
	if err := os.MkdirAll(run, 0o700); err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(run, "journal.jsonl")
	write := func(lines ...string) {
		f, _ := os.OpenFile(journal, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		_, _ = f.WriteString(strings.Join(lines, "\n") + "\n")
		f.Close()
	}
	write(`{"type":"launched"}`,
		`{"type":"started","agentId":"a1","label":"scout","phase":"Scout"}`,
		`{"type":"result","agentId":"a1","result":{}}`,
		`{"type":"started","agentId":"b1","label":"find:x","phase":"Suche"}`,
		`{"type":"started","agentId":"b2","label":"find:y","phase":"Suche"}`,
		`{"type":"result","agentId":"b1","result":{}}`)
	wf := Workflow(tr, time.Minute)
	if wf == nil || wf.Name != "bughunt" {
		t.Fatalf("workflow: %+v", wf)
	}
	want := []string{"Scout", "Suche", "Jury", "Kritiker"}
	if len(wf.Phases) != len(want) {
		t.Fatalf("phases: %+v", wf.Phases)
	}
	for i, p := range wf.Phases {
		if p.Title != want[i] {
			t.Fatalf("phase %d: %+v", i, p)
		}
	}
	if p := wf.Phases[0]; p.Running != 0 || p.Done != 1 {
		t.Fatalf("scout: %+v", p)
	}
	if p := wf.Phases[1]; p.Running != 1 || p.Done != 1 {
		t.Fatalf("suche: %+v", p)
	}
	if p := wf.Phases[2]; p.Running != 0 || p.Done != 0 {
		t.Fatalf("jury ahead: %+v", p)
	}

	// A phase the script doesn't name still shows, after the script's.
	write(`{"type":"result","agentId":"b2","result":{}}`,
		`{"type":"started","agentId":"c1","label":"extra","phase":"Nachlese"}`)
	wf = Workflow(tr, time.Minute)
	if len(wf.Phases) != 5 || wf.Phases[4].Title != "Nachlese" || wf.Phases[4].Running != 1 {
		t.Fatalf("extra phase: %+v", wf.Phases)
	}

	// All done and quiet past the window: gone. Fresh and done: still shown.
	write(`{"type":"result","agentId":"c1","result":{}}`)
	if wf := Workflow(tr, time.Minute); wf == nil {
		t.Fatal("a run that just finished should still show")
	}
	old := time.Now().Add(-5 * time.Minute)
	if err := os.Chtimes(journal, old, old); err != nil {
		t.Fatal(err)
	}
	if wf := Workflow(tr, time.Minute); wf != nil {
		t.Fatalf("a finished run past the window: %+v", wf)
	}

	// No script to read: the journal's own order, the run's id as the name.
	if err := os.Remove(script); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	_ = os.Chtimes(journal, now, now)
	write(`{"type":"started","agentId":"d1","label":"again","phase":"Suche"}`)
	wf = Workflow(tr, time.Minute)
	if wf == nil || wf.Name != "wf_1" || wf.Phases[0].Title != "Scout" || wf.Phases[1].Title != "Suche" || len(wf.Phases) != 3 {
		t.Fatalf("without the script: %+v", wf)
	}
}

func TestMetaOfInlineScript(t *testing.T) {
	name, titles := metaOf("export const meta = {\n  name: \"review\",\n  description: 'd',\n  phases: [{ title: \"Review\" }, { title: \"Verify\" }],\n}\n")
	if name != "review" || len(titles) != 2 || titles[1] != "Verify" {
		t.Fatalf("%q %v", name, titles)
	}
	if n, ts := metaOf(""); n != "" || ts != nil {
		t.Fatal("empty")
	}
}

func jsonString(s string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}
