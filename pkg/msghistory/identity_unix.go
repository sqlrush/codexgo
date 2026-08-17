//go:build unix

package msghistory

import (
	"io/fs"
	"syscall"
)

// logIdentity returns the file's inode as a stable identifier, mirroring the
// Rust `log_identity` on Unix (`metadata.ino()`). It returns (0, false) when the
// underlying stat data is unavailable.
func logIdentity(info fs.FileInfo) (uint64, bool) {
	sys, ok := info.Sys().(*syscall.Stat_t)
	if !ok || sys == nil {
		return 0, false
	}
	return uint64(sys.Ino), true
}
