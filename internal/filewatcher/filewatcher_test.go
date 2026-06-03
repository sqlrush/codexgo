package filewatcher

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

const testThrottleInterval = 50 * time.Millisecond

// recvWithTimeout runs Recv with a deadline, returning the event, ok, and
// whether the call timed out.
func recvWithTimeout(t *testing.T, rx interface {
	Recv(context.Context) (FileWatcherEvent, bool, error)
}, timeout time.Duration) (FileWatcherEvent, bool, bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	ev, ok, err := rx.Recv(ctx)
	timedOut := err == context.DeadlineExceeded
	return ev, ok, timedOut
}

func wantPaths(t *testing.T, got FileWatcherEvent, want ...string) {
	t.Helper()
	expected := FileWatcherEvent{Paths: want}
	if !got.Equal(expected) {
		t.Fatalf("paths = %v, want %v", got.Paths, want)
	}
}

func TestThrottledReceiverCoalescesWithinInterval(t *testing.T) {
	tx, rx := newWatchChannel()
	throttled := NewThrottledWatchReceiver(rx, testThrottleInterval)

	tx.addChangedPaths([]string{"a"})
	first, ok, timedOut := recvWithTimeout(t, throttled, time.Second)
	if timedOut || !ok {
		t.Fatalf("first emit: ok=%v timedOut=%v", ok, timedOut)
	}
	wantPaths(t, first, "a")

	tx.addChangedPaths([]string{"b", "c"})
	_, _, timedOut = recvWithTimeout(t, throttled, testThrottleInterval/2)
	if !timedOut {
		t.Fatalf("expected throttle to block within interval")
	}

	second, ok, timedOut := recvWithTimeout(t, throttled, testThrottleInterval*2)
	if timedOut || !ok {
		t.Fatalf("second emit: ok=%v timedOut=%v", ok, timedOut)
	}
	wantPaths(t, second, "b", "c")
}

func TestThrottledReceiverFlushesPendingOnShutdown(t *testing.T) {
	tx, rx := newWatchChannel()
	throttled := NewThrottledWatchReceiver(rx, testThrottleInterval)

	tx.addChangedPaths([]string{"a"})
	first, ok, timedOut := recvWithTimeout(t, throttled, time.Second)
	if timedOut || !ok {
		t.Fatalf("first emit: ok=%v timedOut=%v", ok, timedOut)
	}
	wantPaths(t, first, "a")

	tx.addChangedPaths([]string{"b"})
	tx.close()

	second, ok, timedOut := recvWithTimeout(t, throttled, time.Second)
	if timedOut || !ok {
		t.Fatalf("shutdown flush: ok=%v timedOut=%v", ok, timedOut)
	}
	wantPaths(t, second, "b")

	_, ok, timedOut = recvWithTimeout(t, throttled, time.Second)
	if timedOut {
		t.Fatalf("closed recv timed out")
	}
	if ok {
		t.Fatalf("expected closed (ok=false) after sender drop")
	}
}

func TestDebouncedReceiverCoalescesEachEventBatch(t *testing.T) {
	tx, rx := newWatchChannel()
	debounced := NewDebouncedWatchReceiver(rx, testThrottleInterval)

	tx.addChangedPaths([]string{"a"})
	first, ok, timedOut := recvWithTimeout(t, debounced, testThrottleInterval*2)
	if timedOut || !ok {
		t.Fatalf("first emit: ok=%v timedOut=%v", ok, timedOut)
	}
	wantPaths(t, first, "a")

	tx.addChangedPaths([]string{"c"})
	_, _, timedOut = recvWithTimeout(t, debounced, testThrottleInterval/2)
	if !timedOut {
		t.Fatalf("expected debounce window to block")
	}

	tx.addChangedPaths([]string{"d"})
	second, ok, timedOut := recvWithTimeout(t, debounced, testThrottleInterval*4)
	if timedOut || !ok {
		t.Fatalf("second emit: ok=%v timedOut=%v", ok, timedOut)
	}
	wantPaths(t, second, "c", "d")
}

