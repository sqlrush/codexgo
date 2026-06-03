package filewatcher

import (
	"fmt"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// fileWatcherInner owns the live fsnotify watcher and tracks which paths are
// currently registered with it and in what mode. It mirrors codex's
// FileWatcherInner.
//
// fsnotify has no recursive mode, so recursiveMode is recorded only to detect
// reconfiguration; the OS watch is always added non-recursively. Recursive
// semantics are enforced by the event-matching layer.
type fileWatcherInner struct {
	mu           sync.Mutex
	watcher      *fsnotify.Watcher
	watchedPaths map[string]recursiveMode
}

// newFileWatcherInner constructs the backend around an fsnotify watcher.
func newFileWatcherInner() (*fileWatcherInner, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("filewatcher: create fsnotify watcher: %w", err)
	}
	return &fileWatcherInner{
		watcher:      w,
		watchedPaths: make(map[string]recursiveMode),
	}, nil
}

// reconfigure adjusts the OS watch for path to nextMode. It unwatches when the
// mode changes or drops to none, and (re)watches existing paths. Missing paths
// are skipped, mirroring codex's reconfigure_watch_inner. Errors are surfaced
// via the optional logf hook rather than returned, matching the warn! calls.
func (in *fileWatcherInner) reconfigure(path string, nextMode recursiveMode, logf func(string, ...any)) {
	in.mu.Lock()
	defer in.mu.Unlock()

	existing, watched := in.watchedPaths[path]
	if watched && existing == nextMode {
		return
	}

	if watched {
		if err := in.watcher.Remove(path); err != nil {
			logf("filewatcher: failed to unwatch %s: %v", path, err)
		}
		delete(in.watchedPaths, path)
	}

	if nextMode == modeNone {
		return
	}
	if !pathExists(path) {
		return
	}

	if err := in.watcher.Add(path); err != nil {
		logf("filewatcher: failed to watch %s: %v", path, err)
		return
	}
	in.watchedPaths[path] = nextMode
}

// mode returns the recorded OS watch mode for path, for tests.
func (in *fileWatcherInner) mode(path string) (recursiveMode, bool) {
	in.mu.Lock()
	defer in.mu.Unlock()
	m, ok := in.watchedPaths[path]
	return m, ok
}

// close shuts down the underlying fsnotify watcher and closes its channels.
func (in *fileWatcherInner) close() error {
	in.mu.Lock()
	defer in.mu.Unlock()
	if err := in.watcher.Close(); err != nil {
		return fmt.Errorf("filewatcher: close fsnotify watcher: %w", err)
	}
	in.watchedPaths = make(map[string]recursiveMode)
	return nil
}
