package pty

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// envMap returns the current process environment as a map, like the Rust tests'
// std::env::vars().collect().
func envMap() map[string]string {
	out := make(map[string]string)
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			out[kv[:i]] = kv[i+1:]
		}
	}
	return out
}

// collectUntilExit drains combined output until the exit code arrives or the
// timeout elapses, returning the collected bytes and the exit code.
func collectUntilExit(t *testing.T, out <-chan []byte, exit <-chan int, timeout time.Duration) ([]byte, int) {
	t.Helper()
	var collected []byte
	deadline := time.After(timeout)
	for {
		select {
		case chunk, ok := <-out:
			if ok {
				collected = append(collected, chunk...)
			} else {
				out = nil
			}
		case code := <-exit:
			// Drain any remaining buffered output briefly.
			drain := time.After(200 * time.Millisecond)
		drainLoop:
			for {
				select {
				case chunk, ok := <-out:
					if !ok {
						break drainLoop
					}
					collected = append(collected, chunk...)
				case <-drain:
					break drainLoop
				}
			}
			return collected, code
		case <-deadline:
			return collected, -1
		}
	}
}

func TestSpawnPipeRoundTripsStdin(t *testing.T) {
	ctx := context.Background()
	spawned, err := SpawnPipe(ctx, "/bin/cat", nil, ".", envMap(), nil)
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	spawned.Process.Stdin() <- []byte("roundtrip\n")
	spawned.Process.CloseStdin()

	out := CombineOutput(spawned.Stdout, spawned.Stderr)
	collected, code := collectUntilExit(t, out, spawned.Exit, 5*time.Second)
	if !strings.Contains(string(collected), "roundtrip") {
		t.Fatalf("expected echoed stdin, got %q", collected)
	}
	if code != 0 {
		t.Fatalf("expected clean exit, got %d", code)
	}
}

func TestSpawnPipeSplitStdoutStderr(t *testing.T) {
	ctx := context.Background()
	script := "printf 'split-out\\n'; printf 'split-err\\n' >&2"
	spawned, err := SpawnPipeNoStdin(ctx, "/bin/sh", []string{"-c", script}, ".", envMap(), nil)
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}

	var stdout, stderr []byte
	stdoutDone := make(chan struct{})
	stderrDone := make(chan struct{})
	go func() {
		for c := range spawned.Stdout {
			stdout = append(stdout, c...)
		}
		close(stdoutDone)
	}()
	go func() {
		for c := range spawned.Stderr {
			stderr = append(stderr, c...)
		}
		close(stderrDone)
	}()

	code := <-spawned.Exit
	<-stdoutDone
	<-stderrDone

	if string(stdout) != "split-out\n" {
		t.Fatalf("stdout = %q, want %q", stdout, "split-out\n")
	}
	if string(stderr) != "split-err\n" {
		t.Fatalf("stderr = %q, want %q", stderr, "split-err\n")
	}
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
}

func TestSpawnPipeReportsNonZeroExit(t *testing.T) {
	ctx := context.Background()
	spawned, err := SpawnPipeNoStdin(ctx, "/bin/sh", []string{"-c", "exit 7"}, ".", envMap(), nil)
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	out := CombineOutput(spawned.Stdout, spawned.Stderr)
	_, code := collectUntilExit(t, out, spawned.Exit, 5*time.Second)
	if code != 7 {
		t.Fatalf("exit = %d, want 7", code)
	}
	if c, ok := spawned.Process.ExitCode(); !ok || c != 7 {
		t.Fatalf("ExitCode() = (%d, %v), want (7, true)", c, ok)
	}
	if !spawned.Process.HasExited() {
		t.Fatalf("HasExited() = false, want true")
	}
}

func TestSpawnPipeMissingProgram(t *testing.T) {
	_, err := SpawnPipe(context.Background(), "", nil, ".", envMap(), nil)
	if err == nil {
		t.Fatal("expected error for empty program")
	}
}

func TestSpawnPipeArg0Override(t *testing.T) {
	ctx := context.Background()
	arg0 := "custom-name"
	// `sh -c 'echo $0'` prints argv[0] of the shell.
	spawned, err := SpawnPipeNoStdin(ctx, "/bin/sh", []string{"-c", "echo $0"}, ".", envMap(), &arg0)
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	out := CombineOutput(spawned.Stdout, spawned.Stderr)
	collected, code := collectUntilExit(t, out, spawned.Exit, 5*time.Second)
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(string(collected), "custom-name") {
		t.Fatalf("expected arg0 override in output, got %q", collected)
	}
}
