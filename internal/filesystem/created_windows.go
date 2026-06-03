//go:build windows

package filesystem

import (
	"os"
	"syscall"
	"time"
)

// createdAtUnixMs returns the file creation time in Unix milliseconds, or 0 when
// unavailable.
//
// Rust uses `metadata.created().ok().map_or(0, system_time_to_unix_ms)`. On
// Windows the creation time is available via the file attribute data.
func createdAtUnixMs(info os.FileInfo) int64 {
	sys, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return 0
	}
	t := time.Unix(0, sys.CreationTime.Nanoseconds())
	return systemTimeToUnixMs(t)
}
