package localexec

import (
	"testing"

	"github.com/sqlrush/codexgo/internal/sandbox"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// TestResolveTurnSandboxPolicyModes pins the per-turn mapping from sandbox mode +
// network access to (SandboxType, FileSystemSandboxPolicy, NetworkSandboxPolicy),
// mirroring codex's PermissionProfile::{read_only,workspace_write,Disabled} →
// to_runtime_permissions → SandboxManager::select_initial(Auto).
//
// On non-darwin hosts no platform sandbox is available, so read-only /
// workspace-write resolve to none too; the FS/network policies are asserted
// unconditionally and the SandboxType is compared against SelectBackendType so
// the assertion holds on every platform.
func TestResolveTurnSandboxPolicyModes(t *testing.T) {
	t.Parallel()
	const cwd = "/work"

	tests := []struct {
		name           string
		mode           protocol.SandboxMode
		networkEnabled bool
		wantFSKind     protocol.FileSystemSandboxKind
		wantNetwork    protocol.NetworkSandboxPolicy
		wantPlatform   bool // whether a platform sandbox is required (Auto)
	}{
		{
			name:         "read-only restricted no network",
			mode:         protocol.SandboxModeReadOnly,
			wantFSKind:   protocol.FileSystemSandboxKindRestricted,
			wantNetwork:  protocol.NetworkSandboxPolicyRestricted,
			wantPlatform: true,
		},
		{
			name:           "read-only network enabled",
			mode:           protocol.SandboxModeReadOnly,
			networkEnabled: true,
			wantFSKind:     protocol.FileSystemSandboxKindRestricted,
			wantNetwork:    protocol.NetworkSandboxPolicyEnabled,
			// read-only with network enabled stays restricted FS -> still
			// requires a platform sandbox (fsHasFullDiskWriteAccess is false).
			wantPlatform: true,
		},
		{
			name:         "workspace-write restricted no network",
			mode:         protocol.SandboxModeWorkspaceWrite,
			wantFSKind:   protocol.FileSystemSandboxKindRestricted,
			wantNetwork:  protocol.NetworkSandboxPolicyRestricted,
			wantPlatform: true,
		},
		{
			name:         "danger-full-access -> none, unrestricted, network enabled",
			mode:         protocol.SandboxModeDangerFullAccess,
			wantFSKind:   protocol.FileSystemSandboxKindUnrestricted,
			wantNetwork:  protocol.NetworkSandboxPolicyEnabled,
			wantPlatform: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tc := newTestTurn(cwd)
			tc.SandboxMode = tt.mode
			tc.NetworkAccessEnabled = tt.networkEnabled

			got := resolveTurnSandboxPolicy(tc)

			if got.FileSystemSandboxPolicy.Kind != tt.wantFSKind {
				t.Errorf("FS kind = %q, want %q", got.FileSystemSandboxPolicy.Kind, tt.wantFSKind)
			}
			if got.NetworkSandboxPolicy != tt.wantNetwork {
				t.Errorf("network = %q, want %q", got.NetworkSandboxPolicy, tt.wantNetwork)
			}

			// danger-full-access must NEVER spawn under a platform sandbox so the
			// existing danger-full-access differentials stay byte-identical.
			if !tt.wantPlatform {
				if got.SandboxType != sandbox.SandboxTypeNone {
					t.Errorf("SandboxType = %v, want none for %v", got.SandboxType, tt.mode)
				}
			}

			// SelectBackendType uses Auto; on darwin a required platform sandbox
			// is Seatbelt. Assert that the resolver's SandboxType matches what
			// SelectBackendType would pick for the resolved policies.
			wantType := sandbox.SelectBackendType(sandbox.SelectParams{
				FileSystemSandboxPolicy: got.FileSystemSandboxPolicy,
				NetworkSandboxPolicy:    got.NetworkSandboxPolicy,
				Preference:              sandbox.SandboxablePreferenceAuto,
				WindowsSandboxLevel:     tc.WindowsSandboxLevel,
			})
			if got.SandboxType != wantType {
				t.Errorf("SandboxType = %v, want %v (from SelectBackendType)", got.SandboxType, wantType)
			}
		})
	}
}

