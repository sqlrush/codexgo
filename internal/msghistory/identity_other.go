//go:build !unix

package msghistory

import "io/fs"

// logIdentity has no stable inode-based identifier on non-Unix platforms, so it
// reports that no identity is available (mirroring the `cfg(not(any(unix,
// windows)))` arm of the Rust `log_identity`).
func logIdentity(fs.FileInfo) (uint64, bool) {
	return 0, false
}
