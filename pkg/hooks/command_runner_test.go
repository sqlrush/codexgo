package hooks

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// unixOnly skips the test on Windows, where these shell snippets do not apply.
func unixOnly(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("test relies on a POSIX shell")
	}
}

func TestRunCommandExitZeroCapturesStdoutAndStdin(t *testing.T) {
	unixOnly(t)
	handler := makeHandler(t, protocol.HookEventNamePreToolUse, nil, "cat", 0)
	got := runCommand(context.Background(), CommandShell{}, handler, "payload-bytes", t.TempDir())
	if got.error != nil {
		t.Fatalf("unexpected error: %v", *got.error)
	}
	if got.exitCode == nil || *got.exitCode != 0 {
		t.Fatalf("exitCode = %v, want 0", got.exitCode)
	}
	// `cat` with no args echoes stdin: confirms the payload reached the process.
	if strings.TrimSpace(got.stdout) != "payload-bytes" {
		t.Errorf("stdout = %q, want payload echoed back", got.stdout)
	}
}

func TestRunCommandNonZeroExit(t *testing.T) {
	unixOnly(t)
	handler := makeHandler(t, protocol.HookEventNamePreToolUse, nil, "exit 3", 0)
	got := runCommand(context.Background(), CommandShell{}, handler, "", t.TempDir())
	if got.error != nil {
		t.Fatalf("unexpected error: %v", *got.error)
	}
	if got.exitCode == nil || *got.exitCode != 3 {
		t.Errorf("exitCode = %v, want 3", got.exitCode)
	}
}

func TestRunCommandStderrCaptured(t *testing.T) {
	unixOnly(t)
	handler := makeHandler(t, protocol.HookEventNamePreToolUse, nil, "echo oops 1>&2; exit 2", 0)
	got := runCommand(context.Background(), CommandShell{}, handler, "", t.TempDir())
	if got.exitCode == nil || *got.exitCode != 2 {
		t.Fatalf("exitCode = %v, want 2", got.exitCode)
	}
	if strings.TrimSpace(got.stderr) != "oops" {
		t.Errorf("stderr = %q, want %q", got.stderr, "oops")
	}
}

func TestRunCommandTimeout(t *testing.T) {
	unixOnly(t)
	handler := makeHandler(t, protocol.HookEventNamePreToolUse, nil, "sleep 5", 0)
	handler.TimeoutSec = 1
	start := time.Now()
	got := runCommand(context.Background(), CommandShell{}, handler, "", t.TempDir())
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Fatalf("command did not time out promptly: %v", elapsed)
	}
	if got.error == nil {
		t.Fatalf("expected timeout error, got exit %v", got.exitCode)
	}
	if !strings.Contains(*got.error, "hook timed out after 1s") {
		t.Errorf("error = %q, want timeout message", *got.error)
	}
}

func TestRunCommandSpawnFailure(t *testing.T) {
	// An explicit non-existent program with no shell wrapping should fail to
	// spawn, surfacing an error rather than an exit code.
	handler := makeHandler(t, protocol.HookEventNamePreToolUse, nil, "ignored", 0)
	shell := CommandShell{Program: "/nonexistent/definitely-not-a-real-binary-xyz"}
	got := runCommand(context.Background(), shell, handler, "", t.TempDir())
	if got.error == nil {
		t.Fatalf("expected spawn error, got exit %v stdout %q", got.exitCode, got.stdout)
	}
	if got.exitCode != nil {
		t.Errorf("exitCode = %v, want nil on spawn failure", got.exitCode)
	}
}

func TestRunCommandEnvPassedThrough(t *testing.T) {
	unixOnly(t)
	handler := makeHandler(t, protocol.HookEventNamePreToolUse, nil, `printf '%s' "$HOOK_TEST_VAR"`, 0)
	handler.Env = map[string]string{"HOOK_TEST_VAR": "from-env"}
	got := runCommand(context.Background(), CommandShell{}, handler, "", t.TempDir())
	if got.error != nil {
		t.Fatalf("unexpected error: %v", *got.error)
	}
	if got.stdout != "from-env" {
		t.Errorf("stdout = %q, want %q (env not passed through)", got.stdout, "from-env")
	}
}

func TestRunCommandExplicitShellArgs(t *testing.T) {
	unixOnly(t)
	handler := makeHandler(t, protocol.HookEventNamePreToolUse, nil, "printf hi", 0)
	shell := CommandShell{Program: "/bin/sh", Args: []string{"-c"}}
	got := runCommand(context.Background(), shell, handler, "", t.TempDir())
	if got.error != nil {
		t.Fatalf("unexpected error: %v", *got.error)
	}
	if got.stdout != "hi" {
		t.Errorf("stdout = %q, want %q", got.stdout, "hi")
	}
}

func TestExitCodePtr(t *testing.T) {
	if got := exitCodePtr(0); got == nil || *got != 0 {
		t.Errorf("exitCodePtr(0) = %v, want 0", got)
	}
	if got := exitCodePtr(7); got == nil || *got != 7 {
		t.Errorf("exitCodePtr(7) = %v, want 7", got)
	}
	// Signal termination reports -1, which maps to nil (no exit code).
	if got := exitCodePtr(-1); got != nil {
		t.Errorf("exitCodePtr(-1) = %v, want nil", got)
	}
}
