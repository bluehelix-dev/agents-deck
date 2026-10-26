//go:build windows

package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/paths"
)

// runKey is the per-user login items key; no administrator rights needed
// (a scheduled task with a logon trigger would need them).
const (
	runKey   = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
	runValue = "agents-deck"
)

// install registers `agentctl serve` as a login item that runs with no
// window (conhost --headless) and its log in the data folder, then starts
// it, replacing a daemon that is already running (a rebuilt binary takes
// over).
func install() error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	self, _ = filepath.EvalSymlinks(self)
	if err := paths.Ensure(); err != nil {
		return err
	}
	cmdline := fmt.Sprintf(`conhost.exe --headless "%s" serve --log "%s"`, self, paths.Log())
	if out, err := exec.Command("reg", "add", runKey, "/v", runValue, "/t", "REG_SZ", "/d", cmdline, "/f").CombinedOutput(); err != nil {
		return fmt.Errorf("reg add: %v: %s", err, strings.TrimSpace(string(out)))
	}
	for _, pid := range otherAgentctls() {
		if p, err := os.FindProcess(pid); err == nil {
			_ = p.Kill()
			p.Release()
		}
	}
	if err := startHidden(self, "serve", "--log", paths.Log()); err != nil {
		return err
	}
	fmt.Printf("installed login item %s (starts at login, no window); started now; log: %s\n", runValue, paths.Log())
	return nil
}

// otherAgentctls are the pids of every agentctl.exe but this one (the
// daemon, however it was started).
func otherAgentctls() []int {
	out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq agentctl.exe", "/FO", "CSV", "/NH").Output()
	if err != nil {
		return nil
	}
	rows, _ := csv.NewReader(strings.NewReader(string(out))).ReadAll()
	var pids []int
	for _, r := range rows {
		if len(r) < 2 || !strings.EqualFold(r[0], "agentctl.exe") {
			continue
		}
		if pid, err := strconv.Atoi(r[1]); err == nil && pid != os.Getpid() {
			pids = append(pids, pid)
		}
	}
	return pids
}

// startHidden runs the daemon detached, with no console window, and makes
// sure it is still up a moment later (a port in use, say, ends it at once).
func startHidden(exe string, args ...string) error {
	const createNoWindow = 0x08000000
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow | syscall.CREATE_NEW_PROCESS_GROUP}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start the daemon: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return fmt.Errorf("the daemon stopped right away (%v): see %s", err, paths.Log())
	case <-time.After(1500 * time.Millisecond):
		return nil
	}
}
