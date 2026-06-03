package pathutil

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteAtomically writes contents to writePath atomically.
//
// It mirrors write_atomically: the parent directory is created if necessary, the
// contents are written to a uniquely named temporary file in the same directory,
// and that file is renamed onto writePath. Writing within the destination
// directory keeps the final rename on the same filesystem so it is atomic. On
// any failure the temporary file is removed and the destination is left
// untouched.
//
// An error is returned if writePath has no parent directory, mirroring the Rust
// InvalidInput error.
func WriteAtomically(writePath string, contents string) error {
	parent := filepath.Dir(writePath)
	if parent == writePath || parent == "" {
		return fmt.Errorf("path %s has no parent directory", writePath)
	}

	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(parent, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	// Ensure the temporary file does not linger if a later step fails. After a
	// successful rename the path no longer exists, so the cleanup is a harmless
	// no-op.
	cleanup := func() { _ = os.Remove(tmpName) }

	if _, err := tmp.WriteString(contents); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}

	if err := os.Rename(tmpName, writePath); err != nil {
		cleanup()
		return err
	}
	return nil
}
