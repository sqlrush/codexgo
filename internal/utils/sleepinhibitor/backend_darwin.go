//go:build darwin

package sleepinhibitor

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// macBackend prevents idle system sleep on macOS by supervising a `caffeinate`
// helper process while a turn is active.
//
// The Rust crate uses native IOKit power assertions
// (kIOPMAssertionTypePreventUserIdleSystemSleep). Reproducing that here would
// require cgo and linking the IOKit framework, which is outside this package's
// standard-library-only constraint. `caffeinate -i` requests the same
// "prevent idle system sleep" behavior using Apple's own supported tool, so the
// externally observable effect (the machine stays awake during a turn) is
// preserved as a best-effort fallback.
type macBackend struct {
	// cmd is the running caffeinate helper, or nil when inactive.
	cmd *exec.Cmd
}

// newBackend returns the macOS sleep-prevention backend.
func newBackend() backend {
	return &macBackend{}
}

// acquire starts a caffeinate helper if one is not already running or has died.
// It is idempotent while the helper is alive.
func (b *macBackend) acquire() {
	if b.cmd != nil {
		if processAlive(b.cmd) {
			return
		}
		// The previous helper exited unexpectedly; reap and restart it.
		_ = b.cmd.Wait()
		b.cmd = nil
	}

	// -i prevents idle system sleep, mirroring PreventUserIdleSystemSleep.
	cmd := exec.Command("caffeinate", "-i")
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		if !errors.Is(err, exec.ErrNotFound) && !os.IsNotExist(err) {
			warn("Failed to create macOS sleep-prevention assertion: %v", err)
		}
		return
	}
	b.cmd = cmd
}

// release terminates and reaps the caffeinate helper. It is idempotent.
func (b *macBackend) release() {
	if b.cmd == nil {
		return
	}
	cmd := b.cmd
	b.cmd = nil

	if cmd.Process != nil {
		if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			warn("Failed to release macOS sleep-prevention assertion: %v", err)
		}
	}
	if err := cmd.Wait(); err != nil && !isExpectedWaitError(err) {
		warn("Failed to reap macOS sleep-prevention assertion: %v", err)
	}
}

// processAlive reports whether the helper started by cmd is still running.
func processAlive(cmd *exec.Cmd) bool {
	if cmd == nil || cmd.Process == nil {
		return false
	}
	// Signal 0 performs existence checking without affecting the process.
	return cmd.Process.Signal(syscall.Signal(0)) == nil
}

// isExpectedWaitError reports whether a Wait error is the benign
// already-finished or killed case.
func isExpectedWaitError(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, os.ErrProcessDone) {
		return true
	}
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr)
}