func TestDebouncedReceiverFlushesPendingOnShutdown(t *testing.T) {
	tx, rx := newWatchChannel()
	debounced := NewDebouncedWatchReceiver(rx, testThrottleInterval)

	tx.addChangedPaths([]string{"a"})
	tx.close()

	flushed, ok, timedOut := recvWithTimeout(t, debounced, time.Second)
	if timedOut || !ok {
		t.Fatalf("shutdown flush: ok=%v timedOut=%v", ok, timedOut)
	}
	wantPaths(t, flushed, "a")

	_, ok, timedOut = recvWithTimeout(t, debounced, time.Second)
	if timedOut {
		t.Fatalf("closed recv timed out")
	}
	if ok {
		t.Fatalf("expected closed (ok=false) after sender drop")
	}
}

func TestIsMutatingEventFiltersNonMutatingKinds(t *testing.T) {
	tests := []struct {
		name string
		op   fsnotify.Op
		want bool
	}{
		{"create", fsnotify.Create, true},
		{"write", fsnotify.Write, true},
		{"remove", fsnotify.Remove, true},
		{"rename", fsnotify.Rename, true},
		{"chmod", fsnotify.Chmod, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isMutatingEvent(fsnotify.Event{Name: "/tmp/x", Op: tt.op})
			if got != tt.want {
				t.Fatalf("isMutatingEvent(%v) = %v, want %v", tt.op, got, tt.want)
			}
		})
	}
}

func TestRegisterDedupesByPathAndScope(t *testing.T) {
	dir := t.TempDir()
	skills := filepath.Join(dir, "skills")
	otherSkills := filepath.Join(dir, "other-skills")
	mustMkdir(t, skills)
	mustMkdir(t, otherSkills)

	w := NewNoop()
	sub, _ := w.AddSubscriber()
	defer sub.Close()
	w.subscriberRegisterPath(t, sub, skills, false)
	w.subscriberRegisterPath(t, sub, skills, false)
	w.subscriberRegisterPath(t, sub, skills, true)
	w.subscriberRegisterPath(t, sub, otherSkills, true)

	assertCounts(t, w, skills, 2, 1)
	assertCounts(t, w, otherSkills, 0, 1)
}

func TestWatchRegistrationCloseUnregistersPaths(t *testing.T) {
	dir := t.TempDir()
	skills := filepath.Join(dir, "skills")
	mustMkdir(t, skills)

	w := NewNoop()
	sub, _ := w.AddSubscriber()
	defer sub.Close()
	reg := sub.RegisterPath(skills, true)

	reg.Close()

	if _, _, ok := w.WatchCountsForTest(skills); ok {
		t.Fatalf("expected no counts after registration close")
	}
}

func TestSubscriberCloseUnregistersPaths(t *testing.T) {
	dir := t.TempDir()
	skills := filepath.Join(dir, "skills")
	mustMkdir(t, skills)

	w := NewNoop()
	var reg *WatchRegistration
	func() {
		sub, _ := w.AddSubscriber()
		reg = sub.RegisterPath(skills, true)
		sub.Close()
	}()

	if _, _, ok := w.WatchCountsForTest(skills); ok {
		t.Fatalf("expected no counts after subscriber close")
	}
	reg.Close()
}

func TestMissingPathRegistersNearestExistingParent(t *testing.T) {
	dir := t.TempDir()
	missingFile := filepath.Join(dir, "FETCH_HEAD")

	w := NewNoop()
	sub, _ := w.AddSubscriber()
	defer sub.Close()
	reg := sub.RegisterPath(missingFile, false)

	assertCounts(t, w, dir, 1, 0)
	if _, _, ok := w.WatchCountsForTest(missingFile); ok {
		t.Fatalf("missing file should not be watched directly")
	}

	reg.Close()
	if _, _, ok := w.WatchCountsForTest(dir); ok {
		t.Fatalf("expected parent watch released after close")
	}
}

func TestDeeplyMissingPathRegistersNearestExistingDirectoryAncestor(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "refs"), "not a dir")
	missingFile := filepath.Join(dir, "refs", "heads", "main")

	w := NewNoop()
	sub, _ := w.AddSubscriber()
	defer sub.Close()
	_ = sub.RegisterPath(missingFile, false)

	assertCounts(t, w, dir, 1, 0)
}

