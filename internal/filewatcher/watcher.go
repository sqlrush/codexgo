package filewatcher

import (
	"sync"

	"github.com/fsnotify/fsnotify"
)

// subscriberID identifies a single logical consumer.
type subscriberID uint64

// subscriberState holds one subscriber's registrations and its event sender. It
// mirrors codex's SubscriberState.
type subscriberState struct {
	watchedPaths map[subscriberWatchKey]*subscriberWatchState
	tx           *watchSender
}

// watchState is the shared registration bookkeeping guarded by FileWatcher's
// state lock. It mirrors codex's WatchState.
type watchState struct {
	nextSubscriberID subscriberID
	pathRefCounts    map[string]pathWatchCounts
	subscribers      map[subscriberID]*subscriberState
}

// FileWatcher is a multi-subscriber file watcher built on top of fsnotify.
//
// Construct it with [New] for a live watcher or [NewNoop] for an inert watcher
// usable only with synthetic notifications (tests). Always call [FileWatcher.Close]
// on a live watcher to release OS resources.
type FileWatcher struct {
	inner   *fileWatcherInner // nil for a noop watcher
	stateMu sync.RWMutex
	state   *watchState

	loopDone chan struct{}
	logf     func(string, ...any)
}

// New creates a live filesystem watcher and starts its background event loop.
func New() (*FileWatcher, error) {
	inner, err := newFileWatcherInner()
	if err != nil {
		return nil, err
	}
	fw := &FileWatcher{
		inner:    inner,
		state:    newWatchState(),
		loopDone: make(chan struct{}),
		logf:     func(string, ...any) {},
	}
	fw.spawnEventLoop()
	return fw, nil
}

// NewNoop creates an inert watcher that only supports synthetic notifications
// via [FileWatcher.SendPathsForTest]. It performs no OS watching.
func NewNoop() *FileWatcher {
	return &FileWatcher{
		inner: nil,
		state: newWatchState(),
		logf:  func(string, ...any) {},
	}
}

// SetLogf installs a logging hook used for non-fatal watch/unwatch failures,
// mirroring codex's tracing::warn! calls. It must be called before any
// registrations to avoid data races; passing nil restores the silent default.
func (fw *FileWatcher) SetLogf(logf func(string, ...any)) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	fw.logf = logf
}

// newWatchState returns an empty registration store.
func newWatchState() *watchState {
	return &watchState{
		pathRefCounts: make(map[string]pathWatchCounts),
		subscribers:   make(map[subscriberID]*subscriberState),
	}
}

// AddSubscriber adds a new subscriber and returns both its registration handle
// and its dedicated event receiver. It mirrors codex's add_subscriber.
func (fw *FileWatcher) AddSubscriber() (*FileWatcherSubscriber, *Receiver) {
	tx, rx := newWatchChannel()

	fw.stateMu.Lock()
	id := fw.state.nextSubscriberID
	fw.state.nextSubscriberID++
	fw.state.subscribers[id] = &subscriberState{
		watchedPaths: make(map[subscriberWatchKey]*subscriberWatchState),
		tx:           tx,
	}
	fw.stateMu.Unlock()

	return &FileWatcherSubscriber{id: id, fw: fw}, rx
}

// subscriberWatchRegistration is registration-time data before it is merged into
// subscriber state, mirroring codex's SubscriberWatchRegistration.
type subscriberWatchRegistration struct {
	key      subscriberWatchKey
	actual   WatchPath
	fallback bool
}

// registerPaths reference-counts the provided registrations for subscriberID and
// (re)configures OS watches as needed. It mirrors codex's register_paths.
func (fw *FileWatcher) registerPaths(id subscriberID, regs []subscriberWatchRegistration) {
	fw.stateMu.Lock()
	defer fw.stateMu.Unlock()

	for _, reg := range regs {
		sub, ok := fw.state.subscribers[id]
		if !ok {
			return
		}

		var actual WatchPath
		if existing, ok := sub.watchedPaths[reg.key]; ok {
			existing.count++
			actual = existing.actual
		} else {
			sub.watchedPaths[reg.key] = &subscriberWatchState{
				actual:     reg.actual,
				count:      1,
				lastExists: pathExists(reg.key.matched.Path),
				fallback:   reg.fallback,
			}
			actual = reg.actual
		}

		counts := fw.state.pathRefCounts[actual.Path]
		prevMode := counts.effectiveMode()
		counts = counts.increment(actual.Recursive, 1)
		fw.state.pathRefCounts[actual.Path] = counts
		nextMode := counts.effectiveMode()
		if prevMode != nextMode {
			fw.reconfigure(actual.Path, nextMode)
		}
	}
}

