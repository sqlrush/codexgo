package readiness

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// lockTimeout bounds how long the internal token lock acquisition may take
// before MarkReady / Subscribe report ErrTokenLockFailed. It mirrors the Rust
// LOCK_TIMEOUT constant of 1000ms.
const lockTimeout = 1000 * time.Millisecond

// Readiness is the behavioral contract of a readiness flag. It mirrors the Rust
// Readiness trait.
//
// Methods that may block on the internal lock accept a context.Context so
// callers can bound the wait, in addition to the built-in lock timeout.
type Readiness interface {
	// IsReady reports whether the flag is currently marked ready. At least
	// one token must have been marked ready before, OR there must have been
	// no outstanding subscribers when IsReady was first called. The ready
	// state is not reversible.
	IsReady() bool

	// Subscribe registers interest in readiness and returns an authorization
	// token. If the flag is already ready it returns ErrFlagAlreadyReady.
	Subscribe(ctx context.Context) (Token, error)

	// MarkReady attempts to mark the flag ready, validated by token. It
	// reports true only if token is currently subscribed and the flag was
	// not already ready.
	MarkReady(ctx context.Context, token Token) (bool, error)

	// WaitReady blocks until the flag becomes ready or ctx is cancelled. It
	// returns ctx.Err() if the context is cancelled before readiness.
	WaitReady(ctx context.Context) error
}

// Flag is the default Readiness implementation. The zero value is not usable;
// construct one with New.
type Flag struct {
	// ready is an atomic for cheap, lock-free reads. Once set to true it is
	// never reset to false.
	ready atomic.Bool

	// nextID generates the next int32 token id. It starts at 1 so that 0
	// stays reserved.
	nextID atomic.Int32

	// lock guards tokens.
	lock *timedMutex

	// tokens is the set of currently active subscriptions. It is only
	// accessed while holding lock.
	tokens map[Token]struct{}

	// done is closed exactly once when the flag transitions to ready. Closing
	// a channel is the standard-library analogue of the Rust watch-channel
	// broadcast and naturally unblocks every waiter.
	done chan struct{}

	// closeOnce guards closing done so that a double transition (which the
	// atomic guards against in practice) cannot panic.
	closeOnce sync.Once
}

// compile-time assurance that Flag satisfies the Readiness interface.
var _ Readiness = (*Flag)(nil)

// New creates a new, not-yet-ready flag.
func New() *Flag {
	f := &Flag{
		lock:   newTimedMutex(),
		tokens: make(map[Token]struct{}),
		done:   make(chan struct{}),
	}
	f.nextID.Store(1) // Reserve 0.
	return f
}

// loadReady performs an acquire-ordered read of the ready flag.
func (f *Flag) loadReady() bool {
	return f.ready.Load()
}

// signalReady closes the done channel exactly once, broadcasting readiness to
// all waiters. It is safe to call multiple times.
func (f *Flag) signalReady() {
	f.closeOnce.Do(func() { close(f.done) })
}

// String renders the flag for debugging, mirroring the Rust Debug impl which
// exposes only the ready field.
func (f *Flag) String() string {
	return fmt.Sprintf("ReadinessFlag { ready: %t }", f.loadReady())
}

// IsReady reports whether the flag is ready.
//
// If the flag is not yet ready and there are currently no outstanding
// subscribers, IsReady transitions the flag to ready and broadcasts. This
// matches the Rust implementation, which uses a non-blocking try_lock: if the
// token set is observably empty, the flag becomes ready immediately.
func (f *Flag) IsReady() bool {
	if f.loadReady() {
		return true
	}

	if f.lock.tryLock() {
		empty := len(f.tokens) == 0
		f.lock.unlock()
		if empty {
			wasReady := f.ready.Swap(true)
			if !wasReady {
				f.signalReady()
			}
			return true
		}
	}

	return f.loadReady()
}

// Subscribe registers interest in readiness and returns an authorization token.
func (f *Flag) Subscribe(ctx context.Context) (Token, error) {
	if f.loadReady() {
		return Token{}, ErrFlagAlreadyReady
	}

	if err := f.lock.lock(ctx, lockTimeout); err != nil {
		return Token{}, err
	}
	defer f.lock.unlock()

	// Recheck readiness while holding the lock so MarkReady cannot flip the
	// flag between the check above and inserting the token.
	if f.loadReady() {
		return Token{}, ErrFlagAlreadyReady
	}

	// Generate a non-zero, unique token, accounting for int32 wrap-around.
	for {
		token := Token{id: f.nextID.Add(1) - 1}
		if token.id == 0 {
			continue
		}
		if _, exists := f.tokens[token]; exists {
			continue
		}
		f.tokens[token] = struct{}{}
		return token, nil
	}
}

// MarkReady attempts to mark the flag ready using token.
func (f *Flag) MarkReady(ctx context.Context, token Token) (bool, error) {
	if f.loadReady() {
		return false, nil
	}
	if token.id == 0 {
		return false, nil // The reserved zero token is never authorized.
	}

	if err := f.lock.lock(ctx, lockTimeout); err != nil {
		return false, err
	}
	marked := func() bool {
		if _, exists := f.tokens[token]; !exists {
			return false // invalid or already used
		}
		f.ready.Store(true)
		// No further tokens are needed once ready; clear the set.
		f.tokens = make(map[Token]struct{})
		return true
	}()
	f.lock.unlock()

	if !marked {
		return false, nil
	}

	// Best-effort broadcast to any waiters.
	f.signalReady()
	return true, nil
}

// WaitReady blocks until the flag becomes ready or ctx is cancelled.
func (f *Flag) WaitReady(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	// Fast path: calling IsReady can also transition the flag when there are
	// no subscribers, matching the Rust wait_ready which begins with
	// self.is_ready().
	if f.IsReady() {
		return nil
	}

	select {
	case <-f.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
