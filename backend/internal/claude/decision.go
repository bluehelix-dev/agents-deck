package claude

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/brushknight/agents-deck/backend/internal/store"
)

// Claude Code lets a hook decide what it was about to ask: a PermissionRequest
// hook allows or denies the tool call, and a PreToolUse hook on
// AskUserQuestion answers the question by handing the tool its input with the
// answers filled in. These build what `agentctl hook` prints for an answer
// given on the deck (daemon: hold.go).

// PermissionDecision is the PermissionRequest hook's output for a deck answer:
// "1" allows, "2" allows and keeps the rules Claude Code suggested (so it
// doesn't ask again), "3" denies and stops the turn, as "no" does in the
// terminal. ok is false for any other key.
func PermissionDecision(key string, suggestions json.RawMessage) (out []byte, ok bool) {
	d := map[string]any{}
	switch key {
	case "1":
		d["behavior"] = "allow"
	case "2":
		d["behavior"] = "allow"
		if len(suggestions) > 0 && string(suggestions) != "null" {
			d["updatedPermissions"] = suggestions
		}
	case "3":
		d["behavior"] = "deny"
		d["message"] = "denied from the deck"
		d["interrupt"] = true
	default:
		return nil, false
	}
	b, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PermissionRequest", "decision": d}})
	return b, true
}

// AnswerQuestion is the PreToolUse hook's output for one pick-one question
// answered with key (the option's number): the tool's input with the answer,
// which makes Claude Code skip its own dialog. ok is false when the input
// isn't such a question or the key isn't one of its options.
func AnswerQuestion(input json.RawMessage, key string) (out []byte, ok bool) {
	var in askInput
	if json.Unmarshal(input, &in) != nil || len(in.Questions) != 1 || in.Questions[0].MultiSelect {
		return nil, false
	}
	k, err := strconv.Atoi(key)
	q := in.Questions[0]
	if err != nil || k < 1 || k > len(q.Options) {
		return nil, false
	}
	return answered(input, map[string]string{strings.TrimSpace(q.Question): q.Options[k-1].Label}), true
}

// AnswerForm is the PreToolUse hook's output for a form the deck walked to its
// review and submitted: every answered question with the labels picked (a
// pick-any question's joined with commas, as Claude Code does).
func AnswerForm(f *store.Form, input json.RawMessage) []byte {
	answers := map[string]string{}
	for i, q := range f.Questions {
		var labels []string
		for _, k := range f.Picked[i] {
			if k >= 0 && k < len(q.Options) {
				labels = append(labels, q.Options[k].Label)
			}
		}
		if len(labels) > 0 {
			answers[q.Question] = strings.Join(labels, ", ")
		}
	}
	return answered(input, answers)
}

// CancelQuestion is the PreToolUse hook's output when the form is cancelled
// on the deck: the question is declined and Claude stops, as Cancel does in
// the terminal.
func CancelQuestion() []byte {
	b, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": "PreToolUse", "permissionDecision": "deny",
		"permissionDecisionReason": "the question was cancelled from the deck; the user will say how to go on"}})
	return b
}

func answered(input json.RawMessage, answers map[string]string) []byte {
	in := map[string]any{}
	_ = json.Unmarshal(input, &in)
	in["answers"] = answers
	b, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": "PreToolUse", "permissionDecision": "allow", "updatedInput": in}})
	return b
}