// unregisterPaths decrements reference counts for the provided keys and tears
// down OS watches that are no longer needed. It mirrors codex's unregister_paths.
func (fw *FileWatcher) unregisterPaths(id subscriberID, keys []subscriberWatchKey) {
	fw.stateMu.Lock()
	defer fw.stateMu.Unlock()

	for _, key := range keys {
		sub, ok := fw.state.subscribers[id]
		if !ok {
			return
		}
		ws, ok := sub.watchedPaths[key]
		if !ok {
			continue
		}
		actual := ws.actual
		ws.count = saturatingSub(ws.count, 1)
		if ws.count == 0 {
			delete(sub.watchedPaths, key)
		}

		counts, ok := fw.state.pathRefCounts[actual.Path]
		if !ok {
			continue
		}
		prevMode := counts.effectiveMode()
		counts = counts.decrement(actual.Recursive, 1)
		nextMode := counts.effectiveMode()
		if counts.isEmpty() {
			delete(fw.state.pathRefCounts, actual.Path)
		} else {
			fw.state.pathRefCounts[actual.Path] = counts
		}
		if prevMode != nextMode {
			fw.reconfigure(actual.Path, nextMode)
		}
	}
}

// removeSubscriber drops a subscriber and all of its registrations, releasing OS
// watches and closing its event sender. It mirrors codex's remove_subscriber.
func (fw *FileWatcher) removeSubscriber(id subscriberID) {
	fw.stateMu.Lock()
	sub, ok := fw.state.subscribers[id]
	if !ok {
		fw.stateMu.Unlock()
		return
	}
	delete(fw.state.subscribers, id)

	for _, ws := range sub.watchedPaths {
		counts, ok := fw.state.pathRefCounts[ws.actual.Path]
		if !ok {
			continue
		}
		prevMode := counts.effectiveMode()
		counts = counts.decrement(ws.actual.Recursive, ws.count)
		nextMode := counts.effectiveMode()
		if counts.isEmpty() {
			delete(fw.state.pathRefCounts, ws.actual.Path)
		} else {
			fw.state.pathRefCounts[ws.actual.Path] = counts
		}
		if prevMode != nextMode {
			fw.reconfigure(ws.actual.Path, nextMode)
		}
	}
	tx := sub.tx
	fw.stateMu.Unlock()

	// Close the stored sender outside the state lock; this wakes the receiver so
	// Recv returns ok=false (channel closed).
	tx.close()
}

// reconfigure forwards to the backend when present. It assumes the state lock is
// held, preserving codex's ordering where unwatch happens under the state lock.
func (fw *FileWatcher) reconfigure(path string, nextMode recursiveMode) {
	if fw.inner == nil {
		return
	}
	fw.inner.reconfigure(path, nextMode, fw.logf)
}

// applyActualWatchMove migrates ref counts (and OS watches) from oldActual to
// newActual when a fallback watch's actual path changes. It mirrors codex's
// apply_actual_watch_move and assumes the state lock is held.
func (fw *FileWatcher) applyActualWatchMove(oldActual, newActual WatchPath, count int) {
	if oldActual == newActual {
		return
	}

	if counts, ok := fw.state.pathRefCounts[oldActual.Path]; ok {
		prevMode := counts.effectiveMode()
		counts = counts.decrement(oldActual.Recursive, count)
		nextMode := counts.effectiveMode()
		if counts.isEmpty() {
			delete(fw.state.pathRefCounts, oldActual.Path)
		} else {
			fw.state.pathRefCounts[oldActual.Path] = counts
		}
		if prevMode != nextMode {
			fw.reconfigure(oldActual.Path, nextMode)
		}
	}

	counts := fw.state.pathRefCounts[newActual.Path]
	prevMode := counts.effectiveMode()
	counts = counts.increment(newActual.Recursive, count)
	fw.state.pathRefCounts[newActual.Path] = counts
	nextMode := counts.effectiveMode()
	if prevMode != nextMode {
		fw.reconfigure(newActual.Path, nextMode)
	}
}

