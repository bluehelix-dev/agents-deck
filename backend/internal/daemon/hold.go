package daemon

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/claude"
	"github.com/brushknight/agents-deck/backend/internal/model"
	"github.com/brushknight/agents-deck/backend/internal/server"
	"github.com/brushknight/agents-deck/backend/internal/store"
)

// A watched session (started by hand, reporting through hooks) has nothing the
// deck can type into: on Windows there is no tmux. Claude Code lets a hook
// decide a permission request, and answer a question through the tool's
// input. So while a phone, a board or the browser dashboard is connected, the
// daemon holds such a hook open for a while (Config.AnswerWaitSeconds): an
// answer from the deck goes back as the hook's decision, and when none comes
// the hook returns empty and Claude Code asks in the terminal as usual. The
// prompt's Until tells the clients how long the deck can answer.

// DefaultAnswerWait is how long a hook waits for the deck when the config
// doesn't say.
const DefaultAnswerWait = 20 * time.Second

// maxAnswerWait stays under the hook's own timeout (claude.HookTimeout), so
// Claude Code never kills the hook while it waits.
const maxAnswerWait = 110 * time.Second

type hold struct {
	agent string
	event claude.Event
	done  chan []byte // the decision for `agentctl hook` to print

	// mu serialises the deck's answer against the end of the wait: an answer
	// is taken only while the hook is still there to carry it, and the wait
	// ends only when no answer is being taken.
	mu     sync.Mutex
	prompt string // the prompt open now (a form moves on to new ids)
	closed bool   // answered, timed out, or the hook went away
}

func (d *Daemon) answerWait() time.Duration {
	w := DefaultAnswerWait
	if d.Cfg.AnswerWaitSeconds > 0 {
		w = time.Duration(d.Cfg.AnswerWaitSeconds) * time.Second
	}
	return min(w, maxAnswerWait)
}

// holdable tells the hook events Claude Code lets the hook decide: a
// permission request, and the question tool before it asks (its own
// PermissionRequest only repeats the question).
func holdable(ev claude.Event) bool {
	if ev.Name == "PermissionRequest" {
		return ev.ToolName != "AskUserQuestion"
	}
	return ev.Name == "PreToolUse" && ev.ToolName == "AskUserQuestion"
}

// canHold: something connected could answer, and the event is one to wait for.
func (d *Daemon) canHold(ev claude.Event) bool {
	return holdable(ev) && d.Answerers != nil && d.Answerers() > 0
}

// startHold: the entry is a watched Claude session showing a prompt with
// options; sets Until and returns the hold to wait on. Called inside a store
// update, after the event was applied, on the prompt it just made.
func (d *Daemon) startHold(agentID string, ev claude.Event, e *store.Entry) *hold {
	if !e.External || e.A.Tool != "claude" || e.A.App != "" || e.A.Status != model.Waiting || e.A.Waiting == nil || len(e.A.Waiting.Options) == 0 {
		return nil
	}
	e.A.Waiting.Until = time.Now().Add(d.answerWait())
	return &hold{agent: agentID, event: ev, prompt: e.A.Waiting.ID, done: make(chan []byte, 1)}
}