func TestReceiverClosesWhenSubscriberCloses(t *testing.T) {
	w := NewNoop()
	sub, rx := w.AddSubscriber()

	sub.Close()

	_, ok, timedOut := recvWithTimeout(t, rx, time.Second)
	if timedOut {
		t.Fatalf("closed recv timed out")
	}
	if ok {
		t.Fatalf("expected closed receiver after subscriber close")
	}
}

func TestMatchingSubscribersAreNotified(t *testing.T) {
	w := NewNoop()
	skillsSub, skillsRaw := w.AddSubscriber()
	pluginsSub, pluginsRaw := w.AddSubscriber()
	defer skillsSub.Close()
	defer pluginsSub.Close()
	_ = skillsSub.RegisterPath("/tmp/skills", true)
	_ = pluginsSub.RegisterPath("/tmp/plugins", true)
	skillsRx := NewThrottledWatchReceiver(skillsRaw, testThrottleInterval)
	pluginsRx := NewThrottledWatchReceiver(pluginsRaw, testThrottleInterval)

	w.SendPathsForTest([]string{"/tmp/skills/rust/SKILL.md"})

	ev, ok, timedOut := recvWithTimeout(t, skillsRx, time.Second)
	if timedOut || !ok {
		t.Fatalf("skills change: ok=%v timedOut=%v", ok, timedOut)
	}
	wantPaths(t, ev, "/tmp/skills/rust/SKILL.md")

	_, _, timedOut = recvWithTimeout(t, pluginsRx, testThrottleInterval)
	if !timedOut {
		t.Fatalf("plugins subscriber should not be notified")
	}
}

func TestNonRecursiveWatchIgnoresGrandchildren(t *testing.T) {
	w := NewNoop()
	sub, raw := w.AddSubscriber()
	defer sub.Close()
	_ = sub.RegisterPath("/tmp/skills", false)
	rx := NewThrottledWatchReceiver(raw, testThrottleInterval)

	w.SendPathsForTest([]string{"/tmp/skills/nested/SKILL.md"})

	_, _, timedOut := recvWithTimeout(t, rx, testThrottleInterval)
	if !timedOut {
		t.Fatalf("non-recursive watch should ignore grandchildren")
	}
}

func TestAncestorEventsNotifyChildWatches(t *testing.T) {
	dir := t.TempDir()
	skillsDir := filepath.Join(dir, "skills")
	rustDir := filepath.Join(skillsDir, "rust")
	skillFile := filepath.Join(rustDir, "SKILL.md")
	mustMkdir(t, skillsDir)
	mustMkdir(t, rustDir)
	mustWrite(t, skillFile, "name: rust\n")

	w := NewNoop()
	sub, raw := w.AddSubscriber()
	defer sub.Close()
	_ = sub.RegisterPath(skillFile, false)
	rx := NewThrottledWatchReceiver(raw, testThrottleInterval)

	w.SendPathsForTest([]string{skillsDir})

	ev, ok, timedOut := recvWithTimeout(t, rx, time.Second)
	if timedOut || !ok {
		t.Fatalf("ancestor event: ok=%v timedOut=%v", ok, timedOut)
	}
	wantPaths(t, ev, skillsDir)
}

func TestMissingFileWatchReportsRequestedPathWhenParentChanges(t *testing.T) {
	dir := t.TempDir()
	missingFile := filepath.Join(dir, "FETCH_HEAD")

	w := NewNoop()
	sub, raw := w.AddSubscriber()
	defer sub.Close()
	_ = sub.RegisterPath(missingFile, false)
	rx := NewThrottledWatchReceiver(raw, testThrottleInterval)

	w.SendPathsForTest([]string{filepath.Join(dir, "FETCH_HEAD.lock")})
	_, _, timedOut := recvWithTimeout(t, rx, testThrottleInterval)
	if !timedOut {
		t.Fatalf("sibling event should not match the missing file watch")
	}

	mustWrite(t, missingFile, "origin/main\n")
	w.SendPathsForTest([]string{dir})

	ev, ok, timedOut := recvWithTimeout(t, rx, time.Second)
	if timedOut || !ok {
		t.Fatalf("missing file change: ok=%v timedOut=%v", ok, timedOut)
	}
	wantPaths(t, ev, missingFile)
}

