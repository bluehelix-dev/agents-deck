package claude

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
)

// Workflow is the run of the Workflow tool a session has going: its phases
// in the script's order, with how many agents each has running and done.
// The run's journal (subagents/workflows/<run>/journal.jsonl) has every
// agent's start and result with its phase; the phases still ahead come from
// the script's meta, found through the Workflow call in the transcript. Nil
// when there is no run, or the last one has been quiet for longer than
// window with nothing running.
func Workflow(transcript string, window time.Duration) *model.Workflow {
	if transcript == "" {
		return nil
	}
	runs, _ := filepath.Glob(filepath.Join(strings.TrimSuffix(transcript, ".jsonl"), "subagents", "workflows", "*", "journal.jsonl"))
	var journal string
	var newest time.Time
	for _, j := range runs {
		if st, err := os.Stat(j); err == nil && st.ModTime().After(newest) {
			journal, newest = j, st.ModTime()
		}
	}
	if journal == "" || time.Since(newest) > abandoned {
		return nil
	}
	counts, order := readJournal(journal)
	if len(order) == 0 {
		return nil
	}
	name, titles := runScript(transcript, filepath.Base(filepath.Dir(journal)))
	if name == "" && titles == nil {
		name, titles = scriptPhases(transcript)
	}
	if name == "" {
		name = filepath.Base(filepath.Dir(journal))
	}
	wf := &model.Workflow{Name: name}
	seen := map[string]bool{}
	for _, t := range append(titles, order...) {
		if seen[t] {
			continue
		}
		seen[t] = true
		c := counts[t]
		wf.Phases = append(wf.Phases, model.WorkflowPhase{Title: t, Running: c.running, Done: c.done})
	}
	running := 0
	for _, p := range wf.Phases {
		running += p.Running
	}
	if running == 0 && time.Since(newest) > window {
		return nil // over, and shown long enough
	}
	return wf
}

// abandoned: a run whose journal hasn't moved for this long is over, whatever
// it last said (a session that died mid-run never writes its results).
const abandoned = 30 * time.Minute

type phaseCount struct{ running, done int }

// readJournal counts each phase's agents: started without a result are
// running, with one done. Phases come back in the order they first appear.
func readJournal(path string) (map[string]phaseCount, []string) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil
	}
	defer f.Close()
	counts := map[string]phaseCount{}
	var order []string
	phaseOf := map[string]string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var ev struct {
			Type    string `json:"type"`
			AgentID string `json:"agentId"`
			Phase   string `json:"phase"`
		}
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "started":
			if ev.Phase == "" {
				ev.Phase = "work"
			}
			if _, ok := counts[ev.Phase]; !ok {
				order = append(order, ev.Phase)
			}
			c := counts[ev.Phase]
			c.running++
			counts[ev.Phase] = c
			phaseOf[ev.AgentID] = ev.Phase
		case "result":
			p, ok := phaseOf[ev.AgentID]
			if !ok {
				continue
			}
			c := counts[p]
			if c.running > 0 {
				c.running--
			}
			c.done++
			counts[p] = c
			delete(phaseOf, ev.AgentID)
		}
	}
	return counts, order
}

var (
	metaName   = regexp.MustCompile(`name:\s*['"]([^'"\n]+)['"]`)
	metaPhases = regexp.MustCompile(`(?s)phases:\s*\[(.*?)\]`)
	phaseTitle = regexp.MustCompile(`title:\s*['"]([^'"\n]+)['"]`)
)

// scriptPhases finds the last Workflow call in the transcript's tail and
// reads the script's meta: its name and its phases' titles, in order. The
// script is the file `scriptPath` names, or the inline `script`.
func scriptPhases(transcript string) (name string, titles []string) {
	f, err := os.Open(transcript)
	if err != nil {
		return "", nil
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", nil
	}
	size := st.Size()
	off := max(0, size-512<<10)
	buf := make([]byte, size-off)
	n, _ := f.ReadAt(buf, off)
	lines := bytes.Split(buf[:n], []byte("\n"))
	var m struct {
		Type    string `json:"type"`
		Message *struct {
			Content []struct {
				Type  string `json:"type"`
				Name  string `json:"name"`
				Input struct {
					ScriptPath string `json:"scriptPath"`
					Script     string `json:"script"`
				} `json:"input"`
			} `json:"content"`
		} `json:"message"`
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if !bytes.Contains(lines[i], []byte(`"Workflow"`)) {
			continue
		}
		m.Type, m.Message = "", nil
		if json.Unmarshal(lines[i], &m) != nil || m.Type != "assistant" || m.Message == nil {
			continue
		}
		for _, c := range m.Message.Content {
			if c.Type != "tool_use" || c.Name != "Workflow" {
				continue
			}
			src := c.Input.Script
			if src == "" && c.Input.ScriptPath != "" {
				if b, err := os.ReadFile(c.Input.ScriptPath); err == nil && len(b) < 4<<20 {
					src = string(b)
				}
			}
			return metaOf(src)
		}
	}
	return "", nil
}

// runScript reads the meta off the copy of the script Claude Code keeps for
// each run (<session>/workflows/scripts/<name>-<run>.js). It stays when the
// Workflow call has long scrolled out of a busy transcript's tail.
func runScript(transcript, run string) (name string, titles []string) {
	files, _ := filepath.Glob(filepath.Join(strings.TrimSuffix(transcript, ".jsonl"), "workflows", "scripts", "*-"+run+".js"))
	if len(files) != 1 {
		return "", nil
	}
	b, err := os.ReadFile(files[0])
	if err != nil || len(b) > 4<<20 {
		return "", nil
	}
	return metaOf(string(b))
}

// metaOf reads `export const meta = { name, phases: [{title}] }` off a
// workflow script, without running it.
func metaOf(src string) (name string, titles []string) {
	if src == "" {
		return "", nil
	}
	head := src
	if len(head) > 64<<10 {
		head = head[:64<<10]
	}
	if m := metaName.FindStringSubmatch(head); m != nil {
		name = m[1]
	}
	if m := metaPhases.FindStringSubmatch(head); m != nil {
		for _, t := range phaseTitle.FindAllStringSubmatch(m[1], -1) {
			titles = append(titles, t[1])
		}
	}
	return name, titles
}