// wait blocks the hook until the deck answers, the wait is over, or the hook
// is gone (ctx: Claude Code killed it, or the connection dropped). It returns
// what the hook prints: the decision, or nothing.
func (d *Daemon) wait(ctx context.Context, h *hold) []byte {
	// Never hold d.mu and a hold's mu at once: answerHeld takes them one
	// after the other too, so neither can wait on the other.
	d.mu.Lock()
	old := d.holds[h.agent]
	if d.holds == nil {
		d.holds = map[string]*hold{}
	}
	d.holds[h.agent] = h
	d.mu.Unlock()
	if old != nil {
		old.mu.Lock()
		if !old.closed { // a newer prompt replaced it: let that hook go
			old.closed = true
			old.done <- nil
		}
		old.mu.Unlock()
	}
	timer := time.NewTimer(d.answerWait())
	defer timer.Stop()
	var out []byte
	select {
	case out = <-h.done:
	case <-timer.C:
	case <-ctx.Done():
	}
	h.mu.Lock()
	if out == nil && h.closed {
		// Ended by someone else just as the wait ended: whoever closed it put
		// its value (an answer, or nil when replaced) on done before letting
		// go of h.mu, so it is there now, unless select already took it.
		select {
		case out = <-h.done:
		default:
		}
	}
	h.closed = true
	id := h.prompt
	h.mu.Unlock()
	d.mu.Lock()
	if d.holds[h.agent] == h {
		delete(d.holds, h.agent)
	}
	d.mu.Unlock()
	if out == nil {
		// Claude Code asks in the terminal now; the deck keeps showing the
		// prompt, to be answered there. A copy: snapshots read the prompt
		// outside the store's lock.
		d.Store.Update(h.agent, func(e *store.Entry) bool {
			if e.A.Waiting == nil || e.A.Waiting.ID != id || e.A.Waiting.Until.IsZero() {
				return false
			}
			p := *e.A.Waiting
			p.Until = time.Time{}
			e.A.Waiting = &p
			return true
		})
	}
	return out
}

// answerHeld answers a prompt whose hook is waiting. handled is false when no
// hook waits for this agent any more: the answer goes its usual way (for a
// watched session, "answer it in the terminal").
func (d *Daemon) answerHeld(id, promptID, key string) (handled bool, err error) {
	d.mu.Lock()
	h := d.holds[id]
	d.mu.Unlock()
	if h == nil {
		return false, nil
	}
	h.mu.Lock()
	handled, err = d.answerLocked(h, id, promptID, key)
	ended := h.closed
	h.mu.Unlock()
	if handled && ended && err == nil {
		d.mu.Lock()
		if d.holds[id] == h {
			delete(d.holds, id)
		}
		d.mu.Unlock()
		log.Printf("answered %s prompt %s with option %s through its hook", id, promptID, key)
	}
	return handled, err
}

// answerLocked does answerHeld's work under h.mu, and never takes d.mu.
func (d *Daemon) answerLocked(h *hold, id, promptID, key string) (handled bool, err error) {
	if h.closed {
		return false, nil
	}
	var out []byte
	err = server.ErrStalePrompt
	d.Store.Update(id, func(e *store.Entry) bool {
		p := e.A.Waiting
		if e.A.Status != model.Waiting || p == nil || p.ID != promptID || p.ID != h.prompt {
			return false
		}
		known := false
		for _, o := range p.Options {
			known = known || o.Key == key
		}
		if !known {
			err = server.ErrBadKey
			return false
		}
		err = nil
		if f := claude.ActiveForm(e); f != nil {
			if _, _, ok := claude.FormKeys(f, key); !ok {
				err = server.ErrBadKey
				return false
			}
			answers := claude.AnswerForm(f, h.event.ToolInput)
			switch claude.AdvanceForm(e, key) {
			case claude.FormNext:
				e.A.Waiting.Until = p.Until // the hook keeps waiting for the next step
				h.prompt = e.A.Waiting.ID
			case claude.FormSubmitted:
				out = answers
				e.A.Waiting = nil
			case claude.FormCancelled:
				out = claude.CancelQuestion()
				e.A.Waiting = nil
			}
			return true
		}
		var ok bool
		switch h.event.Name {
		case "PreToolUse":
			out, ok = claude.AnswerQuestion(h.event.ToolInput, key)
			e.SetStatus(model.Running)
		default:
			out, ok = claude.PermissionDecision(key, h.event.Suggestions)
			if key == "3" {
				e.SetStatus(model.Idle) // Claude stops and waits for instructions
			} else {
				e.SetStatus(model.Running)
			}
		}
		if !ok {
			err = server.ErrBadKey
			return false
		}
		e.A.Waiting = nil
		return true
	})
	if err != nil {
		return true, err
	}
	if out != nil {
		h.closed = true
		h.done <- out
	}
	return true, nil
}
