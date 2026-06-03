//go:build unix

package cli

import (
	"os/exec"
	"syscall"
)

// signalExitCode extracts a signal-based exit code (128+signal) from a child
// process that was terminated by a signal, mirroring the unix branch of
// exit_status.rs handle_exit_status (status.signal() => 128 + signal).
func signalExitCode(exitErr *exec.ExitError) (int, bool) {
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok {
		return 0, false
	}
	if status.Signaled() {
		return 128 + int(status.Signal()), true
	}
	return 0, false
}
