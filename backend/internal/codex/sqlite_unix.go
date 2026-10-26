//go:build !windows

package codex

// sqlite3Bin is the system sqlite3 (never one from PATH: the daemon doesn't
// run whatever a shell profile put first).
func sqlite3Bin() string { return "/usr/bin/sqlite3" }
