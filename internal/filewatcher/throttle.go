package filewatcher

import (
	"context"
	"time"
)

// ThrottledWatchReceiver coalesces bursts of watch notifications and emits at
// most once per interval.
//
// It mirrors codex's ThrottledWatchReceiver: after emitting an event, the next
// emission is delayed until at least interval has elapsed.
type ThrottledWatchReceiver struct {
	rx          *Receiver
	interval    time.Duration
	nextAllowed time.Time
	hasNext     bool
}

// NewThrottledWatchReceiver wraps rx so emissions are spaced by at least
// interval.
func NewThrottledWatchReceiver(rx *Receiver, interval time.Duration) *ThrottledWatchReceiver {
	return &ThrottledWatchReceiver{rx: rx, interval: interval}
}

// Recv receives the next event, enforcing the configured minimum delay after
// the previous emission. It returns ok=false when the underlying receiver is
// closed, or an error if ctx is cancelled.
func (t *ThrottledWatchReceiver) Recv(ctx context.Context) (FileWatcherEvent, bool, error) {
	if t.hasNext {
		if err := sleepUntil(ctx, t.nextAllowed); err != nil {
			return FileWatcherEvent{}, false, err
		}
	}

	event, ok, err := t.rx.Recv(ctx)
	if ok {
		t.nextAllowed = time.Now().Add(t.interval)
		t.hasNext = true
	}
	return event, ok, err
}

// DebouncedWatchReceiver coalesces file watcher notifications that arrive within
// a fixed debounce window after the first event in each batch.
//
// It mirrors codex's DebouncedWatchReceiver: once at least one path is pending,
// it keeps accumulating for up to interval (or until the source closes) before
// emitting the merged, sorted batch.
type DebouncedWatchReceiver struct {
	rx       *Receiver
	interval time.Duration
	changed  map[string]struct{}
}

// NewDebouncedWatchReceiver wraps rx with a debounce window of interval.
func NewDebouncedWatchReceiver(rx *Receiver, interval time.Duration) *DebouncedWatchReceiver {
	return &DebouncedWatchReceiver{
		rx:       rx,
		interval: interval,
		changed:  make(map[string]struct{}),
	}
}

// Recv receives the next debounced event batch. It returns ok=false only when
// the source is closed and nothing remains buffered, or an error if ctx is
// cancelled.
func (d *DebouncedWatchReceiver) Recv(ctx context.Context) (FileWatcherEvent, bool, error) {
	for len(d.changed) == 0 {
		event, ok, err := d.rx.Recv(ctx)
		if err != nil {
			return FileWatcherEvent{}, false, err
		}
		if !ok {
			return FileWatcherEvent{}, false, nil
		}
		for _, p := range event.Paths {
			d.changed[p] = struct{}{}
		}
	}

	deadline := time.Now().Add(d.interval)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		windowCtx, cancel := context.WithDeadline(ctx, deadline)
		event, ok, err := d.rx.Recv(windowCtx)
		cancel()
		if err != nil {
			// Distinguish window expiry (emit batch) from caller cancellation.
			if ctx.Err() != nil {
				return FileWatcherEvent{}, false, ctx.Err()
			}
			break
		}
		if !ok {
			break
		}
		for _, p := range event.Paths {
			d.changed[p] = struct{}{}
		}
	}

	paths := drainSorted(d.changed)
	return FileWatcherEvent{Paths: paths}, true, nil
}

// sleepUntil blocks until deadline or ctx cancellation. It returns ctx.Err() if
// cancelled, nil otherwise.
func sleepUntil(ctx context.Context, deadline time.Time) error {
	d := time.Until(deadline)
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
