package filewatcher

import (
	"context"
	"sort"
	"sync"
)

// Receiver receives coalesced change notifications for a single subscriber.
//
// It mirrors codex's async `Receiver`: changed paths accumulate in a
// deduplicated set and are drained, sorted, on each [Receiver.Recv] call. Recv
// returns ok=false once the owning subscriber has been removed and no further
// events can arrive.
type Receiver struct {
	inner *receiverInner
}

// receiverInner is shared between a Receiver and its watchSenders.
type receiverInner struct {
	mu          sync.Mutex
	cond        *sync.Cond
	changed     map[string]struct{}
	senderCount int
}

// watchSender pushes changed paths into a receiver. It is reference counted: a
// Go analogue of codex's Clone/Drop on the Rust WatchSender. The last sender to
// close wakes any waiting receiver so Recv can observe closure.
type watchSender struct {
	inner  *receiverInner
	closed bool
}

// newWatchChannel creates a linked (watchSender, *Receiver) pair with a single
// active sender, mirroring codex's watch_channel.
func newWatchChannel() (*watchSender, *Receiver) {
	inner := &receiverInner{
		changed:     make(map[string]struct{}),
		senderCount: 1,
	}
	inner.cond = sync.NewCond(&inner.mu)
	return &watchSender{inner: inner}, &Receiver{inner: inner}
}

// addChangedPaths extends the receiver's pending set with paths, waking the
// receiver only when the set actually grows (matching codex's notify-on-change).
func (s *watchSender) addChangedPaths(paths []string) {
	if len(paths) == 0 {
		return
	}
	s.inner.mu.Lock()
	defer s.inner.mu.Unlock()
	grew := false
	for _, p := range paths {
		if _, ok := s.inner.changed[p]; !ok {
			s.inner.changed[p] = struct{}{}
			grew = true
		}
	}
	if grew {
		s.inner.cond.Signal()
	}
}

// clone produces an additional active sender, mirroring Rust's Clone which
// increments the sender count.
func (s *watchSender) clone() *watchSender {
	s.inner.mu.Lock()
	s.inner.senderCount++
	s.inner.mu.Unlock()
	return &watchSender{inner: s.inner}
}

// close releases this sender. When the final sender closes, waiting receivers
// are woken so Recv can observe closure and return ok=false. close is
// idempotent.
func (s *watchSender) close() {
	if s.closed {
		return
	}
	s.closed = true
	s.inner.mu.Lock()
	s.inner.senderCount--
	last := s.inner.senderCount == 0
	if last {
		s.inner.cond.Broadcast()
	}
	s.inner.mu.Unlock()
}

// Recv waits for the next batch of changed paths.
//
// It returns the coalesced event and ok=true on success. It returns ok=false
// when the subscriber has been removed and no more events can arrive, or when
// ctx is cancelled (the returned error is ctx.Err() in that case, nil
// otherwise).
func (r *Receiver) Recv(ctx context.Context) (FileWatcherEvent, bool, error) {
	inner := r.inner

	// Wake the cond-wait goroutine if ctx is cancelled so Recv does not block
	// forever. The broadcast is harmless to other waiters.
	stop := context.AfterFunc(ctx, func() {
		inner.mu.Lock()
		inner.cond.Broadcast()
		inner.mu.Unlock()
	})
	defer stop()

	inner.mu.Lock()
	defer inner.mu.Unlock()
	for {
		if err := ctx.Err(); err != nil {
			return FileWatcherEvent{}, false, err
		}
		if len(inner.changed) > 0 {
			paths := drainSorted(inner.changed)
			return FileWatcherEvent{Paths: paths}, true, nil
		}
		if inner.senderCount == 0 {
			return FileWatcherEvent{}, false, nil
		}
		inner.cond.Wait()
	}
}

// drainSorted empties set and returns its keys in sorted order, matching the
// BTreeSet ordering codex relies on for deterministic event payloads.
func drainSorted(set map[string]struct{}) []string {
	paths := make([]string, 0, len(set))
	for p := range set {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		delete(set, p)
	}
	return paths
}
