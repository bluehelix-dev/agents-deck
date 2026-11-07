//go:build windows

package paths

import "os"

// runtimeDir: the per-user temp folder (%LOCALAPPDATA%\Temp); no shared /tmp
// on Windows.
func runtimeDir() string { return os.TempDir() }

// SocketDirOK: Windows has no shared temp folder to guard against.
func SocketDirOK(string) error { return nil }
