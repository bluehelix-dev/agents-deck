package pricing

import (
	"math"
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/model"
)

func TestLookup(t *testing.T) {
	for _, c := range []struct {
		id, label string
		window    int64
	}{
		{"claude-sonnet-5-5", "sonnet 5.5", 1_000_000},
		{"claude-sonnet-5", "sonnet 5", 1_000_000},
		{"claude-haiku-5-5", "haiku 5.5", 1_000_000},
		{"claude-haiku-4-5-20251001", "haiku 4.5", 200_000},
		{"claude-opus-5-5", "opus 5.5", 1_000_000},
		{"claude-something-new", "something-new", 200_000},
	} {
		if got := Label(c.id); got != c.label {
			t.Errorf("Label(%q) = %q, want %q", c.id, got, c.label)
		}
		if got := Window(c.id); got != c.window {
			t.Errorf("Window(%q) = %d, want %d", c.id, got, c.window)
		}
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestCost(t *testing.T) {
	// Sonnet 5.5: $2 in, $10 out, $0.10 cache read, writes 1.25× input.
	got := Cost("claude-sonnet-5-5", model.Tokens{Input: 1_000_000, Output: 1_000_000, CacheRead: 1_000_000, CacheWrite: 1_000_000})
	if want := 2 + 10 + 0.10 + 2.5; !near(got, want) {
		t.Errorf("sonnet 5.5 = %v, want %v", got, want)
	}
	if Cost("claude-haiku-5-5", model.Tokens{Input: 1000}) == 0 {
		t.Error("haiku 5.5 is not priced")
	}
}

// Haiku 5.5 costs five times as much for a request whose prompt (input,
// cache reads and cache writes together) is over 100,000 tokens.
func TestCostHaikuLongPrompt(t *testing.T) {
	short := model.Tokens{Input: 20_000, CacheRead: 60_000, CacheWrite: 20_000, Output: 1_000} // exactly 100,000
	want := (20_000*0.10 + 60_000*0.01 + 20_000*0.10*1.25 + 1_000*0.50) / 1e6
	if got := Cost("claude-haiku-5-5", short); !near(got, want) {
		t.Errorf("100,000 prompt tokens = %v, want %v", got, want)
	}
	long := short
	long.Input++
	want = (20_001*0.50 + 60_000*0.05 + 20_000*0.50*1.25 + 1_000*2.50) / 1e6
	if got := Cost("claude-haiku-5-5", long); !near(got, want) {
		t.Errorf("100,001 prompt tokens = %v, want %v", got, want)
	}
}
