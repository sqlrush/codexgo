package unifiedexec

import (
	"sync"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/sandbox"
)

// captureSink records the deltas and the single end emission the watcher
// produces. It is the test double for the core event sink the watcher drives.
type captureSink struct {
	mu     sync.Mutex
	deltas [][]byte
	end    *ExecEndInfo
	endCh  chan struct{}
}

func newCaptureSink() *captureSink {
	return &captureSink{endCh: make(chan struct{})}
}

func (c *captureSink) OutputDelta(_ string, chunk []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := make([]byte, len(chunk))
	copy(cp, chunk)
	c.deltas = append(c.deltas, cp)
}

func (c *captureSink) ExecEnd(info ExecEndInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.end != nil {
		return
	}
	cp := info
	c.end = &cp
	close(c.endCh)
}

func (c *captureSink) waitEnd(t *testing.T) ExecEndInfo {
	t.Helper()
	select {
	case <-c.endCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for ExecEnd")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return *c.end
}

func (c *captureSink) joinedDeltas() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []byte
	for _, d := range c.deltas {
		out = append(out, d...)
	}
	return string(out)
}

// newWatchableProcess builds a bare Process whose output/exit state the test can
// drive directly (no real PTY), exercising the watcher's subscription path.
func newWatchableProcess() *Process {
	return newProcess(sandbox.SandboxTypeNone)
}

// TestExitWatcherStreamsThenEmitsEnd asserts the watcher streams output deltas
// while a session runs and emits a single late exec end with the aggregated
// transcript, exit code, and originating call id when the process exits.
func TestExitWatcherStreamsThenEmitsEnd(t *testing.T) {
	t.Parallel()
	proc := newWatchableProcess()
	sink := newCaptureSink()
	transcript := NewDefaultHeadTailBuffer()
	var transcriptMu sync.Mutex

	info := WatcherInfo{
		CallID:    "call-orig",
		Command:   []string{"/bin/sh", "-c", "echo hi"},
		Cwd:       "/work",
		ProcessID: 4242,
		StartedAt: time.Now(),
	}
	StartSessionWatcher(proc, transcript, &transcriptMu, info, sink)

	// Stream output while the session is live.
	proc.pushOutput([]byte("hello "))
	proc.pushOutput([]byte("world\n"))

	// Wait briefly for the streaming goroutine to drain the broadcast.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if sink.joinedDeltas() == "hello world\n" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := sink.joinedDeltas(); got != "hello world\n" {
		t.Fatalf("streamed deltas = %q, want %q", got, "hello world\n")
	}

	// Now the process exits; the watcher must emit the late end.
	code := 0
	proc.markExited(&code)
	proc.Terminate()

	end := sink.waitEnd(t)
	if end.CallID != "call-orig" {
		t.Errorf("end.CallID = %q, want call-orig", end.CallID)
	}
	if end.ProcessID != 4242 {
		t.Errorf("end.ProcessID = %d, want 4242", end.ProcessID)
	}
	if end.ExitCode != 0 {
		t.Errorf("end.ExitCode = %d, want 0", end.ExitCode)
	}
	if end.Failure != "" {
		t.Errorf("end.Failure = %q, want empty", end.Failure)
	}
	if end.Output != "hello world\n" {
		t.Errorf("end.Output = %q, want aggregated transcript", end.Output)
	}
	if len(end.Command) != 3 || end.Command[2] != "echo hi" {
		t.Errorf("end.Command = %v", end.Command)
	}
}

// TestExitWatcherEmitsFailedEnd asserts a recorded failure produces a failed end
// with the failure message threaded through.
func TestExitWatcherEmitsFailedEnd(t *testing.T) {
	t.Parallel()
	proc := newWatchableProcess()
	sink := newCaptureSink()
	transcript := NewDefaultHeadTailBuffer()
	var transcriptMu sync.Mutex

	StartSessionWatcher(proc, transcript, &transcriptMu, WatcherInfo{
		CallID:    "c1",
		Command:   []string{"boom"},
		Cwd:       "/work",
		ProcessID: 7,
		StartedAt: time.Now(),
	}, sink)

	proc.recordFailure("spawn blew up")

	end := sink.waitEnd(t)
	if end.Failure != "spawn blew up" {
		t.Errorf("end.Failure = %q, want spawn blew up", end.Failure)
	}
	if end.ExitCode != -1 {
		t.Errorf("end.ExitCode = %d, want -1 for failure", end.ExitCode)
	}
}

// TestExitWatcherTerminateEndsWatcher asserts terminating the process without an
// exit code still finalizes the watcher (no goroutine leak), emitting an end
// with the default exit code.
func TestExitWatcherTerminateEndsWatcher(t *testing.T) {
	t.Parallel()
	proc := newWatchableProcess()
	sink := newCaptureSink()
	transcript := NewDefaultHeadTailBuffer()
	var transcriptMu sync.Mutex

	StartSessionWatcher(proc, transcript, &transcriptMu, WatcherInfo{
		CallID: "c1", Command: []string{"sleep"}, Cwd: "/w", ProcessID: 9, StartedAt: time.Now(),
	}, sink)

	proc.Terminate()

	end := sink.waitEnd(t)
	if end.ExitCode != -1 {
		t.Errorf("end.ExitCode = %d, want -1 when no code is known", end.ExitCode)
	}
}

// TestSplitValidUTF8Prefix ports the Rust async_watcher tests for the UTF-8
// boundary splitter.
func TestSplitValidUTF8Prefix(t *testing.T) {
	t.Parallel()

	t.Run("respects max bytes for ascii", func(t *testing.T) {
		buf := []byte("hello word!")
		first, ok := splitValidUTF8PrefixWithMax(&buf, 5)
		if !ok || string(first) != "hello" {
			t.Fatalf("first = %q ok=%v, want hello", first, ok)
		}
		if string(buf) != " word!" {
			t.Fatalf("buf = %q, want ' word!'", buf)
		}
		second, ok := splitValidUTF8PrefixWithMax(&buf, 5)
		if !ok || string(second) != " word" {
			t.Fatalf("second = %q, want ' word'", second)
		}
		if string(buf) != "!" {
			t.Fatalf("buf = %q, want '!'", buf)
		}
	})

	t.Run("avoids splitting utf8 codepoints", func(t *testing.T) {
		buf := []byte("ééé")
		first, ok := splitValidUTF8PrefixWithMax(&buf, 3)
		if !ok || string(first) != "é" {
			t.Fatalf("first = %q, want é", first)
		}
		if string(buf) != "éé" {
			t.Fatalf("buf = %q, want éé", buf)
		}
	})

	t.Run("makes progress on invalid utf8", func(t *testing.T) {
		buf := []byte{0xff, 'a', 'b'}
		first, ok := splitValidUTF8PrefixWithMax(&buf, 2)
		if !ok || len(first) != 1 || first[0] != 0xff {
			t.Fatalf("first = %v, want [0xff]", first)
		}
		if string(buf) != "ab" {
			t.Fatalf("buf = %q, want ab", buf)
		}
	})
}
