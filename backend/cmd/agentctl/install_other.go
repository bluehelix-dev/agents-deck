//go:build !darwin && !windows

package main

import "errors"

// install: Linux has no login item yet (a systemd user unit is the plan);
// run `agentctl serve` from one by hand until then.
func install() error {
	return errors.New("agentctl install isn't available on this platform yet: run `agentctl serve` from a systemd user unit")
}
