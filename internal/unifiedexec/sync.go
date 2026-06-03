package unifiedexec

import "sync"

// notify is a minimal broadcast-style port of tokio's Notify::notify_waiters:
// waiters obtain a wait channel via waiter() and select on it; notifyWaiters
// wakes every current waiter. It is safe for concurrent use.
//
// Waiters must call waiter() before checking the condition they wait on so a
// notify that races with an empty check is not missed (the channel returned by
// waiter() is the one that will be closed by the next notifyWaiters).
type notify struct {
	mu sync.Mutex
	ch chan struct{}
}

// newNotify creates an empty notify.
func newNotify() *notify {
	return &notify{ch: make(chan struct{})}
}

// waiter returns a channel that is closed by the next notifyWaiters call.
// Mirrors Notify::notified.
func (n *notify) waiter() <-chan struct{} {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.ch
}

// notifyWaiters wakes every current waiter. Mirrors Notify::notify_waiters.
func (n *notify) notifyWaiters() {
	n.mu.Lock()
	defer n.mu.Unlock()
	close(n.ch)
	n.ch = make(chan struct{})
}

// cancelToken is a minimal port of tokio_util CancellationToken: it can be
// cancelled once, exposing a channel that is closed on cancellation and a
// snapshot predicate. It is safe for concurrent use.
type cancelToken struct {
	mu     sync.Mutex
	done   chan struct{}
	isDone bool
}

// newCancelToken creates an uncancelled token.
func newCancelToken() *cancelToken {
	return &cancelToken{done: make(chan struct{})}
}

// cancel cancels the token, closing its done channel. It is idempotent. Mirrors
// CancellationToken::cancel.
func (c *cancelToken) cancel() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.isDone {
		return
	}
	c.isDone = true
	close(c.done)
}

// isCancelled reports whether the token has been cancelled. Mirrors
// CancellationToken::is_cancelled.
func (c *cancelToken) isCancelled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.isDone
}

// cancelled returns a channel closed when the token is cancelled. Mirrors
// CancellationToken::cancelled.
func (c *cancelToken) cancelled() <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.done
}