// TestResolveTurnSandboxPolicyWorkspaceWriteEntries asserts the workspace-write
// FS policy carries the default codex entries (root read + project_roots write +
// slash_tmp + tmpdir writes), mirroring FileSystemSandboxPolicy::workspace_write.
func TestResolveTurnSandboxPolicyWorkspaceWriteEntries(t *testing.T) {
	t.Parallel()
	tc := newTestTurn("/work")
	tc.SandboxMode = protocol.SandboxModeWorkspaceWrite

	got := resolveTurnSandboxPolicy(tc)
	entries := got.FileSystemSandboxPolicy.Entries

	wantSpecials := map[protocol.FileSystemSpecialPathKind]protocol.FileSystemAccessMode{
		protocol.FileSystemSpecialPathKindRoot:         protocol.FileSystemAccessModeRead,
		protocol.FileSystemSpecialPathKindProjectRoots: protocol.FileSystemAccessModeWrite,
		protocol.FileSystemSpecialPathKindSlashTmp:     protocol.FileSystemAccessModeWrite,
		protocol.FileSystemSpecialPathKindTmpdir:       protocol.FileSystemAccessModeWrite,
	}
	seen := map[protocol.FileSystemSpecialPathKind]protocol.FileSystemAccessMode{}
	for _, e := range entries {
		if e.Path.Type == protocol.FileSystemPathTypeSpecial && e.Path.Special != nil {
			seen[e.Path.Special.Kind] = e.Access
		}
	}
	for kind, access := range wantSpecials {
		if seen[kind] != access {
			t.Errorf("workspace-write entry %q = %q, want %q", kind, seen[kind], access)
		}
	}
}

// TestBuildUnifiedExecRequestThreadsPolicy asserts the exec_command request is
// built with the turn's resolved sandbox policies (the seam that fixes the
// none-STUB). A read-only turn must produce the resolved backend and the
// restricted FS / restricted network policy on every host.
func TestBuildUnifiedExecRequestThreadsPolicy(t *testing.T) {
	t.Parallel()
	tc := newTestTurn("/work")
	tc.SandboxMode = protocol.SandboxModeReadOnly

	pol := resolveTurnSandboxPolicy(tc)
	req := buildUnifiedExecRequest(unifiedExecRequestParams{
		Turn:        tc,
		Argv:        []string{"/bin/echo", "hi"},
		HookCommand: "echo hi",
		CallID:      "call-1",
		ProcessID:   7,
		YieldTimeMS: 1000,
		Cwd:         "/work",
	})

	if req.SandboxType != pol.SandboxType {
		t.Errorf("req.SandboxType = %v, want %v", req.SandboxType, pol.SandboxType)
	}
	if req.FileSystemSandboxPolicy.Kind != protocol.FileSystemSandboxKindRestricted {
		t.Errorf("req FS kind = %q, want restricted", req.FileSystemSandboxPolicy.Kind)
	}
	if req.NetworkSandboxPolicy != protocol.NetworkSandboxPolicyRestricted {
		t.Errorf("req network = %q, want restricted", req.NetworkSandboxPolicy)
	}
	if req.SandboxCwd != "/work" {
		t.Errorf("req SandboxCwd = %q, want /work", req.SandboxCwd)
	}
}

// TestBuildUnifiedExecRequestDangerFullAccessNone asserts the danger-full-access
// turn keeps the none backend (so the parity differentials stay byte-identical).
func TestBuildUnifiedExecRequestDangerFullAccessNone(t *testing.T) {
	t.Parallel()
	tc := newTestTurn("/work")
	tc.SandboxMode = protocol.SandboxModeDangerFullAccess

	req := buildUnifiedExecRequest(unifiedExecRequestParams{
		Turn:        tc,
		Argv:        []string{"/bin/echo", "hi"},
		HookCommand: "echo hi",
		CallID:      "call-1",
		ProcessID:   7,
		YieldTimeMS: 1000,
		Cwd:         "/work",
	})
	if req.SandboxType != sandbox.SandboxTypeNone {
		t.Errorf("danger-full-access req.SandboxType = %v, want none", req.SandboxType)
	}
}
