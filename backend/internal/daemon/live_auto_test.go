package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/store"
	"github.com/brushknight/agents-deck/backend/internal/tmux"
)

// With liveAutoAdd, a session found with a terminal goes on the deck by
// itself, a background one doesn't, and one taken off stays off until it is
// added again; watched.json records both.
func TestLiveAutoAdd(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGENTSTERM_HOME", filepath.Join(root, "home"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "cfg"))
	_ = os.MkdirAll(filepath.Join(root, "home"), 0o700)
	old := tmux.Socket
	tmux.Socket = fmt.Sprintf("agentsdeck-test-auto%d", os.Getpid())
	t.Cleanup(func() { tmux.Socket = old })

	term := "6a7b8c90-1111-4222-8333-944455556666"
	bg := "7a7b8c90-1111-4222-8333-944455556666"
	list := `[{"id":"ab12cd34","cwd":"/work/app","kind":"interactive","startedAt":1791446002609,"sessionId":"` + term + `","name":"the app","pid":4242,"status":"idle"},` +
		`{"id":"ef56ab78","cwd":"/work/bg","kind":"background","startedAt":1791446002609,"sessionId":"` + bg + `","name":"a background job","state":"done"}]`
	fake, script := filepath.Join(root, "claude"), "#!/bin/sh\necho '"+list+"'\n"
	if runtime.GOOS == "windows" {
		fake, script = fake+".cmd", "@echo off\r\necho "+list+"\r\n"
	}
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	d := &Daemon{Store: store.New("", "test", "0"), Cfg: Config{LiveAutoAdd: true}}
	d.live.claudeBin = fake
	d.syncLive()
	if _, ok := d.Store.Get(liveAgentID(term)); !ok {
		t.Fatal("the terminal session is not on the deck")
	}
	if _, ok := d.Store.Get(liveAgentID(bg)); ok {
		t.Fatal("the background session went on the deck by itself")
	}

	// Taken off: it stays off through the next syncs, and the file says so.
	e, _ := d.Store.Get(liveAgentID(term))
	d.removeLive(e)
	d.syncLive()
	if _, ok := d.Store.Get(liveAgentID(term)); ok {
		t.Fatal("the removed session came back")
	}
	var f watchedFile
	b, err := os.ReadFile(addedPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &f); err != nil || len(f.Added) != 0 || len(f.Removed) != 1 || f.Removed[0] != term {
		t.Fatalf("watched.json = %s (%v)", b, err)
	}

	// Added again by hand: back, and no longer marked removed.
	if err := d.AddLive(term[:8], -1); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.Store.Get(liveAgentID(term)); !ok {
		t.Fatal("the session added again is not on the deck")
	}
	b, _ = os.ReadFile(addedPath())
	f = watchedFile{}
	if err := json.Unmarshal(b, &f); err != nil || len(f.Added) != 1 || len(f.Removed) != 0 {
		t.Fatalf("watched.json = %s (%v)", b, err)
	}

	// A fresh daemon reads the file back; an older bare list reads too.
	d2 := &Daemon{Store: store.New("", "test", "0"), Cfg: Config{LiveAutoAdd: true}}
	if added := d2.liveAdded(); !added[term] || d2.live.removed[term] {
		t.Fatalf("reread: added %v removed %v", added, d2.live.removed)
	}
	if err := os.WriteFile(addedPath(), []byte(`["`+bg+`"]`), 0o600); err != nil {
		t.Fatal(err)
	}
	d3 := &Daemon{Store: store.New("", "test", "0")}
	if added := d3.liveAdded(); !added[bg] || len(added) != 1 {
		t.Fatalf("bare list: %v", added)
	}
}