// spawnEventLoop bridges fsnotify's channels into subscriber notifications. It
// mirrors codex's spawn_event_loop.
func (fw *FileWatcher) spawnEventLoop() {
	inner := fw.inner
	go func() {
		defer close(fw.loopDone)
		events := inner.watcher.Events
		errs := inner.watcher.Errors
		for {
			select {
			case event, ok := <-events:
				if !ok {
					return
				}
				if !isMutatingEvent(event) {
					continue
				}
				if event.Name == "" {
					continue
				}
				fw.notifySubscribers([]string{event.Name})
			case err, ok := <-errs:
				if !ok {
					return
				}
				if err != nil {
					fw.logf("filewatcher: error: %v", err)
				}
			}
		}
	}()
}

// notifySubscribers matches eventPaths against all subscribers, migrates any
// fallback watches whose actual path advanced, and delivers changed paths. It
// mirrors codex's notify_subscribers.
func (fw *FileWatcher) notifySubscribers(eventPaths []string) {
	type pending struct {
		tx    *watchSender
		paths []string
	}

	fw.stateMu.Lock()

	type move struct {
		oldActual WatchPath
		newActual WatchPath
		count     int
	}
	var moves []move
	var toNotify []pending

	for _, sub := range fw.state.subscribers {
		var changed []string
		for _, eventPath := range eventPaths {
			for key, ws := range sub.watchedPaths {
				if p, ok := changedPathForEvent(key, ws, eventPath); ok {
					changed = append(changed, p)
				}

				newActual, _, fallback := actualWatchPath(key.requested)
				if fallback {
					ws.fallback = true
				}
				if ws.actual != newActual {
					moves = append(moves, move{
						oldActual: ws.actual,
						newActual: newActual,
						count:     ws.count,
					})
					ws.actual = newActual
				}
			}
		}
		if len(changed) > 0 {
			toNotify = append(toNotify, pending{tx: sub.tx.clone(), paths: changed})
		}
	}

	for _, m := range moves {
		fw.applyActualWatchMove(m.oldActual, m.newActual, m.count)
	}
	fw.stateMu.Unlock()

	for _, p := range toNotify {
		p.tx.addChangedPaths(p.paths)
		p.tx.close()
	}
}

// SendPathsForTest injects synthetic event paths as if delivered by the OS
// backend. It mirrors codex's send_paths_for_test and is intended for tests of
// the matching and notification logic.
func (fw *FileWatcher) SendPathsForTest(paths []string) {
	fw.notifySubscribers(paths)
}

// Close shuts down the live backend and waits for the event loop to exit. It is
// a no-op for a noop watcher. Existing subscribers' receivers are unaffected by
// Close itself; close their subscribers to release them.
func (fw *FileWatcher) Close() error {
	if fw.inner == nil {
		return nil
	}
	err := fw.inner.close()
	if fw.loopDone != nil {
		<-fw.loopDone
	}
	return err
}

// isMutatingEvent reports whether an fsnotify event represents a create, write,
// or remove/rename, mirroring codex's is_mutating_event (Create/Modify/Remove).
// fsnotify splits Modify into Write/Chmod and Remove into Remove/Rename; Write,
// Remove, and Rename are treated as mutations, Chmod is not (it is an attribute
// change, analogous to notify's non-mutating metadata events being filtered).
func isMutatingEvent(event fsnotify.Event) bool {
	return event.Op.Has(fsnotify.Create) ||
		event.Op.Has(fsnotify.Write) ||
		event.Op.Has(fsnotify.Remove) ||
		event.Op.Has(fsnotify.Rename)
}
