//go:build !unix

package msghistory

import (
	"errors"
	"os"
)

// errWouldBlock mirrors the Rust `TryLockError::WouldBlock` signal.
var errWouldBlock = errors.New("msghistory: file lock would block")

// tryLockExclusive is a no-op on platforms without advisory file locking.
func tryLockExclusive(*os.File) error { return nil }

// tryLockShared is a no-op on platforms without advisory file locking.
func tryLockShared(*os.File) error { return nil }

// unlock is a no-op on platforms without advisory file locking.
func unlock(*os.File) error { return nil }
