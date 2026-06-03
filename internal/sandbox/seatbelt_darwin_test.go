//go:build darwin

package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// TestDarwinPlatformDefaultIsSeatbelt verifies that on macOS the platform
// default sandbox is Seatbelt regardless of the windows-sandbox toggle.
func TestDarwinPlatformDefaultIsSeatbelt(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		st, ok := GetPlatformSandbox(enabled)
		if !ok || st != SandboxTypeMacosSeatbelt {
			t.Fatalf("GetPlatformSandbox(%v) = (%v,%v), want (Seatbelt,true)", enabled, st, ok)
		}
	}
}

// TestSeatbeltExecEnforcesReadOnlyPolicy is a behavioral exec test: it builds a
// minimal Seatbelt profile (deny-by-default, with the working directory escaped
// via the package's regexEscape helper made writable) and runs it under
// /usr/bin/sandbox-exec to confirm the policy is actually enforced by the OS.
//
// It validates the contract the package's policy model targets: a write inside
// an allowed subpath succeeds while a write outside it is blocked. This is
// darwin-only because it shells out to the real macOS Seatbelt backend.
func TestSeatbeltExecEnforcesReadOnlyPolicy(t *testing.T) {
	sandboxExec := "/usr/bin/sandbox-exec"
	if _, err := os.Stat(sandboxExec); err != nil {
		t.Skipf("sandbox-exec unavailable: %v", err)
	}

	workspace := t.TempDir()
	// Resolve symlinks so the .sbpl subpath rule matches the path the child sees
	// (macOS resolves /var -> /private/var for temp dirs).
	resolved, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", workspace, err)
	}

	allowedEscaped := regexEscape(resolved)
	profile := fmt.Sprintf(`(version 1)
(deny default)
(allow process-exec)
(allow process-fork)
(allow file-read*)
(allow file-write*
  (regex #"^%s(/|$)"))
`, allowedEscaped)

	insidePath := filepath.Join(resolved, "inside.txt")
	outside := t.TempDir()
	outsideResolved, err := filepath.EvalSymlinks(outside)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", outside, err)
	}
	outsidePath := filepath.Join(outsideResolved, "outside.txt")

	cases := []struct {
		name      string
		target    string
		wantWrite bool
	}{
		{"write-inside-allowed-root-succeeds", insidePath, true},
		{"write-outside-allowed-root-blocked", outsidePath, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Use a fresh shell that attempts to create the target file.
			script := fmt.Sprintf(": > %q", tc.target)
			cmd := exec.Command(sandboxExec, "-p", profile, "/bin/sh", "-c", script)
			runErr := cmd.Run()
			_, statErr := os.Stat(tc.target)
			wrote := runErr == nil && statErr == nil

			if wrote != tc.wantWrite {
				t.Fatalf("write to %q: got wrote=%v (runErr=%v), want %v",
					tc.target, wrote, runErr, tc.wantWrite)
			}
		})
	}
}

// TestSelectInitialDarwinSeatbelt confirms the Auto preference selects Seatbelt
// for a restricted policy that needs a platform sandbox on macOS.
func TestSelectInitialDarwinSeatbelt(t *testing.T) {
	got := selectInitial(
		restricted(rootEntry(protocol.FileSystemAccessModeRead)),
		protocol.NetworkSandboxPolicyRestricted,
		SandboxablePreferenceAuto,
		protocol.WindowsSandboxLevelDisabled,
		false,
	)
	if got != SandboxTypeMacosSeatbelt {
		t.Fatalf("selectInitial on darwin = %v, want Seatbelt", got)
	}
}
