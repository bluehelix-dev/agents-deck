package store

import "testing"

// The state says whether focus works on this machine, as the daemon set it.
func TestStateCarriesFocus(t *testing.T) {
	s := New("", "deck", "0")
	if s.Snapshot().Server.Focus {
		t.Fatal("focus should be off until the daemon says otherwise")
	}
	s.Focus = true
	if !s.Snapshot().Server.Focus {
		t.Fatal("focus not carried into the state")
	}
}
