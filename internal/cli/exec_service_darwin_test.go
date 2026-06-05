//go:build darwin

package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sqlrush/codexgo/internal/core"
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/sandbox"
)

// TestLocalExecServiceSeatbeltEnforcesReadOnly is a behavioral exec test on the
// real macOS Seatbelt backend: a write attempt outside the workspace under a
// read-only policy is blocked, while the same write under the none backend
// (danger-full-access) succeeds. It mirrors the darwin guard + sandbox-exec
// availability skip used by internal/sandbox/seatbelt_darwin_test.go.
func TestLocalExecServiceSeatbeltEnforcesReadOnly(t *testing.T) {
	if _, err := os.Stat(sandbox.MacosPathToSeatbeltExecutable); err != nil {
		t.Skipf("sandbox-exec unavailable: %v", err)
	}

	workspace := t.TempDir()
	resolved, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", workspace, err)
	}

	// Target a write OUTSIDE the workspace so the read-only policy (which grants
	// no write roots) must block it.
	outside := t.TempDir()
	outsideResolved, err := filepath.EvalSymlinks(outside)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", outside, err)
	}

	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("sh unavailable: %v", err)
	}

	svc := newLocalExecService()

	tests := []struct {
		name      string
		fsPolicy  protocol.FileSystemSandboxPolicy
		network   protocol.NetworkSandboxPolicy
		sandbox   sandbox.SandboxType
		wantWrite bool
	}{
		{
			name: "read-only-blocks-outside-write",
			fsPolicy: protocol.FileSystemSandboxPolicy{
				Kind: protocol.FileSystemSandboxKindRestricted,
				Entries: []protocol.FileSystemSandboxEntry{{
					Path:   protocol.NewFileSystemSpecialPath(protocol.FileSystemSpecialPath{Kind: protocol.FileSystemSpecialPathKindRoot}),
					Access: protocol.FileSystemAccessModeRead,
				}},
			},
			network:   protocol.NetworkSandboxPolicyRestricted,
			sandbox:   sandbox.SandboxTypeMacosSeatbelt,
			wantWrite: false,
		},
		{
			name:      "danger-full-access-allows-write",
			fsPolicy:  protocol.FileSystemSandboxPolicy{Kind: protocol.FileSystemSandboxKindUnrestricted},
			network:   protocol.NetworkSandboxPolicyEnabled,
			sandbox:   sandbox.SandboxTypeNone,
			wantWrite: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			target := filepath.Join(outsideResolved, tt.name+".txt")
			_ = os.Remove(target)

			res, runErr := svc.Run(context.Background(), core.ExecRequest{
				Command:                 []string{shell, "-c", ": > " + target},
				Cwd:                     resolved,
				SandboxType:             tt.sandbox,
				FileSystemSandboxPolicy: tt.fsPolicy,
				NetworkSandboxPolicy:    tt.network,
				SandboxPolicyCwd:        resolved,
			})
			if runErr != nil {
				t.Fatalf("Run returned error: %v", runErr)
			}

			_, statErr := os.Stat(target)
			wrote := res.ExitCode == 0 && statErr == nil
			if wrote != tt.wantWrite {
				t.Fatalf("write to %q: got wrote=%v (exit=%d, stderr=%q), want %v",
					target, wrote, res.ExitCode, res.Stderr, tt.wantWrite)
			}
		})
	}
}