func TestMissingFileWatchReportsRequestedPathOnParentDelete(t *testing.T) {
	dir := t.TempDir()
	missingFile := filepath.Join(dir, "FETCH_HEAD")

	w := NewNoop()
	sub, raw := w.AddSubscriber()
	defer sub.Close()
	_ = sub.RegisterPath(missingFile, false)
	rx := NewThrottledWatchReceiver(raw, testThrottleInterval)

	mustWrite(t, missingFile, "origin/main\n")
	w.SendPathsForTest([]string{dir})
	created, ok, timedOut := recvWithTimeout(t, rx, time.Second)
	if timedOut || !ok {
		t.Fatalf("created event: ok=%v timedOut=%v", ok, timedOut)
	}
	wantPaths(t, created, missingFile)

	if err := os.Remove(missingFile); err != nil {
		t.Fatalf("remove missing file: %v", err)
	}
	w.SendPathsForTest([]string{dir})
	deleted, ok, timedOut := recvWithTimeout(t, rx, time.Second)
	if timedOut || !ok {
		t.Fatalf("deleted event: ok=%v timedOut=%v", ok, timedOut)
	}
	wantPaths(t, deleted, missingFile)
}

func TestMissingDirectoryWatchMovesToCreatedDirectory(t *testing.T) {
	dir := t.TempDir()
	skillsDir := filepath.Join(dir, "skills")
	skillFile := filepath.Join(skillsDir, "SKILL.md")

	w := NewNoop()
	sub, raw := w.AddSubscriber()
	defer sub.Close()
	_ = sub.RegisterPath(skillsDir, false)
	rx := NewThrottledWatchReceiver(raw, testThrottleInterval)

	assertCounts(t, w, dir, 1, 0)
	if _, _, ok := w.WatchCountsForTest(skillsDir); ok {
		t.Fatalf("skills dir should not be watched before creation")
	}

	mustMkdir(t, skillsDir)
	w.SendPathsForTest([]string{dir})

	created, ok, timedOut := recvWithTimeout(t, rx, time.Second)
	if timedOut || !ok {
		t.Fatalf("created dir event: ok=%v timedOut=%v", ok, timedOut)
	}
	wantPaths(t, created, skillsDir)

	if _, _, ok := w.WatchCountsForTest(dir); ok {
		t.Fatalf("parent watch should have moved off dir")
	}
	assertCounts(t, w, skillsDir, 1, 0)

	mustWrite(t, skillFile, "name: rust\n")
	w.SendPathsForTest([]string{skillFile})

	changed, ok, timedOut := recvWithTimeout(t, rx, time.Second)
	if timedOut || !ok {
		t.Fatalf("changed child event: ok=%v timedOut=%v", ok, timedOut)
	}
	wantPaths(t, changed, skillFile)
}

func TestRecursiveRegistrationDowngradesToNonRecursiveAfterClose(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "watched-dir")
	mustMkdir(t, root)

	w, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	sub, _ := w.AddSubscriber()
	defer sub.Close()
	nonRecursive := sub.RegisterPath(root, false)
	recursive := sub.RegisterPath(root, true)

	if rec, ok := w.ModeForTest(root); !ok || !rec {
		t.Fatalf("expected recursive mode, got rec=%v ok=%v", rec, ok)
	}

	recursive.Close()

	if rec, ok := w.ModeForTest(root); !ok || rec {
		t.Fatalf("expected non-recursive mode after recursive close, got rec=%v ok=%v", rec, ok)
	}

	nonRecursive.Close()
}

