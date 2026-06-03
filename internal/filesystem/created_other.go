//go:build !darwin && !freebsd && !netbsd && !windows

package filesystem

import "os"

// createdAtUnixMs returns the file creation time in Unix milliseconds, or 0 when
// unavailable.
//
// DEVIATION: on Linux the creation (birth) time is only retrievable via the
// statx(2) syscall, which the Go standard library does not surface through
// os.FileInfo (Sys() returns a *syscall.Stat_t without a btime field), and the
// statx wrapper lives in golang.org/x/sys, which is outside this package's
// stdlib-only dependency budget. Rust's `metadata.created()` likewise returns an
// error on filesystems without birth-time support, in which case Codex stores 0
// (`.ok().map_or(0, ...)`). This port therefore reports 0 on these platforms,
// which matches the Rust fallback whenever the birth time is unavailable. The
// modification time (the field actually consumed by the app-server fs/* methods'
// callers in practice) is fully supported on all platforms.
func createdAtUnixMs(info os.FileInfo) int64 {
	_ = info
	return 0
}
