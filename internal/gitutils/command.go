package gitutils

import (
	"context"
	"os/exec"
	"runtime"
	"time"
)

// gitCommandTimeout bounds individual git invocations to prevent blocking on
// large repositories. Mirrors the Rust `GIT_COMMAND_TIMEOUT` (5 seconds).
const gitCommandTimeout = 5 * time.Second

// disabledHooksPath is the null device used to neutralise configured git hooks.
// Mirrors the Rust `DISABLED_HOOKS_PATH`.
func disabledHooksPath() string {
	if runtime.GOOS == "windows" {
		return "NUL"
	}
	return "/dev/null"
}

// nullDevice is the platform null device used as a `/dev/null` source path.
func nullDevice() string { return disabledHooksPath() }

// gitOutput holds the captured result of a git invocation.
type gitOutput struct {
	exitCode int
	success  bool
	stdout   []byte
	stderr   []byte
}

// runGitWithTimeout runs `git <args>` in cwd with the same environment and
// safety flags the Rust crate uses (GIT_OPTIONAL_LOCKS=0, disabled hooks path,
// fsmonitor off) and a 5-second timeout. It returns false when the command
// could not be started or timed out, mirroring the Rust helper that yields
// `None` in those cases.
//
// Mirrors the Rust `run_git_command_with_timeout`.
func runGitWithTimeout(ctx context.Context, cwd string, args ...string) (gitOutput, bool) {
	cctx, cancel := context.WithTimeout(ctx, gitCommandTimeout)
	defer cancel()

	full := make([]string, 0, len(args)+4)
	full = append(full,
		"-c", "core.hooksPath="+disabledHooksPath(),
		"-c", "core.fsmonitor=false",
	)
	full = append(full, args...)

	cmd := exec.CommandContext(cctx, "git", full...)
	cmd.Dir = cwd
	cmd.Env = append(cmd.Environ(), "GIT_OPTIONAL_LOCKS=0")

	stdout, runErr := cmd.Output()
	out := gitOutput{stdout: stdout}
	if ee, ok := runErr.(*exec.ExitError); ok {
		out.stderr = ee.Stderr
		out.exitCode = ee.ExitCode()
		out.success = false
		// A non-zero exit is still a completed command, not a failure to run.
		if cctx.Err() != nil {
			return gitOutput{}, false
		}
		return out, true
	}
	if runErr != nil {
		// Could not start the process or it was cancelled / timed out.
		return gitOutput{}, false
	}

	out.success = true
	out.exitCode = 0
	return out, true
}
