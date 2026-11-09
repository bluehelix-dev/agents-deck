package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/server"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

const holdSID = "9a7b8c90-1111-4222-8333-944455557777"

// A deck with something connected that could answer, and a watched session.
func holdDaemon(t *testing.T, waitSeconds int) *Daemon {
	t.Helper()
	d := &Daemon{Store: store.New("", "test", "0"), Cfg: Config{AnswerWaitSeconds: waitSeconds}, Answerers: func() int { return 1 }}
	d.Store.Add(&store.Entry{External: true, SessionID: holdSID, A: model.Agent{ID: "l1", Tool: "claude", External: true, Status: model.Idle, Cwd: "/w/app"}})
	return d
}

// hookAsync runs the hook as Claude Code would: blocked until the daemon
// answers it. It returns the channel the hook's output arrives on.
func hookAsync(d *Daemon, ev string) <-chan []byte {
	ch := make(chan []byte, 1)
	go func() {
		out, _ := d.Hook(context.Background(), "", []byte(ev))
		ch <- out
	}()
	return ch
}

// awaitPrompt waits for the watched session to show a prompt the deck can
// answer, and returns it.
func awaitPrompt(t *testing.T, d *Daemon) model.Prompt {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
		if e, _ := d.Store.Get("l1"); e.A.Waiting != nil && !e.A.Waiting.Until.IsZero() {
			return *e.A.Waiting
		}
		time.Sleep(10 * time.Millisecond)
	}
	e, _ := d.Store.Get("l1")
	t.Fatalf("no prompt the deck can answer: %+v", e.A.Waiting)
	return model.Prompt{}
}

func hookOutput(t *testing.T, ch <-chan []byte) map[string]any {
	t.Helper()
	select {
	case out := <-ch:
		var m map[string]any
		if err := json.Unmarshal(out, &m); err != nil {
			t.Fatalf("hook output %q: %v", out, err)
		}
		hso, _ := m["hookSpecificOutput"].(map[string]any)
		if hso == nil {
			t.Fatalf("no hookSpecificOutput in %s", out)
		}
		return hso
	case <-time.After(3 * time.Second):
		t.Fatal("the hook is still waiting")
	}
	return nil
}

// A watched session's permission request waits for the deck: an answer there
// goes back as the hook's decision, and the tile moves on at once.
func TestHeldPermissionAnsweredFromDeck(t *testing.T) {
	d := holdDaemon(t, 10)
	perm := `{"hook_event_name":"PermissionRequest","session_id":"` + holdSID + `","cwd":"/w/app","tool_name":"Bash","tool_input":{"command":"npm test"},` +
		`"permission_suggestions":[{"type":"addRules","rules":[{"toolName":"Bash","ruleContent":"npm test"}],"behavior":"allow","destination":"localSettings"}]}`

	// yes
	ch := hookAsync(d, perm)
	p := awaitPrompt(t, d)
	if p.Kind != "permission" || time.Until(p.Until) < 5*time.Second {
		t.Fatalf("prompt: %+v", p)
	}
	if err := d.Answer("l1", p.ID, "1"); err != nil {
		t.Fatal(err)
	}
	hso := hookOutput(t, ch)
	if dec, _ := hso["decision"].(map[string]any); hso["hookEventName"] != "PermissionRequest" || dec["behavior"] != "allow" || dec["updatedPermissions"] != nil {
		t.Fatalf("yes: %v", hso)
	}
	if e, _ := d.Store.Get("l1"); e.A.Status != model.Running || e.A.Waiting != nil {
		t.Fatalf("after yes: %s %+v", e.A.Status, e.A.Waiting)
	}

	// yes, and don't ask again: Claude Code's suggested rules go back with it
	ch = hookAsync(d, perm)
	p = awaitPrompt(t, d)
	if err := d.Answer("l1", p.ID, "2"); err != nil {
		t.Fatal(err)
	}
	dec, _ := hookOutput(t, ch)["decision"].(map[string]any)
	rules, _ := dec["updatedPermissions"].([]any)
	if dec["behavior"] != "allow" || len(rules) != 1 {
		t.Fatalf("don't ask again: %v", dec)
	}

	// no: denied, and Claude stops as it does for "no" in the terminal
	ch = hookAsync(d, perm)
	p = awaitPrompt(t, d)
	if err := d.Answer("l1", p.ID, "3"); err != nil {
		t.Fatal(err)
	}
	dec, _ = hookOutput(t, ch)["decision"].(map[string]any)
	if dec["behavior"] != "deny" || dec["interrupt"] != true || dec["message"] == "" {
		t.Fatalf("no: %v", dec)
	}
	if e, _ := d.Store.Get("l1"); e.A.Status != model.Idle || e.A.Waiting != nil {
		t.Fatalf("after no: %s %+v", e.A.Status, e.A.Waiting)
	}

	// A stale prompt id or an unknown key is refused and the hook keeps waiting.
	ch = hookAsync(d, perm)
	p = awaitPrompt(t, d)
	if err := d.Answer("l1", "p0", "1"); err != server.ErrStalePrompt {
		t.Fatalf("stale: %v", err)
	}
	if err := d.Answer("l1", p.ID, "9"); err != server.ErrBadKey {
		t.Fatalf("bad key: %v", err)
	}
	select {
	case out := <-ch:
		t.Fatalf("the hook returned %q after refused answers", out)
	case <-time.After(100 * time.Millisecond):
	}
	if err := d.Answer("l1", p.ID, "1"); err != nil {
		t.Fatal(err)
	}
	hookOutput(t, ch)
}

