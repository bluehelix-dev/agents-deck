package claude

import (
	"encoding/json"
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/store"
)

func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%q: %v", b, err)
	}
	hso, _ := m["hookSpecificOutput"].(map[string]any)
	if hso == nil {
		t.Fatalf("no hookSpecificOutput: %s", b)
	}
	return hso
}

// The three permission answers become the decisions Claude Code's hook
// contract takes; anything else is no decision.
func TestPermissionDecision(t *testing.T) {
	sugg := json.RawMessage(`[{"type":"addRules","rules":[{"toolName":"Bash","ruleContent":"npm test"}],"behavior":"allow","destination":"localSettings"}]`)
	out, ok := PermissionDecision("1", sugg)
	dec, _ := decode(t, out)["decision"].(map[string]any)
	if !ok || dec["behavior"] != "allow" || dec["updatedPermissions"] != nil {
		t.Fatalf("yes: %v", dec)
	}
	out, _ = PermissionDecision("2", sugg)
	dec, _ = decode(t, out)["decision"].(map[string]any)
	if rules, _ := dec["updatedPermissions"].([]any); dec["behavior"] != "allow" || len(rules) != 1 {
		t.Fatalf("don't ask again: %v", dec)
	}
	out, _ = PermissionDecision("2", nil) // Claude Code offered no rule: a plain allow
	dec, _ = decode(t, out)["decision"].(map[string]any)
	if dec["behavior"] != "allow" || dec["updatedPermissions"] != nil {
		t.Fatalf("don't ask again without suggestions: %v", dec)
	}
	out, _ = PermissionDecision("3", sugg)
	dec, _ = decode(t, out)["decision"].(map[string]any)
	if dec["behavior"] != "deny" || dec["interrupt"] != true || dec["message"] == "" {
		t.Fatalf("no: %v", dec)
	}
	if _, ok := PermissionDecision("4", sugg); ok {
		t.Fatal("4 is no permission answer")
	}
}

// A question's answer goes back as the tool's input plus answers, keyed by
// the question's text; a form's answers join a pick-any question's labels.
func TestAnswerQuestionAndForm(t *testing.T) {
	one := json.RawMessage(`{"questions":[{"question":" Which database? ","header":"Database","options":[{"label":"Postgres","description":"relational"},{"label":"SQLite"}],"multiSelect":false}]}`)
	out, ok := AnswerQuestion(one, "2")
	hso := decode(t, out)
	in, _ := hso["updatedInput"].(map[string]any)
	answers, _ := in["answers"].(map[string]any)
	if !ok || hso["hookEventName"] != "PreToolUse" || hso["permissionDecision"] != "allow" || answers["Which database?"] != "SQLite" || in["questions"] == nil {
		t.Fatalf("one question: %v", hso)
	}
	if _, ok := AnswerQuestion(one, "3"); ok {
		t.Fatal("3 is no option")
	}
	two := json.RawMessage(`{"questions":[{"question":"A?","options":[{"label":"x"}]},{"question":"B?","options":[{"label":"y"}]}]}`)
	if _, ok := AnswerQuestion(two, "1"); ok {
		t.Fatal("a form is walked, not answered with one key")
	}

	f := newForm(json.RawMessage(`{"questions":[{"question":"Size?","options":[{"label":"S"},{"label":"L"}]},{"question":"Toppings?","options":[{"label":"Cheese"},{"label":"Ham"}],"multiSelect":true},{"question":"Note?","options":[{"label":"none"}]}]}`))
	f.Picked[0], f.Answered[0] = []int{1}, true
	f.Picked[1], f.Answered[1] = []int{1, 0}, true // ticked in this order
	f.Answered[2] = true                           // moved on without a pick
	hso = decode(t, AnswerForm(f, two))
	in, _ = hso["updatedInput"].(map[string]any)
	answers, _ = in["answers"].(map[string]any)
	if hso["permissionDecision"] != "allow" || answers["Size?"] != "L" || answers["Toppings?"] != "Ham, Cheese" || len(answers) != 2 {
		t.Fatalf("form: %v", answers)
	}
	hso = decode(t, CancelQuestion())
	if hso["permissionDecision"] != "deny" || hso["permissionDecisionReason"] == "" {
		t.Fatalf("cancel: %v", hso)
	}
	_ = store.Form{}
}
