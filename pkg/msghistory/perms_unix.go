//go:build unix

package msghistory

import (
	"fmt"
	"os"
)

// ensureOwnerOnlyPermissions ensures the history file has 0o600 permissions,
// mirroring the Rust `ensure_owner_only_permissions` on Unix. The error is
// propagated when the permissions cannot be changed.
func ensureOwnerOnlyPermissions(file *os.File) error {
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat history file for permissions: %w", err)
	}
	if info.Mode().Perm() == fileMode {
		return nil
	}
	if err := file.Chmod(fileMode); err != nil {
		return fmt.Errorf("set history file permissions: %w", err)
	}
	return nil
}
