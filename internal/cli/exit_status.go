package cli

import "os/exec"

// ExitCodeFromError maps an error returned by a child process (run via os/exec)
// to the process exit code the codex CLI should propagate.
//
// It mirrors exit_status.rs handle_exit_status: a normal exit propagates the
// child's code; a signal-terminated child maps to 128+signal on unix; anything
// else falls back to 1. Platform-specific signal extraction lives in
// exit_status_unix.go / exit_status_other.go.
func ExitCodeFromError(err error) int {
	if err == nil {
		return 0
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		return 1
	}
	if code := exitErr.ExitCode(); code >= 0 {
		return code
	}
	if signal, ok := signalExitCode(exitErr); ok {
		return signal
	}
	return 1
}
