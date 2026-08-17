package hooks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// commandRunResult captures the outcome of running a single hook subprocess.
// Mirrors CommandRunResult.
type commandRunResult struct {
	startedAt   int64
	completedAt int64
	durationMs  int64
	exitCode    *int
	stdout      string
	stderr      string
	error       *string
}

// runCommand executes the handler's command via the configured shell, writing
// inputJSON to stdin and capturing stdout/stderr. The handler timeout is
// enforced; the parent ctx can cancel earlier. Mirrors run_command.
func runCommand(ctx context.Context, shell CommandShell, handler ConfiguredHandler, inputJSON string, cwd string) commandRunResult {
	startedAt := time.Now().Unix()
	started := time.Now()

	timeout := time.Duration(handler.TimeoutSec) * time.Second
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := buildCommand(runCtx, shell, handler)
	cmd.Dir = cwd
	cmd.Stdin = bytes.NewReader([]byte(inputJSON))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	completedAt := time.Now().Unix()
	durationMs := saturatingMillis(started)

	if err != nil {
		// Timeout (context deadline) reports a dedicated message that mirrors
		// the Rust timeout branch.
		if runCtx.Err() == context.DeadlineExceeded {
			msg := fmt.Sprintf("hook timed out after %ds", handler.TimeoutSec)
			return commandRunResult{
				startedAt:   startedAt,
				completedAt: completedAt,
				durationMs:  durationMs,
				error:       &msg,
			}
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code := exitErr.ExitCode()
			return commandRunResult{
				startedAt:   startedAt,
				completedAt: completedAt,
				durationMs:  durationMs,
				exitCode:    exitCodePtr(code),
				stdout:      stdout.String(),
				stderr:      stderr.String(),
			}
		}
		msg := err.Error()
		return commandRunResult{
			startedAt:   startedAt,
			completedAt: completedAt,
			durationMs:  durationMs,
			error:       &msg,
		}
	}

	code := 0
	return commandRunResult{
		startedAt:   startedAt,
		completedAt: completedAt,
		durationMs:  durationMs,
		exitCode:    &code,
		stdout:      stdout.String(),
		stderr:      stderr.String(),
	}
}

func exitCodePtr(code int) *int {
	// exec.ExitError.ExitCode returns -1 when the process was terminated by a
	// signal; in that case there is no exit code, matching Rust's None.
	if code < 0 {
		return nil
	}
	c := code
	return &c
}

func buildCommand(ctx context.Context, shell CommandShell, handler ConfiguredHandler) *exec.Cmd {
	var cmd *exec.Cmd
	if shell.Program == "" {
		program, leadingArgs := defaultShell()
		args := append(append([]string{}, leadingArgs...), handler.Command)
		cmd = exec.CommandContext(ctx, program, args...)
	} else {
		args := append(append([]string{}, shell.Args...), handler.Command)
		cmd = exec.CommandContext(ctx, shell.Program, args...)
	}
	cmd.Env = append(os.Environ(), handlerEnv(handler)...)
	return cmd
}

func handlerEnv(handler ConfiguredHandler) []string {
	out := make([]string, 0, len(handler.Env))
	for _, k := range handler.sortedEnvKeys() {
		out = append(out, k+"="+handler.Env[k])
	}
	return out
}

func defaultShell() (string, []string) {
	if runtime.GOOS == "windows" {
		comspec := os.Getenv("COMSPEC")
		if comspec == "" {
			comspec = "cmd.exe"
		}
		return comspec, []string{"/C"}
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	return shell, []string{"-lc"}
}

func saturatingMillis(started time.Time) int64 {
	elapsed := time.Since(started).Milliseconds()
	if elapsed < 0 {
		return 0
	}
	return elapsed
}
