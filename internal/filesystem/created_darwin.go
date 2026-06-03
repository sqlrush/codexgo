//go:build darwin || freebsd || netbsd

package filesystem

import (
	"os"
	"syscall"
	"time"
)

// createdAtUnixMs returns the file creation (birth) time in Unix milliseconds,
// or 0 when unavailable.
//
// Rust uses `metadata.created().ok().map_or(0, system_time_to_unix_ms)`. On
// Darwin/BSD the birth time is available via the stat struct's Birthtimespec.
func createdAtUnixMs(info os.FileInfo) int64 {
	sys, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0
	}
	birth := sys.Birthtimespec
	t := time.Unix(birth.Sec, birth.Nsec)
	return systemTimeToUnixMs(t)
}
