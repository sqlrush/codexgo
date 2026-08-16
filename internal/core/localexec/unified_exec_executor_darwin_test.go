//go:build darwin

package localexec

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/internal/core"
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/sandbox"
	"github.com/sqlrush/codexgo/internal/tools"
	"github.com/sqlrush/codexgo/internal/unifiedexec"
)

// TestUnifiedExecSeatbeltEnforcesReadOnly is a behavioral exec test for the
// exec_command (UnifiedExec) bridge on the real macOS Seatbelt backend: a write
// outside the workspace fails under a read-only turn but succeeds under a
// danger-full-access turn (none backend). It follows the darwin + PTY +
// sandbox-exec guards of the existing seatbelt/unified-exec tests.
func TestUnifiedExecSeatbeltEnforcesReadOnly(t *testing.T) {
	requirePTY(t)
	if _, err := os.Stat(sandbox.MacosPathToSeatbeltExecutable); err != nil {
		t.Skipf("sandbox-exec unavailable: %v", err)
	}

	workspace := t.TempDir()
	resolved, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", workspace, err)
	}
	outside := t.TempDir()
	outsideResolved, err := filepath.EvalSymlinks(outside)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", outside, err)
	}

	tests := []struct {
		name      string
		mode      protocol.SandboxMode
		wantWrite bool
	}{
		{"read-only-blocks-outside-write", protocol.SandboxModeReadOnly, false},
		{"danger-full-access-allows-write", protocol.SandboxModeDangerFullAccess, true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			executor := unifiedexec.NewExecutor(nil)
			t.Cleanup(executor.Shutdown)
			execEx := newUnifiedExecCommandExecutor(executor, nil)

			tc := turnWithShellFeatures(resolved, true)
			tc.SandboxMode = tt.mode

			target := filepath.Join(outsideResolved, tt.name+".txt")
			_ = os.Remove(target)

			_, herr := execEx.Handle(context.Background(), &core.ToolHandlerContext{
				Turn: tc, CallID: "c1", ToolName: execEx.Name(),
				Payload: tools.FunctionPayload(
					`{"cmd":": > ` + escapeForJSON(target) + `","yield_time_ms":3000}`),
			})
			if herr != nil {
				t.Fatalf("exec_command: %v", herr)
			}

			_, statErr := os.Stat(target)
			wrote := statErr == nil
			if wrote != tt.wantWrite {
				t.Fatalf("write to %q under %s: got wrote=%v, want %v", target, tt.mode, wrote, tt.wantWrite)
			}
		})
	}
}

// escapeForJSON escapes a path for embedding inside a JSON string literal in the
// test payload (temp paths contain no quotes, but backslashes are escaped to be
// safe across platforms).
func escapeForJSON(s string) string {
	return strings.ReplaceAll(s, `\`, `\\`)
}
