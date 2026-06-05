package core

import (
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/sandbox"
)

// This file ports the per-turn sandbox-policy resolution that codex performs in
// the ToolOrchestrator before spawning a command. In codex the turn carries a
// PermissionProfile; the orchestrator reads turn_context.file_system_sandbox_policy()
// / network_sandbox_policy() (PermissionProfile::{file_system,network}_sandbox_policy)
// and SandboxManager::select_initial(Auto) picks the platform backend. Core's
// reduced TurnContext carries the projected SandboxMode + NetworkAccessEnabled
// instead of the full opaque PermissionProfile, so this resolver reconstructs the
// same (SandboxType, FileSystemSandboxPolicy, NetworkSandboxPolicy) triple from
// those fields.
//
// Mapping (mirrors PermissionProfile::{read_only,workspace_write,Disabled} ->
// to_runtime_permissions -> select_initial):
//
//	read-only          -> Managed restricted (root read); network restricted/enabled.
//	workspace-write     -> Managed workspace_write entries; network restricted/enabled.
//	danger-full-access  -> Disabled: unrestricted FS, network ALWAYS enabled.
//
// SandboxManager::select_initial(Auto) then resolves the backend: restricted FS
// without full-disk write requires a platform sandbox (Seatbelt on macOS), while
// the unrestricted danger-full-access policy never does -> none. The
// danger-full-access -> none arm is load-bearing for parity: the tool-call
// differentials run under danger-full-access and must keep spawning unsandboxed.

// turnSandboxPolicy is the resolved spawn policy for a turn: the selected
// platform backend plus the filesystem and network policies the backend
// enforces. Mirrors the (SandboxType, FileSystemSandboxPolicy,
// NetworkSandboxPolicy) the Rust SandboxAttempt carries.
type turnSandboxPolicy struct {
	// SandboxType is the platform backend the command spawns under.
	SandboxType sandbox.SandboxType
	// FileSystemSandboxPolicy is the resolved filesystem policy.
	FileSystemSandboxPolicy protocol.FileSystemSandboxPolicy
	// NetworkSandboxPolicy is the resolved network policy.
	NetworkSandboxPolicy protocol.NetworkSandboxPolicy
}

// resolveTurnSandboxPolicy resolves the real per-turn spawn policy from the
// turn's sandbox mode + network access, mirroring codex's
// turn_context.{file_system,network}_sandbox_policy() + select_initial(Auto).
//
// STUB: codex's orchestrator additionally folds per-call sandbox_permissions /
// additional_permissions (escalation, with_additional_permissions) and the
// approval flow into the attempt. Those are owned by the approvals area and are
// not threaded here; this resolver applies the ambient turn policy unchanged,
// which is the first-attempt default for every call.
func resolveTurnSandboxPolicy(tc *TurnContext) turnSandboxPolicy {
	mode := protocol.SandboxModeReadOnly
	networkEnabled := false
	windowsLevel := protocol.WindowsSandboxLevelDisabled
	if tc != nil {
		if tc.SandboxMode != "" {
			mode = tc.SandboxMode
		}
		networkEnabled = tc.NetworkAccessEnabled
		windowsLevel = tc.WindowsSandboxLevel
	}

	fsPolicy, networkPolicy := runtimePermissionsForMode(mode, networkEnabled)

	sandboxType := sandbox.SelectBackendType(sandbox.SelectParams{
		FileSystemSandboxPolicy: fsPolicy,
		NetworkSandboxPolicy:    networkPolicy,
		Preference:              sandbox.SandboxablePreferenceAuto,
		WindowsSandboxLevel:     windowsLevel,
	})

	return turnSandboxPolicy{
		SandboxType:             sandboxType,
		FileSystemSandboxPolicy: fsPolicy,
		NetworkSandboxPolicy:    networkPolicy,
	}
}

