//go:build !windows

package main

import (
	"os"
	"syscall"
)

// execAttach replaces this process with the tmux client, so the terminal
// is tmux's until the user detaches.
func execAttach(argv []string) error { return syscall.Exec(argv[0], argv, os.Environ()) }
