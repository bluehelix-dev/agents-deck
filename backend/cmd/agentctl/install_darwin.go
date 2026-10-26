//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/brushknight/agents-deck/backend/internal/paths"
)

// ---- install ---------------------------------------------------------------------

const plistLabel = "dev.agents-deck.agentctl"

func install() error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	self, _ = filepath.EvalSymlinks(self)
	if err := paths.Ensure(); err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	plist := filepath.Join(home, "Library", "LaunchAgents", plistLabel+".plist")
	content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>%s</string>
	<key>ProgramArguments</key>
	<array><string>%s</string><string>serve</string></array>
	<key>EnvironmentVariables</key>
	<dict><key>PATH</key><string>%s/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string></dict>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><true/>
	<key>ProcessType</key><string>Interactive</string>
	<key>StandardOutPath</key><string>%s</string>
	<key>StandardErrorPath</key><string>%s</string>
</dict>
</plist>
`, plistLabel, self, home, paths.Log(), paths.Log())
	uid := fmt.Sprint(os.Getuid())
	job := "gui/" + uid + "/" + plistLabel
	// Same login item already registered: just restart the daemon (it picks
	// up a rebuilt binary). Re-registering makes macOS announce a new
	// background item every time.
	if cur, err := os.ReadFile(plist); err == nil && string(cur) == content &&
		exec.Command("launchctl", "print", job).Run() == nil {
		if out, err := exec.Command("launchctl", "kickstart", "-k", job).CombinedOutput(); err != nil {
			return fmt.Errorf("launchctl kickstart: %v: %s", err, out)
		}
		fmt.Println("restarted", plistLabel)
		return nil
	}
	if err := os.WriteFile(plist, []byte(content), 0o644); err != nil {
		return err
	}
	_ = exec.Command("launchctl", "bootout", job).Run()
	time.Sleep(500 * time.Millisecond)
	if out, err := exec.Command("launchctl", "bootstrap", "gui/"+uid, plist).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl bootstrap: %v: %s", err, out)
	}
	fmt.Println("installed", plist)
	return nil
}