func TestUnregisterHoldsStateLockUntilUnwatchFinishes(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "watched-dir")
	mustMkdir(t, root)

	w, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	unregisterSub, _ := w.AddSubscriber()
	registerSub, _ := w.AddSubscriber()
	defer registerSub.Close()
	reg := unregisterSub.RegisterPath(root, true)

	// Hold the inner backend lock to stall reconfigure, mirroring the Rust test
	// which holds the inner mutex to block unwatch.
	w.inner.mu.Lock()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		reg.Close()
	}()

	stateLockObserved := false
	for i := 0; i < 100; i++ {
		if !w.stateMu.TryLock() {
			stateLockObserved = true
			break
		}
		w.stateMu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	if !stateLockObserved {
		w.inner.mu.Unlock()
		wg.Wait()
		t.Fatalf("expected state lock to be held during unwatch")
	}

	var registerReg *WatchRegistration
	var registerWG sync.WaitGroup
	registerWG.Add(1)
	go func() {
		defer registerWG.Done()
		registerReg = registerSub.RegisterPath(root, false)
	}()

	w.inner.mu.Unlock()

	wg.Wait()
	registerWG.Wait()

	assertCounts(t, w, root, 1, 0)
	if rec, ok := w.ModeForTest(root); !ok || rec {
		t.Fatalf("expected non-recursive mode, got rec=%v ok=%v", rec, ok)
	}

	registerReg.Close()
	unregisterSub.Close()
}

func TestSpawnEventLoopFiltersNonMutatingEvents(t *testing.T) {
	dir := t.TempDir()
	skillsDir := filepath.Join(dir, "skills")
	skillFile := filepath.Join(skillsDir, "SKILL.md")
	mustMkdir(t, skillsDir)

	w, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer w.Close()
	sub, raw := w.AddSubscriber()
	defer sub.Close()
	_ = sub.RegisterPath(skillsDir, true)
	rx := NewThrottledWatchReceiver(raw, testThrottleInterval)

	// A Chmod (non-mutating) on a child should not notify.
	mustWrite(t, skillFile, "x")
	if err := os.Chmod(skillFile, 0o600); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	// Drain the create/write triggered by the write above, then assert no
	// further events arrive purely from chmod by waiting on a quiet window.
	drainEvents(t, rx, testThrottleInterval*3)

	if err := os.Chmod(skillFile, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	_, _, timedOut := recvWithTimeout(t, rx, testThrottleInterval*2)
	if !timedOut {
		t.Fatalf("chmod-only change should not notify")
	}

	// A real write must notify.
	mustWrite(t, skillFile, "name: rust\n")
	ev, ok, timedOut := recvWithTimeout(t, rx, 2*time.Second)
	if timedOut || !ok {
		t.Fatalf("write event: ok=%v timedOut=%v", ok, timedOut)
	}
	if len(ev.Paths) == 0 {
		t.Fatalf("expected at least one changed path, got none")
	}
}

func TestClosingLiveWatcherStopsEventLoop(t *testing.T) {
	w, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// loopDone must be closed after Close returns.
	select {
	case <-w.loopDone:
	default:
		t.Fatalf("event loop did not stop after Close")
	}
}

// --- helpers ---

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertCounts(t *testing.T, w *FileWatcher, path string, wantNon, wantRec int) {
	t.Helper()
	non, rec, ok := w.WatchCountsForTest(path)
	if !ok {
		t.Fatalf("counts for %s: not present, want (%d,%d)", path, wantNon, wantRec)
	}
	if non != wantNon || rec != wantRec {
		t.Fatalf("counts for %s = (%d,%d), want (%d,%d)", path, non, rec, wantNon, wantRec)
	}
}

func drainEvents(t *testing.T, rx *ThrottledWatchReceiver, quiet time.Duration) {
	t.Helper()
	for {
		_, _, timedOut := recvWithTimeout(t, rx, quiet)
		if timedOut {
			return
		}
	}
}

// subscriberRegisterPath registers a path and intentionally discards the guard
// so the registration persists for count assertions, mirroring the Rust tests'
// `let _first = ...` bindings.
func (fw *FileWatcher) subscriberRegisterPath(t *testing.T, sub *FileWatcherSubscriber, path string, recursive bool) {
	t.Helper()
	_ = sub.RegisterPath(path, recursive)
}
