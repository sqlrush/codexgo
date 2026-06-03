package cli

import (
	"errors"
	"os/exec"
	"testing"
)

func TestExitCodeFromError(t *testing.T) {
	if got := ExitCodeFromError(nil); got != 0 {
		t.Errorf("nil error -> %d, want 0", got)
	}
	if got := ExitCodeFromError(errors.New("plain")); got != 1 {
		t.Errorf("plain error -> %d, want 1", got)
	}
}

func TestExitCodeFromErrorPropagatesChildCode(t *testing.T) {
	// `false` exits 3 on no platform reliably; use `sh -c exit 7` for portability.
	cmd := exec.Command("sh", "-c", "exit 7")
	err := cmd.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Skipf("could not produce an ExitError: %v", err)
	}
	if got := ExitCodeFromError(err); got != 7 {
		t.Errorf("child exit -> %d, want 7", got)
	}
}

func TestExitCodeFromErrorSignal(t *testing.T) {
	// `sh -c 'kill -TERM $$'` terminates via SIGTERM (15) => 128+15 = 143 on unix.
	cmd := exec.Command("sh", "-c", "kill -TERM $$")
	err := cmd.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Skipf("could not produce an ExitError: %v", err)
	}
	got := ExitCodeFromError(err)
	// On unix this is 143; on non-unix platforms the signal branch is a no-op and
	// the raw code (or 1) is returned. Only assert the unix behavior when the
	// child reported no normal exit code.
	if code := exitErr.ExitCode(); code >= 0 {
		if got != code {
			t.Errorf("exit -> %d, want %d", got, code)
		}
		return
	}
	if got != 143 {
		t.Errorf("signal exit -> %d, want 143 (128+SIGTERM)", got)
	}
}
