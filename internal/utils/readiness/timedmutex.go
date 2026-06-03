package readiness

import (
	"context"
	"time"
)

// timedMutex is a mutual-exclusion lock that supports both non-blocking and
// time-bounded acquisition.
//
// The Go standard library sync.Mutex offers neither TryLock-with-timeout nor a
// context-aware Lock, both of which the Rust original relies on (Tokio's async
// Mutex with time::timeout for timed locking, and try_lock for the non-blocking
// fast path in IsReady). A buffered channel of capacity one is the idiomatic
// standard-library construction that provides all three behaviors.
//
// The channel holds a single token. A goroutine holds the lock while it has
// received the token from the channel; it releases the lock by sending the token
// back.
type timedMutex struct {
	ch chan struct{}
}

// newTimedMutex returns an unlocked timedMutex.
func newTimedMutex() *timedMutex {
	m := &timedMutex{ch: make(chan struct{}, 1)}
	m.ch <- struct{}{}
	return m
}

// tryLock attempts to acquire the lock without blocking. It reports whether the
// lock was acquired. This mirrors Tokio Mutex::try_lock used by IsReady.
func (m *timedMutex) tryLock() bool {
	select {
	case <-m.ch:
		return true
	default:
		return false
	}
}

// lock acquires the lock, blocking until it is available, the supplied context
// is cancelled, or the timeout elapses. It returns nil on success and
// ErrTokenLockFailed if the lock could not be acquired in time. This mirrors the
// Rust with_tokens helper which wraps the async lock in time::timeout and maps a
// timeout to ReadinessError::TokenLockFailed.
//
// A nil context is treated as context.Background. A non-positive timeout means
// no deadline beyond context cancellation.
func (m *timedMutex) lock(ctx context.Context, timeout time.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}

	// Fast path: lock is immediately available.
	select {
	case <-m.ch:
		return nil
	default:
	}

	if timeout <= 0 {
		select {
		case <-m.ch:
			return nil
		case <-ctx.Done():
			return ErrTokenLockFailed
		}
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-m.ch:
		return nil
	case <-timer.C:
		return ErrTokenLockFailed
	case <-ctx.Done():
		return ErrTokenLockFailed
	}
}

// unlock releases the lock. It must only be called by a goroutine that currently
// holds the lock.
func (m *timedMutex) unlock() {
	select {
	case m.ch <- struct{}{}:
	default:
		// Releasing an unlocked mutex is a programming error; ignore the
		// extra release rather than block forever.
	}
}
