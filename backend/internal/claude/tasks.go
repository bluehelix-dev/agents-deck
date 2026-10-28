package claude

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
)

// Task is a background task of a session that Claude Code has not reported
// back yet: a Bash command run in the background, or a Monitor watch. Claude's
// turn ends while it runs, and a <task-notification> with the task's id wakes
// the session when it is done.
type Task struct {
	ID     string
	Tool   string // the activity shown for it: "Bash"
	Detail string // "in the background · go test ./..." or "watching in the background · the build"
}

// TaskActivity is the mark of an activity BackgroundTasks' tasks set.
const TaskActivity = "in the background"

var (
	bgLaunch   = regexp.MustCompile(`Command running in background with ID: ([A-Za-z0-9_-]+)`)
	monLaunch  = regexp.MustCompile(`Monitor started \(task ([A-Za-z0-9_-]+), expires in (\d+)m`)
	taskNotice = regexp.MustCompile(`<task-id>([A-Za-z0-9_-]+)</task-id>`)
)

// IsTaskActivity tells an activity set for background tasks ("in the
// background · …", "2 commands in the background") from a tool call of the
// session's own.
func IsTaskActivity(a *model.Activity) bool {
	return a != nil && a.Tool == "Bash" && strings.Contains(a.Detail, TaskActivity)
}

// BackgroundTasks lists the open background tasks at the end of a session's
// transcript, oldest first: launched (the tool's result names the task) and
// not reported back (no <task-notification> with the task's id, delivered as
// a message or queued as an attachment). A Bash task is open for maxAge
// after its launch, a Monitor until it expires: a session Claude Code has
// left can't keep a deck working forever.
func BackgroundTasks(transcript string, maxAge time.Duration) []Task {
	if transcript == "" {
		return nil
	}
	buf := lastBytes(transcript, 256<<10)
	if len(buf) == 0 {
		return nil
	}
	type open struct {
		t     Task
		until time.Time
	}
	var opens []open
	closed := map[string]bool{}
	inputs := map[string]json.RawMessage{} // tool_use id -> input, for the command
	var m struct {
		Type       string `json:"type"`
		Timestamp  string `json:"timestamp"`
		Message    *struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
		Attachment *struct {
			Prompt string `json:"prompt"`
		} `json:"attachment"`
	}
	type block struct {
		Type      string          `json:"type"`
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Input     json.RawMessage `json:"input"`
		ToolUseID string          `json:"tool_use_id"`
		Text      string          `json:"text"`
		Content   json.RawMessage `json:"content"`
	}
	now := time.Now()
	for _, line := range bytes.Split(buf, []byte("\n")) {
		m.Type, m.Timestamp, m.Message, m.Attachment = "", "", nil, nil
		if json.Unmarshal(line, &m) != nil {
			continue
		}
		switch m.Type {
		case "attachment": // a notification queued for Claude's next turn
			if m.Attachment != nil {
				for _, h := range taskNotice.FindAllStringSubmatch(m.Attachment.Prompt, -1) {
					closed[h[1]] = true
				}
			}
		case "assistant":
			if m.Message == nil {
				continue
			}
			var blocks []block
			if json.Unmarshal(m.Message.Content, &blocks) != nil {
				continue
			}
			for _, b := range blocks {
				if b.Type == "tool_use" && (b.Name == "Bash" || b.Name == "Monitor") {
					inputs[b.ID] = b.Input
				}
			}
		case "user":
			if m.Message == nil {
				continue
			}
			at := now
			if t, err := time.Parse(time.RFC3339Nano, m.Timestamp); err == nil {
				at = t
			}
			var text string
			if json.Unmarshal(m.Message.Content, &text) == nil {
				for _, h := range taskNotice.FindAllStringSubmatch(text, -1) {
					closed[h[1]] = true
				}
				continue
			}
			var blocks []block
			if json.Unmarshal(m.Message.Content, &blocks) != nil {
				continue
			}
			for _, b := range blocks {
				switch b.Type {
				case "text":
					for _, h := range taskNotice.FindAllStringSubmatch(b.Text, -1) {
						closed[h[1]] = true
					}
				case "tool_result":
					s := resultText(b.Content)
					if h := bgLaunch.FindStringSubmatch(s); h != nil {
						detail := TaskActivity
						if cmd := ToolDetail("Bash", inputs[b.ToolUseID], ""); cmd != "" {
							detail += " · " + cmd
						}
						opens = append(opens, open{Task{ID: h[1], Tool: "Bash", Detail: detail}, at.Add(maxAge)})
					} else if h := monLaunch.FindStringSubmatch(s); h != nil {
						detail := "watching " + TaskActivity
						if what := clip(oneLine(field(inputs[b.ToolUseID], "description")), 120); what != "" {
							detail += " · " + what
						}
						mins, _ := strconv.Atoi(h[2])
						opens = append(opens, open{Task{ID: h[1], Tool: "Bash", Detail: detail}, at.Add(time.Duration(mins)*time.Minute + time.Minute)})
					}
				}
			}
		}
	}
	var out []Task
	for _, o := range opens {
		if !closed[o.t.ID] && now.Before(o.until) {
			out = append(out, o.t)
		}
	}
	return out
}

// TasksDetail is the activity detail for a session's open background tasks:
// the one task's own, or how many there are.
func TasksDetail(tasks []Task) string {
	if len(tasks) == 1 {
		return tasks[0].Detail
	}
	return fmt.Sprintf("%d commands %s", len(tasks), TaskActivity)
}

// resultText is the text of a tool result: a string, or text blocks.
func resultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var sb strings.Builder
	for _, b := range blocks {
		if b.Type == "text" {
			sb.WriteString(b.Text)
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// lastBytes reads the last n bytes of a file, from the first whole line.
func lastBytes(path string, n int64) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil
	}
	off := max(0, st.Size()-n)
	buf := make([]byte, st.Size()-off)
	k, _ := f.ReadAt(buf, off)
	buf = buf[:k]
	if off > 0 {
		if i := bytes.IndexByte(buf, '\n'); i >= 0 {
			buf = buf[i+1:]
		}
	}
	return buf
}