// No answer from the deck in time: the hook returns nothing, Claude Code asks
// in the terminal, and the prompt stays on the deck to be answered there.
func TestHeldPermissionTimesOut(t *testing.T) {
	d := holdDaemon(t, 1)
	ch := hookAsync(d, `{"hook_event_name":"PermissionRequest","session_id":"`+holdSID+`","tool_name":"Bash","tool_input":{"command":"npm test"}}`)
	p := awaitPrompt(t, d)
	select {
	case out := <-ch:
		if out != nil {
			t.Fatalf("output without an answer: %q", out)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the hook never returned")
	}
	e, _ := d.Store.Get("l1")
	if e.A.Status != model.Waiting || e.A.Waiting == nil || e.A.Waiting.ID != p.ID || !e.A.Waiting.Until.IsZero() {
		t.Fatalf("after the wait: %s %+v", e.A.Status, e.A.Waiting)
	}
	// Too late for the deck: it says so, as for any watched session.
	if err := d.Answer("l1", p.ID, "1"); err != errLive {
		t.Fatalf("late answer: %v", err)
	}
}

// Nothing connected that could answer: the hook returns at once and the
// prompt carries no deadline.
func TestNoAnswererNoHold(t *testing.T) {
	d := holdDaemon(t, 10)
	d.Answerers = nil
	start := time.Now()
	out, err := d.Hook(context.Background(), "", []byte(`{"hook_event_name":"PermissionRequest","session_id":"`+holdSID+`","tool_name":"Bash","tool_input":{"command":"npm test"}}`))
	if err != nil || out != nil || time.Since(start) > time.Second {
		t.Fatalf("held without an answerer: %q %v", out, err)
	}
	if e, _ := d.Store.Get("l1"); e.A.Waiting == nil || !e.A.Waiting.Until.IsZero() {
		t.Fatalf("prompt: %+v", e.A.Waiting)
	}
	// A deck-started agent (not watched) is typed into, never held.
	d.Answerers = func() int { return 1 }
	d.Store.Add(&store.Entry{SessionID: "own", TmuxName: "t1", A: model.Agent{ID: "o1", Tool: "claude", Status: model.Idle}})
	out, _ = d.Hook(context.Background(), "o1", []byte(`{"hook_event_name":"PermissionRequest","session_id":"own","tool_name":"Bash","tool_input":{"command":"ls"}}`))
	if e, _ := d.Store.Get("o1"); out != nil || e.A.Waiting == nil || !e.A.Waiting.Until.IsZero() {
		t.Fatalf("own agent held: %q %+v", out, e.A.Waiting)
	}
}

// One pick-one question: the hook on the question tool waits, and the answer
// goes back as the tool's input with the answer filled in.
func TestHeldQuestionAnsweredFromDeck(t *testing.T) {
	d := holdDaemon(t, 10)
	ask := `{"hook_event_name":"PreToolUse","session_id":"` + holdSID + `","tool_name":"AskUserQuestion","tool_input":{"questions":[{"question":"Which database?","header":"Database","options":[{"label":"Postgres","description":"relational"},{"label":"SQLite","description":"a file"}],"multiSelect":false}]}}`
	ch := hookAsync(d, ask)
	p := awaitPrompt(t, d)
	if p.Kind != "question" || len(p.Options) != 2 {
		t.Fatalf("prompt: %+v", p)
	}
	if err := d.Answer("l1", p.ID, "2"); err != nil {
		t.Fatal(err)
	}
	hso := hookOutput(t, ch)
	in, _ := hso["updatedInput"].(map[string]any)
	answers, _ := in["answers"].(map[string]any)
	if hso["hookEventName"] != "PreToolUse" || hso["permissionDecision"] != "allow" || answers["Which database?"] != "SQLite" {
		t.Fatalf("answer: %v", hso)
	}
	if qs, _ := in["questions"].([]any); len(qs) != 1 {
		t.Fatalf("the questions must go back with the answers: %v", in)
	}
	if e, _ := d.Store.Get("l1"); e.A.Status != model.Running || e.A.Waiting != nil {
		t.Fatalf("after the answer: %s %+v", e.A.Status, e.A.Waiting)
	}
}

// A form (two questions, one pick-any) is walked on the deck step by step
// while the one hook waits; submitting the review sends every answer.
func TestHeldFormWalkedFromDeck(t *testing.T) {
	d := holdDaemon(t, 10)
	ask := `{"hook_event_name":"PreToolUse","session_id":"` + holdSID + `","tool_name":"AskUserQuestion","tool_input":{"questions":[` +
		`{"question":"Size?","header":"Size","options":[{"label":"S"},{"label":"L"}],"multiSelect":false},` +
		`{"question":"Toppings?","header":"Toppings","options":[{"label":"Cheese"},{"label":"Ham"}],"multiSelect":true}]}}`
	ch := hookAsync(d, ask)
	p := awaitPrompt(t, d)
	if !strings.Contains(p.Context, "question 1 of 2") {
		t.Fatalf("step 1: %+v", p)
	}
	step := func(key string, want string) model.Prompt {
		t.Helper()
		if err := d.Answer("l1", p.ID, key); err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		e, _ := d.Store.Get("l1")
		if e.A.Waiting == nil || e.A.Waiting.ID == p.ID || !strings.Contains(e.A.Waiting.Context+e.A.Waiting.Title, want) || e.A.Waiting.Until.IsZero() {
			t.Fatalf("after %s: %+v", key, e.A.Waiting)
		}
		select {
		case out := <-ch:
			t.Fatalf("the hook returned %q before the review was submitted", out)
		default:
		}
		return *e.A.Waiting
	}
	p = step("2", "question 2 of 2") // L
	p = step("1", "question 2 of 2") // tick Cheese, stay
	if !p.Options[0].Selected || p.Options[1].Selected {
		t.Fatalf("ticks: %+v", p.Options)
	}
	p = step("2", "question 2 of 2") // tick Ham
	p = step("3", "submit these answers?")
	if err := d.Answer("l1", p.ID, "1"); err != nil {
		t.Fatal(err)
	}
	hso := hookOutput(t, ch)
	in, _ := hso["updatedInput"].(map[string]any)
	answers, _ := in["answers"].(map[string]any)
	if hso["permissionDecision"] != "allow" || answers["Size?"] != "L" || answers["Toppings?"] != "Cheese, Ham" {
		t.Fatalf("answers: %v", hso)
	}
	if e, _ := d.Store.Get("l1"); e.A.Status != model.Running || e.A.Waiting != nil || e.Form != nil {
		t.Fatalf("after submit: %s %+v %+v", e.A.Status, e.A.Waiting, e.Form)
	}

	// Cancelled at the review: the question is declined and Claude stops.
	ch = hookAsync(d, ask)
	p = awaitPrompt(t, d)
	p = step("1", "question 2 of 2")
	p = step("3", "submit these answers?")
	if err := d.Answer("l1", p.ID, "2"); err != nil {
		t.Fatal(err)
	}
	hso = hookOutput(t, ch)
	if hso["permissionDecision"] != "deny" || hso["permissionDecisionReason"] == "" {
		t.Fatalf("cancel: %v", hso)
	}
	if e, _ := d.Store.Get("l1"); e.A.Status != model.Idle || e.A.Waiting != nil {
		t.Fatalf("after cancel: %s %+v", e.A.Status, e.A.Waiting)
	}
}

// The hook goes away before the wait is over (Claude Code's timeout, an old
// install's 5 s): the hold ends at once, the prompt stays to be answered in
// the terminal, and a later tap on the deck is refused instead of booked.
func TestHeldHookGoneEndsTheHold(t *testing.T) {
	d := holdDaemon(t, 30)
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan []byte, 1)
	go func() {
		out, _ := d.Hook(ctx, "", []byte(`{"hook_event_name":"PermissionRequest","session_id":"`+holdSID+`","tool_name":"Bash","tool_input":{"command":"npm test"}}`))
		ch <- out
	}()
	p := awaitPrompt(t, d)
	cancel()
	select {
	case out := <-ch:
		if out != nil {
			t.Fatalf("output after the hook went away: %q", out)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the hold outlived its hook")
	}
	e, _ := d.Store.Get("l1")
	if e.A.Status != model.Waiting || e.A.Waiting == nil || e.A.Waiting.ID != p.ID || !e.A.Waiting.Until.IsZero() {
		t.Fatalf("after the hook left: %s %+v", e.A.Status, e.A.Waiting)
	}
	if err := d.Answer("l1", p.ID, "1"); err != errLive {
		t.Fatalf("late answer: %v", err)
	}
}

// A newer prompt replaces a held one: the old hook returns empty at once, so
// Claude Code asks in its terminal, and the deck's answer goes to the new one.
func TestHeldPromptReplacedByANewerOne(t *testing.T) {
	d := holdDaemon(t, 30)
	first := hookAsync(d, `{"hook_event_name":"PermissionRequest","session_id":"`+holdSID+`","tool_name":"Bash","tool_input":{"command":"npm test"}}`)
	p1 := awaitPrompt(t, d)
	second := hookAsync(d, `{"hook_event_name":"PermissionRequest","session_id":"`+holdSID+`","tool_name":"Bash","tool_input":{"command":"go vet ./..."}}`)
	select {
	case out := <-first:
		if out != nil {
			t.Fatalf("the replaced hook got %q", out)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the replaced hook never returned")
	}
	var p2 model.Prompt
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if p2 = awaitPrompt(t, d); p2.ID != p1.ID {
			break
		}
	}
	if p2.ID == p1.ID {
		t.Fatal("no new prompt")
	}
	if err := d.Answer("l1", p2.ID, "1"); err != nil {
		t.Fatalf("answer: %v", err)
	}
	if hso := hookOutput(t, second); hso["decision"].(map[string]any)["behavior"] != "allow" {
		t.Fatalf("second hook: %v", hso)
	}
}

// Taps on a held prompt racing a newer prompt that replaces it never lock up
// the daemon.
func TestHeldAnswerRacingReplacement(t *testing.T) {
	ev := func(cmd string) string {
		return `{"hook_event_name":"PermissionRequest","session_id":"` + holdSID + `","tool_name":"Bash","tool_input":{"command":"` + cmd + `"}}`
	}
	for i := range 50 {
		d := holdDaemon(t, 30)
		first := hookAsync(d, ev("npm test"))
		p := awaitPrompt(t, d)
		done := make(chan struct{})
		go func() { _ = d.Answer("l1", p.ID, "1"); close(done) }()
		second := hookAsync(d, ev("go vet"))
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatalf("round %d: the answer locked up", i)
		}
		select {
		case <-first:
		case <-time.After(3 * time.Second):
			t.Fatalf("round %d: the first hook never returned", i)
		}
		// End the second hold: answer what it shows until its hook returns
		// (the hold may not be registered yet on the first try).
		deadline := time.After(3 * time.Second)
	wait:
		for {
			select {
			case <-second:
				break wait
			case <-deadline:
				t.Fatalf("round %d: the second hook never returned", i)
			case <-time.After(20 * time.Millisecond):
				if e, _ := d.Store.Get("l1"); e.A.Waiting != nil {
					_ = d.Answer("l1", e.A.Waiting.ID, "1")
				}
			}
		}
	}
}
