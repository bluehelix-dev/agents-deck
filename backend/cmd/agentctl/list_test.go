package main

import (
	"testing"

	"github.com/brushknight/agents-deck/backend/internal/model"
)

func TestListCost(t *testing.T) {
	for _, c := range []struct {
		name string
		a    model.Agent
		want string
	}{
		{"own Claude", model.Agent{Tool: "claude", CostUSD: 1.5}, "$1.50"},
		{"watched Claude session", model.Agent{Tool: "claude", External: true, CostUSD: 2.25}, "$2.25"},
		{"Codex app thread", model.Agent{Tool: "codex", External: true}, "—"},
		{"Codex in Orca", model.Agent{Tool: "codex", External: true, App: "orca", CostUSD: 0.4}, "$0.40"},
	} {
		if got := listCost(c.a); got != c.want {
			t.Errorf("%s: listCost = %q, want %q", c.name, got, c.want)
		}
	}
}
