//go:build unix

package msghistory

import (
	"errors"
	"os"
	"syscall"
)

// errWouldBlock mirrors the Rust `TryLockError::WouldBlock` signal: the lock is
// held by someone else and the caller should retry.
var errWouldBlock = errors.New("msghistory: file lock would block")

// tryLockExclusive attempts a non-blocking exclusive advisory lock, mirroring
// the Rust `File::try_lock`. It returns errWouldBlock when the lock is
// contended.
func tryLockExclusive(file *os.File) error {
	return tryFlock(file, syscall.LOCK_EX)
}

// tryLockShared attempts a non-blocking shared advisory lock, mirroring the
// Rust `File::try_lock_shared`. It returns errWouldBlock when the lock is
// contended.
func tryLockShared(file *os.File) error {
	return tryFlock(file, syscall.LOCK_SH)
}

// unlock releases any advisory lock held on the file.
func unlock(file *os.File) error {
	return ignoreEINTR(func() error {
		return syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	})
}

func tryFlock(file *os.File, how int) error {
	err := ignoreEINTR(func() error {
		return syscall.Flock(int(file.Fd()), how|syscall.LOCK_NB)
	})
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		return errWouldBlock
	}
	return err
}

func ignoreEINTR(fn func() error) error {
	for {
		err := fn()
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		return err
	}
}
