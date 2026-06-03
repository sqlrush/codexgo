//go:build !unix

package cli

import "os/exec"

// signalExitCode has no signal semantics on non-unix platforms (Windows uses the
// raw exit code), so it always reports that no signal-based code applies. This
// mirrors the windows branch of exit_status.rs handle_exit_status.
func signalExitCode(_ *exec.ExitError) (int, bool) {
	return 0, false
}
