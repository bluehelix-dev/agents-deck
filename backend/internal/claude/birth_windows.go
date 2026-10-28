package claude

import (
	"os"
	"syscall"
	"time"
)

// birthTime is when the file was created (NTFS keeps it), else zero.
func birthTime(st os.FileInfo) time.Time {
	if s, ok := st.Sys().(*syscall.Win32FileAttributeData); ok {
		return time.Unix(0, s.CreationTime.Nanoseconds())
	}
	return time.Time{}
}
