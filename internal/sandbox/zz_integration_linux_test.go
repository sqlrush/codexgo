//go:build linux && codexsandboxe2e

package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These end-to-end tests run the real Linux sandbox helper entrypoint by
// re-executing the test binary with the NativeSandboxArgv0 sentinel. They are
// gated behind the codexsandboxe2e build tag because they require a host that
// allows creating user namespaces (e.g. a privileged CI container); they are
// never part of the default test suite.
//
// Run with:
//
//	go test -tags codexsandboxe2e -run TestLinuxSandbox ./internal/sandbox/

// runSandboxedShell builds a spec for the given shell script and runs it through
// the helper, returning the combined output. When the host cannot create user
// namespaces the test is skipped rather than failed.
func runSandboxedShell(t *testing.T, spec NativeSandboxSpec) string {
	t.Helper()
	if os.Getenv("CODEX_SANDBOX_E2E_CHILD") == "1" {
		os.Exit(RunLinuxSandboxMain(os.Stderr))
	}

	encoded, err := EncodeNativeSandboxSpec(spec)
	if err != nil {
		t.Fatalf("encode spec: %v", err)
	}

	cmd := exec.Command(os.Args[0], NativeSandboxArgv0)
	cmd.Env = append(os.Environ(),
		"CODEX_SANDBOX_E2E_CHILD=1",
		NativeSandboxSpecEnv+"="+encoded,
	)
	out, _ := cmd.CombinedOutput()
	got := string(out)
	if strings.Contains(got, "needs permission to create user namespaces") {
		t.Skipf("host cannot create user namespaces: %s", got)
	}
	t.Logf("sandbox output:\n%s", got)
	return got
}

func TestLinuxSandboxEnforcesReadOnlyRoot(t *testing.T) {
	work := t.TempDir()
	resolved, err := filepath.EvalSymlinks(work)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}

	got := runSandboxedShell(t, NativeSandboxSpec{
		Command: []string{"/bin/sh", "-c",
			"touch /should_fail_root 2>/dev/null && echo WROTE_ROOT || echo ROOT_RO; " +
				"touch " + resolved + "/ok 2>/dev/null && echo WROTE_WORK || echo WORK_FAIL"},
		Cwd:                resolved,
		FullDiskReadAccess: true,
		WritableRoots:      []string{resolved},
		NetworkSeccompMode: NetworkSeccompModeRestricted,
	})

	if !strings.Contains(got, "ROOT_RO") {
		t.Errorf("expected root to be read-only, output:\n%s", got)
	}
	if !strings.Contains(got, "WROTE_WORK") {
		t.Errorf("expected workspace write to succeed, output:\n%s", got)
	}
}

func TestLinuxSandboxBlocksNetwork(t *testing.T) {
	work := t.TempDir()
	resolved, err := filepath.EvalSymlinks(work)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}

	// Attempt to open a raw TCP socket; the restricted seccomp filter denies
	// non-AF_UNIX socket() with EPERM, so the python/sh probe must fail.
	got := runSandboxedShell(t, NativeSandboxSpec{
		Command: []string{"/bin/sh", "-c",
			"if command -v python3 >/dev/null 2>&1; then " +
				"python3 -c 'import socket; socket.socket(socket.AF_INET, socket.SOCK_STREAM)' 2>/dev/null " +
				"&& echo NET_OPEN || echo NET_BLOCKED; " +
				"else echo NO_PYTHON; fi"},
		Cwd:                resolved,
		FullDiskReadAccess: true,
		WritableRoots:      []string{resolved},
		NetworkSeccompMode: NetworkSeccompModeRestricted,
	})

	if strings.Contains(got, "NO_PYTHON") {
		t.Skip("python3 not available to probe network restriction")
	}
	if !strings.Contains(got, "NET_BLOCKED") {
		t.Errorf("expected AF_INET socket creation to be blocked by seccomp, output:\n%s", got)
	}
}

func TestLinuxSandboxEnforcesProtectedSubpath(t *testing.T) {
	work := t.TempDir()
	resolved, err := filepath.EvalSymlinks(work)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	gitDir := filepath.Join(resolved, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}

	got := runSandboxedShell(t, NativeSandboxSpec{
		Command: []string{"/bin/sh", "-c",
			"touch " + filepath.Join(gitDir, "x") + " 2>/dev/null && echo WROTE_GIT || echo GIT_RO; " +
				"touch " + filepath.Join(resolved, "ok") + " 2>/dev/null && echo WROTE_WORK || echo WORK_FAIL"},
		Cwd:                resolved,
		FullDiskReadAccess: true,
		WritableRoots:      []string{resolved},
		ProtectedSubpaths:  []string{gitDir},
		NetworkSeccompMode: NetworkSeccompModeRestricted,
	})

	if !strings.Contains(got, "GIT_RO") {
		t.Errorf("expected .git to stay read-only under the writable root, output:\n%s", got)
	}
	if !strings.Contains(got, "WROTE_WORK") {
		t.Errorf("expected workspace write to succeed, output:\n%s", got)
	}
}
