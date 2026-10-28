//go:build !darwin && !windows

package claude

import (
	"os"
	"time"
)

// birthTime is unknown here: the standard library has no creation time, and
// the mtime would be the last write, not the start.
func birthTime(os.FileInfo) time.Time { return time.Time{} }
