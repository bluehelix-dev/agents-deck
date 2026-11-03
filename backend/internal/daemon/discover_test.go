package daemon

import (
	"testing"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/claude"
)

// One failed `claude agents --json` keeps the last answer: the watched
// sessions stay on the board. Only failures in a row give it up.
func TestDiscoverKeepsTheLastAnswer(t *testing.T) {
	answer, ok := []claude.Live{{SessionID: "s1", Cwd: "/w/app"}}, true
	was := listActive
	listActive = func(string, map[string]bool) ([]claude.Live, bool) { return answer, ok }
	t.Cleanup(func() { listActive = was })

	d := &Daemon{}
	d.live.claudeBin = "claude"
	ask := func() []claude.Live {
		d.live.reportedAt = time.Time{} // past the 10 s cache
		return d.discover(nil)
	}
	if got := ask(); len(got) != 1 {
		t.Fatalf("first answer: %+v", got)
	}
	answer, ok = nil, false
	for i := 1; i < liveFailuresToGiveUp; i++ {
		if got := ask(); len(got) != 1 || got[0].SessionID != "s1" {
			t.Fatalf("failure %d dropped the sessions: %+v", i, got)
		}
	}
	ask()
	if d.live.reportedOK {
		t.Fatal("still trusting the old answer after failures in a row")
	}
}
