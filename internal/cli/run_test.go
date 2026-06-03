package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// runWith invokes Run with the given argv (excluding/including program name as
// the caller provides) and captures stdout/stderr.
func runWith(t *testing.T, argv []string, stdin string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = Run(context.Background(), argv, Streams{
		Stdin:           strings.NewReader(stdin),
		Stdout:          &out,
		Stderr:          &errOut,
		StdinIsTerminal: false,
	})
	return code, out.String(), errOut.String()
}

func TestRunVersion(t *testing.T) {
	code, out, _ := runWith(t, []string{"codex", "--version"}, "")
	if code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if !strings.Contains(out, "codex "+Version) {
		t.Errorf("stdout = %q", out)
	}
}

func TestRunHelp(t *testing.T) {
	code, out, _ := runWith(t, []string{"codex", "--help"}, "")
	if code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if !strings.Contains(out, "Usage: codex") || !strings.Contains(out, "Commands:") {
		t.Errorf("stdout = %q", out)
	}
}

func TestRunNoSubcommandExitsNonZero(t *testing.T) {
	// runWith provides a non-terminal stdin/stderr, so the default command takes
	// the TTY guard path: it must not launch the TUI and must exit non-zero.
	code, _, errOut := runWith(t, []string{"codex"}, "")
	if code == 0 {
		t.Errorf("code = %d, want non-zero", code)
	}
	if !strings.Contains(errOut, "requires a terminal") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestRunRemoteModeRejectedForSubcommand(t *testing.T) {
	code, _, errOut := runWith(t, []string{"codex", "--remote", "ws://h:1", "doctor"}, "")
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "only supported for interactive TUI") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestRunUnknownFeatureToggleErrors(t *testing.T) {
	code, _, errOut := runWith(t, []string{"codex", "--enable", "definitely_not_a_feature", "doctor"}, "")
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if !strings.Contains(errOut, "Unknown feature flag") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestRunArg0ApplyPatchDispatch(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	patch := "*** Begin Patch\n*** Add File: a.txt\n+hi\n*** End Patch"
	code, out, _ := runWith(t, []string{"apply_patch", patch}, "")
	if code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if !strings.Contains(out, "a.txt") {
		t.Errorf("stdout = %q", out)
	}
}

func TestRunCompletion(t *testing.T) {
	code, out, _ := runWith(t, []string{"codex", "completion", "zsh"}, "")
	if code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if !strings.Contains(out, "#compdef codex") {
		t.Errorf("stdout = %q", out)
	}
}
