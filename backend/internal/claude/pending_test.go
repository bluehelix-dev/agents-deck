package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPendingToolCall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	write := func(lines ...string) {
		f, _ := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		_, _ = f.WriteString(strings.Join(lines, "\n") + "\n")
		f.Close()
	}
	pending := func() (string, bool) {
		_, tool, _, ok := PendingToolCall(path)
		return tool, ok
	}

	write(`{"type":"user","message":{"role":"user","content":"make it blue"}}`)
	if _, ok := pending(); ok {
		t.Fatal("the user's own prompt is not a pending call")
	}
	// A question Claude asks and stops at.
	write(`{"type":"api-request-shape","x":1}`,
		`{"type":"assistant","message":{"stop_reason":"tool_use","content":[{"type":"text","text":"which?"},{"type":"tool_use","id":"t1","name":"AskUserQuestion","input":{"questions":[{"question":"which blue?","options":[{"label":"sky"},{"label":"navy"}]}]}}]}}`)
	id, tool, input, ok := PendingToolCall(path)
	if !ok || tool != "AskUserQuestion" || id != "t1" || !strings.Contains(string(input), "navy") {
		t.Fatalf("question pending: %v %q %q %s", ok, id, tool, input)
	}
	// Answered: the result arrives, Claude goes on.
	write(`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"sky"}]}}`)
	if _, ok := pending(); ok {
		t.Fatal("answered question still pending")
	}
	// Two calls at once, one result in: the other is still open.
	write(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t2","name":"Read","input":{"file_path":"a"}},{"type":"tool_use","id":"t3","name":"Bash","input":{"command":"ls"}}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t2","content":"x"}]}}`)
	if tool, ok := pending(); !ok || tool != "Bash" {
		t.Fatalf("open call: %v %q", ok, tool)
	}
	write(`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t3","content":"y"}]}}`,
		`{"type":"assistant","message":{"stop_reason":"end_turn","content":[{"type":"text","text":"done"}]}}`)
	if _, ok := pending(); ok {
		t.Fatal("a final reply is not a pending call")
	}
	if _, _, _, ok := PendingToolCall(filepath.Join(t.TempDir(), "missing.jsonl")); ok {
		t.Fatal("missing file")
	}
}
