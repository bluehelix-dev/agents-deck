package claude

import (
	"bytes"
	"encoding/json"
	"os"
)

// PendingToolCall is the tool call a session's last turn stopped at and has
// no result for yet: an AskUserQuestion the user hasn't answered, or a tool
// waiting for permission (or still running: the caller tells those apart by
// what Claude Code reports). It returns the call's id, tool and input, and
// false when the last message is anything else (a final reply, a tool result,
// the user's own prompt).
func PendingToolCall(path string) (id, tool string, input json.RawMessage, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", nil, false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", "", nil, false
	}
	size := st.Size()
	off := max(0, size-256<<10)
	buf := make([]byte, size-off)
	n, _ := f.ReadAt(buf, off)
	lines := bytes.Split(bytes.TrimSpace(buf[:n]), []byte("\n"))
	var m struct {
		Type    string `json:"type"`
		Message *struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	type block struct {
		Type      string          `json:"type"`
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Input     json.RawMessage `json:"input"`
		ToolUseID string          `json:"tool_use_id"`
	}
	answered := map[string]bool{}
	for i := len(lines) - 1; i >= 0; i-- {
		m.Type, m.Message = "", nil
		if json.Unmarshal(lines[i], &m) != nil || m.Message == nil {
			continue
		}
		if m.Type != "assistant" && m.Type != "user" {
			continue
		}
		var blocks []block
		if json.Unmarshal(m.Message.Content, &blocks) != nil {
			// A plain string: the user typed something. The turn moved on.
			return "", "", nil, false
		}
		if m.Type == "user" {
			results := 0
			for _, b := range blocks {
				if b.Type == "tool_result" {
					answered[b.ToolUseID] = true
					results++
				}
			}
			if results == 0 {
				return "", "", nil, false // a new prompt from the user
			}
			continue // results for the calls above: keep reading
		}
		// The last assistant message: a call of its own without a result?
		for _, b := range blocks {
			if b.Type == "tool_use" && !answered[b.ID] {
				return b.ID, b.Name, b.Input, true
			}
		}
		return "", "", nil, false
	}
	return "", "", nil, false
}
