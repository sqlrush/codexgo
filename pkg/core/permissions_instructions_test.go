package core

import (
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// TestRenderPermissionsInstructionsReadOnlyNever pins the exact `<permissions
// instructions>` block codex emits for the default `codex exec` config
// (read-only sandbox, network restricted, approval never) — captured byte-for-byte
// from the real codex 0.136.0 binary's /responses request.
func TestRenderPermissionsInstructionsReadOnlyNever(t *testing.T) {
	const want = "<permissions instructions>\n" +
		"Filesystem sandboxing defines which files can be read or written. `sandbox_mode` is `read-only`: The sandbox only permits reading files. Network access is restricted.\n" +
		"Approval policy is currently never. Do not provide the `sandbox_permissions` for any reason, commands will be rejected.\n" +
		"</permissions instructions>"

	got := RenderPermissionsInstructions(SandboxModeReadOnly, false, protocol.AskForApprovalNever)
	if got != want {
		t.Errorf("permissions instructions mismatch\n want: %q\n got:  %q", want, got)
	}
}

// TestRenderPermissionsInstructionsStructure checks the marker wrapping and
// section ordering invariants across the sandbox modes: the body opens after the
// start marker on its own line, the sandbox line precedes the approval line, and
// the network word reflects the flag.
func TestRenderPermissionsInstructionsStructure(t *testing.T) {
	cases := []struct {
		name        string
		mode        PermissionsSandboxMode
		network     bool
		wantSandbox string
		wantNet     string
	}{
		{"read-only restricted", SandboxModeReadOnly, false, "`read-only`", "restricted"},
		{"workspace-write enabled", SandboxModeWorkspaceWrite, true, "`workspace-write`", "enabled"},
		{"danger enabled", SandboxModeDangerFullAccess, true, "`danger-full-access`", "enabled"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RenderPermissionsInstructions(tc.mode, tc.network, protocol.AskForApprovalNever)
			if !strings.HasPrefix(got, permissionsStartMarker+"\n") {
				t.Errorf("missing start marker + newline; got prefix %q", got[:min(60, len(got))])
			}
			if !strings.HasSuffix(got, "\n"+permissionsEndMarker) {
				t.Errorf("missing newline + end marker; got suffix %q", got[max(0, len(got)-60):])
			}
			if !strings.Contains(got, tc.wantSandbox) {
				t.Errorf("expected sandbox marker %q in %q", tc.wantSandbox, got)
			}
			if !strings.Contains(got, "Network access is "+tc.wantNet+".") {
				t.Errorf("expected network %q in %q", tc.wantNet, got)
			}
			sandboxIdx := strings.Index(got, "Filesystem sandboxing")
			approvalIdx := strings.Index(got, "Approval policy is currently never")
			if sandboxIdx < 0 || approvalIdx < 0 || sandboxIdx > approvalIdx {
				t.Errorf("sandbox section must precede approval section; got %q", got)
			}
		})
	}
}