// runtimePermissionsForMode mirrors PermissionProfile::to_runtime_permissions for
// the three built-in profiles a sandbox mode selects. It returns the resolved
// filesystem and network policies the backend enforces.
func runtimePermissionsForMode(
	mode protocol.SandboxMode,
	networkEnabled bool,
) (protocol.FileSystemSandboxPolicy, protocol.NetworkSandboxPolicy) {
	switch mode {
	case protocol.SandboxModeDangerFullAccess:
		// PermissionProfile::Disabled: unrestricted filesystem, network ALWAYS
		// enabled (network_sandbox_policy() returns Enabled for Disabled).
		return protocol.FileSystemSandboxPolicy{Kind: protocol.FileSystemSandboxKindUnrestricted},
			protocol.NetworkSandboxPolicyEnabled
	case protocol.SandboxModeWorkspaceWrite:
		return workspaceWriteFileSystemSandboxPolicy(), networkSandboxPolicyForFlag(networkEnabled)
	default: // read-only (and the zero value)
		return readOnlyFileSystemSandboxPolicy(), networkSandboxPolicyForFlag(networkEnabled)
	}
}

// networkSandboxPolicyForFlag maps the network-access flag to the policy enum.
func networkSandboxPolicyForFlag(enabled bool) protocol.NetworkSandboxPolicy {
	if enabled {
		return protocol.NetworkSandboxPolicyEnabled
	}
	return protocol.NetworkSandboxPolicyRestricted
}

// readOnlyFileSystemSandboxPolicy mirrors PermissionProfile::read_only's
// filesystem policy: restricted with a single root-read entry.
func readOnlyFileSystemSandboxPolicy() protocol.FileSystemSandboxPolicy {
	return protocol.FileSystemSandboxPolicy{
		Kind: protocol.FileSystemSandboxKindRestricted,
		Entries: []protocol.FileSystemSandboxEntry{{
			Path:   protocol.NewFileSystemSpecialPath(protocol.FileSystemSpecialPath{Kind: protocol.FileSystemSpecialPathKindRoot}),
			Access: protocol.FileSystemAccessModeRead,
		}},
	}
}

// workspaceWriteFileSystemSandboxPolicy mirrors
// FileSystemSandboxPolicy::workspace_write for the default config (no extra
// writable_roots, tmpdir + /tmp included): root read, project_roots write,
// /tmp write, $TMPDIR write. The .git/.codex/.agents read-only carveouts are
// materialized by the seatbelt backend from the writable roots at spawn time
// (getWritableRootsWithCwd -> defaultReadOnlySubpathsForWritableRoot), so they
// are not duplicated into the policy entries here.
//
// STUB: the legacy sandbox_workspace_write knobs (additional writable_roots,
// exclude_tmpdir_env_var, exclude_slash_tmp) are not threaded through core's
// reduced configuration; the default profile is used. Tracked in DEVIATIONS.md.
func workspaceWriteFileSystemSandboxPolicy() protocol.FileSystemSandboxPolicy {
	return protocol.FileSystemSandboxPolicy{
		Kind: protocol.FileSystemSandboxKindRestricted,
		Entries: []protocol.FileSystemSandboxEntry{
			{
				Path:   protocol.NewFileSystemSpecialPath(protocol.FileSystemSpecialPath{Kind: protocol.FileSystemSpecialPathKindRoot}),
				Access: protocol.FileSystemAccessModeRead,
			},
			{
				Path:   protocol.NewFileSystemSpecialPath(protocol.NewProjectRootsSpecialPath(nil)),
				Access: protocol.FileSystemAccessModeWrite,
			},
			{
				Path:   protocol.NewFileSystemSpecialPath(protocol.FileSystemSpecialPath{Kind: protocol.FileSystemSpecialPathKindSlashTmp}),
				Access: protocol.FileSystemAccessModeWrite,
			},
			{
				Path:   protocol.NewFileSystemSpecialPath(protocol.FileSystemSpecialPath{Kind: protocol.FileSystemSpecialPathKindTmpdir}),
				Access: protocol.FileSystemAccessModeWrite,
			},
		},
	}
}
