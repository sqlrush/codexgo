//go:build unix

package pty

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSpawnPTYEmitsOutputAndExits(t *testing.T) {
	ctx := context.Background()
	spawned, err := SpawnPTY(ctx, "/bin/sh", []string{"-c", "echo hello-from-pty"}, ".", envMap(), nil, DefaultTerminalSize())
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	out := CombineOutput(spawned.Stdout, spawned.Stderr)
	collected, code := collectUntilExit(t, out, spawned.Exit, 5*time.Second)
	if !strings.Contains(string(collected), "hello-from-pty") {
		t.Fatalf("expected PTY output, got %q", collected)
	}
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
}

func TestSpawnPTYFoldsStderrIntoStdout(t *testing.T) {
	ctx := context.Background()
	spawned, err := SpawnPTY(ctx, "/bin/sh", []string{"-c", "echo on-err >&2"}, ".", envMap(), nil, DefaultTerminalSize())
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	// Stderr channel is closed empty for PTY; output appears on Stdout.
	if _, ok := <-spawned.Stderr; ok {
		t.Fatal("expected PTY Stderr channel to be closed empty")
	}
	collected, code := collectUntilExit(t, spawned.Stdout, spawned.Exit, 5*time.Second)
	if !strings.Contains(string(collected), "on-err") {
		t.Fatalf("expected stderr folded into stdout, got %q", collected)
	}
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
}

func TestSpawnPTYResize(t *testing.T) {
	ctx := context.Background()
	// Print size, wait for a line on stdin, then print size again.
	script := "stty -echo; printf 'start:%s\\n' \"$(stty size)\"; IFS= read _line; printf 'after:%s\\n' \"$(stty size)\""
	spawned, err := SpawnPTY(ctx, "/bin/sh", []string{"-c", script}, ".", envMap(),
		nil, TerminalSize{Rows: 31, Cols: 101})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}

	collected := waitForContains(t, spawned.Stdout, "start:31 101", 5*time.Second)

	if rerr := spawned.Process.Resize(TerminalSize{Rows: 45, Cols: 132}); rerr != nil {
		t.Fatalf("resize: %v", rerr)
	}
	spawned.Process.Stdin() <- []byte("go\n")
	spawned.Process.CloseStdin()

	rest, code := collectUntilExit(t, spawned.Stdout, spawned.Exit, 5*time.Second)
	collected = append(collected, rest...)
	normalized := strings.ReplaceAll(string(collected), "\r\n", "\n")
	if !strings.Contains(normalized, "after:45 132") {
		t.Fatalf("expected resized dimensions in output, got %q", normalized)
	}
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
}

func TestPipeResizeReturnsError(t *testing.T) {
	ctx := context.Background()
	spawned, err := SpawnPipeNoStdin(ctx, "/bin/sh", []string{"-c", "true"}, ".", envMap(), nil)
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if rerr := spawned.Process.Resize(DefaultTerminalSize()); rerr == nil {
		t.Fatal("expected error resizing a pipe-backed process")
	}
	<-spawned.Exit
}

func TestPipeAndPTYShareInterface(t *testing.T) {
	ctx := context.Background()
	pipe, err := SpawnPipeNoStdin(ctx, "/bin/sh", []string{"-c", "echo pipe_ok; sleep 0.05"}, ".", envMap(), nil)
	if err != nil {
		t.Fatalf("spawn pipe: %v", err)
	}
	pty, err := SpawnPTY(ctx, "/bin/sh", []string{"-c", "echo pty_ok; sleep 0.05"}, ".", envMap(), nil, DefaultTerminalSize())
	if err != nil {
		t.Fatalf("spawn pty: %v", err)
	}

	pipeOut, pipeCode := collectUntilExit(t, CombineOutput(pipe.Stdout, pipe.Stderr), pipe.Exit, 3*time.Second)
	ptyOut, ptyCode := collectUntilExit(t, CombineOutput(pty.Stdout, pty.Stderr), pty.Exit, 3*time.Second)

	if pipeCode != 0 || ptyCode != 0 {
		t.Fatalf("exit codes: pipe=%d pty=%d", pipeCode, ptyCode)
	}
	if !strings.Contains(string(pipeOut), "pipe_ok") {
		t.Fatalf("pipe output mismatch: %q", pipeOut)
	}
	if !strings.Contains(string(ptyOut), "pty_ok") {
		t.Fatalf("pty output mismatch: %q", ptyOut)
	}
}

func TestTerminateStopsProcess(t *testing.T) {
	ctx := context.Background()
	spawned, err := SpawnPipeNoStdin(ctx, "/bin/sh", []string{"-c", "sleep 30"}, ".", envMap(), nil)
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	// Give the child a moment to start.
	time.Sleep(50 * time.Millisecond)
	spawned.Process.Terminate()

	select {
	case <-spawned.Exit:
		// killed
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit after Terminate")
	}
}

// waitForContains drains output until the needle appears or the timeout elapses.
func waitForContains(t *testing.T, out <-chan []byte, needle string, timeout time.Duration) []byte {
	t.Helper()
	var collected []byte
	deadline := time.After(timeout)
	for {
		select {
		case chunk, ok := <-out:
			if !ok {
				t.Fatalf("output closed before %q appeared; got %q", needle, collected)
			}
			collected = append(collected, chunk...)
			if strings.Contains(string(collected), needle) {
				return collected
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %q; got %q", needle, collected)
		}
	}
}
