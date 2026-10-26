//go:build windows

package main

import (
	"os"
	"os/exec"
)

// execAttach runs the tmux client as a child with this terminal, since
// Windows can't replace a running process.
func execAttach(argv []string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}
