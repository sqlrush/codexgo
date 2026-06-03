//go:build linux

package sleepinhibitor

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

const (
	// assertionReason is the human-readable reason reported to the inhibitor
	// backend, matching the Rust crate verbatim.
	assertionReason = "Codex is running an active turn"
	// appID identifies this application to systemd-inhibit's --who flag.
	appID = "codex"
	// blockerSleepSeconds keeps the blocker helper alive "long enough" without
	// restarts. This is the string form of math.MaxInt32, which common `sleep`
	// implementations accept. Kept as a string to preserve the exact argument
	// passed by the Rust crate.
	blockerSleepSeconds = "2147483647"
)

// linuxBackendKind enumerates the supported Linux inhibitor helpers, in the
// order they are preferred by default.
type linuxBackendKind int

const (
	backendSystemdInhibit linuxBackendKind = iota
	backendGnomeSessionInhibit
)

func (k linuxBackendKind) String() string {
	switch k {
	case backendSystemdInhibit:
		return "SystemdInhibit"
	case backendGnomeSessionInhibit:
		return "GnomeSessionInhibit"
	default:
		return "Unknown"
	}
}

// linuxBackend supervises a long-lived helper process (systemd-inhibit or
// gnome-session-inhibit) that blocks idle sleep while a turn is active. It
// mirrors the Rust LinuxSleepInhibitor, including backend preference,
// fallback ordering, dead-helper restart, and one-shot "no backend available"
// logging.
type linuxBackend struct {
	// activeKind and activeCmd describe the currently running helper, or are
	// the zero value when inactive (activeCmd == nil).
	activeKind linuxBackendKind
	activeCmd  *exec.Cmd

	// preferredBackend remembers the helper that last started successfully so
	// it is tried first on subsequent acquisitions. nil means "no preference".
	preferredBackend *linuxBackendKind

	// missingBackendLogged suppresses repeated warnings when no backend is
	// available, matching the Rust crate's behavior.
	missingBackendLogged bool
}

// newBackend returns the Linux sleep-prevention backend.
func newBackend() backend {
	return &linuxBackend{}
}

// acquire ensures a helper process is running. If one is already alive it does
// nothing; if it has exited unexpectedly it is replaced. Backends are tried in
// preference order and the first that survives an initial liveness probe wins.
func (b *linuxBackend) acquire() {
	if b.activeCmd != nil {
		if b.helperAlive() {
			return
		}
		warn("Linux sleep inhibitor backend %s exited unexpectedly; attempting fallback", b.activeKind)
	}

	b.clearActive()

	shouldLog := !b.missingBackendLogged
	for _, kind := range b.backendOrder() {
		cmd, err := spawnBackend(kind)
		if err != nil {
			if shouldLog && !errors.Is(err, exec.ErrNotFound) && !os.IsNotExist(err) {
				warn("Failed to start Linux sleep inhibitor backend %s: %v", kind, err)
			}
			continue
		}

		if processAlive(cmd) {
			b.activeKind = kind
			b.activeCmd = cmd
			pref := kind
			b.preferredBackend = &pref
			b.missingBackendLogged = false
			return
		}

		// The helper exited immediately; reap it and try the next backend.
		if shouldLog {
			warn("Linux sleep inhibitor backend %s exited immediately", kind)
		}
		_ = cmd.Wait()
	}

	if shouldLog {
		warn("No Linux sleep inhibitor backend is available")
		b.missingBackendLogged = true
	}
}

// release terminates and reaps any active helper process. It is idempotent.
func (b *linuxBackend) release() {
	if b.activeCmd == nil {
		return
	}
	cmd := b.activeCmd
	kind := b.activeKind
	b.clearActive()

	if cmd.Process != nil {
		if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			warn("Failed to stop Linux sleep inhibitor backend %s: %v", kind, err)
		}
	}
	if err := cmd.Wait(); err != nil && !isExpectedWaitError(err) {
		warn("Failed to reap Linux sleep inhibitor backend %s: %v", kind, err)
	}
}

// backendOrder returns the helpers to try, with the remembered preference (if
// any) first. The returned slice is freshly allocated so callers never mutate
// shared state.
func (b *linuxBackend) backendOrder() []linuxBackendKind {
	if b.preferredBackend != nil && *b.preferredBackend == backendGnomeSessionInhibit {
		return []linuxBackendKind{backendGnomeSessionInhibit, backendSystemdInhibit}
	}
	return []linuxBackendKind{backendSystemdInhibit, backendGnomeSessionInhibit}
}

// helperAlive reports whether the currently tracked helper is still running.
func (b *linuxBackend) helperAlive() bool {
	return b.activeCmd != nil && processAlive(b.activeCmd)
}

// clearActive resets the active-helper bookkeeping to the inactive state.
func (b *linuxBackend) clearActive() {
	b.activeKind = backendSystemdInhibit
	b.activeCmd = nil
}

// spawnBackend launches the helper for the given kind with stdio attached to
// /dev/null and a parent-death signal armed so the helper is terminated if this
// process dies. The exact argument vectors match the Rust crate.
func spawnBackend(kind linuxBackendKind) (*exec.Cmd, error) {
	var cmd *exec.Cmd
	switch kind {
	case backendSystemdInhibit:
		cmd = exec.Command(
			"systemd-inhibit",
			"--what=idle",
			"--mode=block",
			"--who", appID,
			"--why", assertionReason,
			"--",
			"sleep", blockerSleepSeconds,
		)
	case backendGnomeSessionInhibit:
		cmd = exec.Command(
			"gnome-session-inhibit",
			"--inhibit", "idle",
			"--reason", assertionReason,
			"sleep", blockerSleepSeconds,
		)
	default:
		return nil, errors.New("unknown linux backend")
	}

	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	// Pdeathsig delivers SIGTERM to the helper when this process exits, which
	// is the standard-library equivalent of the Rust crate's pre_exec PDEATHSIG
	// setup (the fork/exec race is handled inside the kernel for Pdeathsig).
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGTERM,
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

// processAlive reports whether the helper started by cmd is still running. It
// uses a zero signal probe, which checks for process existence without
// affecting it.
func processAlive(cmd *exec.Cmd) bool {
	if cmd == nil || cmd.Process == nil {
		return false
	}
	// Signal 0 performs error checking without sending a signal.
	err := cmd.Process.Signal(syscall.Signal(0))
	return err == nil
}

// isExpectedWaitError reports whether a Wait error is the benign "already
// reaped / already finished" case, mirroring the Rust crate's tolerance of
// ErrorKind::InvalidInput.
func isExpectedWaitError(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, os.ErrProcessDone) {
		return true
	}
	// A helper killed via SIGTERM/SIGKILL reports a non-zero exit status; that
	// is expected and must not be logged as a failure.
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr)
}
