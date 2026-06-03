// Package sandboxsummary produces human-readable summaries of a sandbox
// policy, a permission profile, and the effective configuration.
//
// It is a faithful Go port of the OpenAI codex Rust crate
// `codex-utils-sandbox-summary`. The externally-observable string formats are
// preserved exactly so that callers depending on the textual output (for
// example status lines printed to a user) behave identically to the Rust
// implementation.
//
// The Rust crate consumes rich types from sibling crates
// (`SandboxPolicy`, `NetworkAccess`, `PermissionProfile`, `Config`). Those
// crates are ported separately; this package depends only on the standard
// library and defines the minimal value shapes and interfaces required to
// reproduce the summaries. Conversions that belong to the protocol crate
// (notably turning a permission profile into a legacy sandbox policy) are
// supplied to this package through small interfaces rather than reimplemented
// here.
//
// All functions are pure: they never mutate caller-provided slices, maps, or
// structs, and always return newly constructed values.
package sandboxsummary

// SandboxPolicyKind enumerates the tagged variants of a sandbox policy.
//
// It mirrors the serde tag (`type`) values of the Rust `SandboxPolicy` enum:
// "danger-full-access", "read-only", "external-sandbox", and
// "workspace-write".
type SandboxPolicyKind string

const (
	// KindDangerFullAccess imposes no restrictions whatsoever.
	KindDangerFullAccess SandboxPolicyKind = "danger-full-access"
	// KindReadOnly grants read-only filesystem access.
	KindReadOnly SandboxPolicyKind = "read-only"
	// KindExternalSandbox indicates the process is already inside an external
	// sandbox that grants full disk access.
	KindExternalSandbox SandboxPolicyKind = "external-sandbox"
	// KindWorkspaceWrite grants read-only access plus writes to the workspace.
	KindWorkspaceWrite SandboxPolicyKind = "workspace-write"
)

// NetworkAccess mirrors the Rust `NetworkAccess` enum used by the
// external-sandbox variant. The zero value is Restricted, matching the Rust
// default.
type NetworkAccess int

const (
	// NetworkRestricted denies outbound network access.
	NetworkRestricted NetworkAccess = iota
	// NetworkEnabled allows outbound network access.
	NetworkEnabled
)

// IsEnabled reports whether outbound network access is allowed, mirroring
// `NetworkAccess::is_enabled`.
func (n NetworkAccess) IsEnabled() bool {
	return n == NetworkEnabled
}

// SandboxPolicy is a value-shape port of the Rust `SandboxPolicy` enum.
//
// The active variant is selected by Kind. Only the fields relevant to the
// active variant are consulted, exactly as the Rust pattern match does:
//
//   - KindReadOnly:        ReadOnlyNetworkAccess
//   - KindExternalSandbox: ExternalNetworkAccess
//   - KindWorkspaceWrite:  WritableRoots, WorkspaceNetworkAccess,
//     ExcludeTmpdirEnvVar, ExcludeSlashTmp
//   - KindDangerFullAccess: (no fields)
type SandboxPolicy struct {
	// Kind selects the active variant.
	Kind SandboxPolicyKind

	// ReadOnlyNetworkAccess corresponds to ReadOnly.network_access (a bool in
	// Rust).
	ReadOnlyNetworkAccess bool

	// ExternalNetworkAccess corresponds to ExternalSandbox.network_access.
	ExternalNetworkAccess NetworkAccess

	// WritableRoots corresponds to WorkspaceWrite.writable_roots. Each element
	// is the string form of an absolute path (Rust's `to_string_lossy`).
	WritableRoots []string
	// WorkspaceNetworkAccess corresponds to WorkspaceWrite.network_access.
	WorkspaceNetworkAccess bool
	// ExcludeTmpdirEnvVar corresponds to WorkspaceWrite.exclude_tmpdir_env_var.
	ExcludeTmpdirEnvVar bool
	// ExcludeSlashTmp corresponds to WorkspaceWrite.exclude_slash_tmp.
	ExcludeSlashTmp bool
}

// DangerFullAccess returns a SandboxPolicy for the danger-full-access variant.
func DangerFullAccess() SandboxPolicy {
	return SandboxPolicy{Kind: KindDangerFullAccess}
}

// ReadOnly returns a SandboxPolicy for the read-only variant.
func ReadOnly(networkAccess bool) SandboxPolicy {
	return SandboxPolicy{Kind: KindReadOnly, ReadOnlyNetworkAccess: networkAccess}
}

// ExternalSandbox returns a SandboxPolicy for the external-sandbox variant.
func ExternalSandbox(networkAccess NetworkAccess) SandboxPolicy {
	return SandboxPolicy{Kind: KindExternalSandbox, ExternalNetworkAccess: networkAccess}
}

// WorkspaceWrite returns a SandboxPolicy for the workspace-write variant.
//
// The provided writableRoots slice is defensively copied so the returned value
// never aliases the caller's slice.
func WorkspaceWrite(writableRoots []string, networkAccess, excludeTmpdirEnvVar, excludeSlashTmp bool) SandboxPolicy {
	return SandboxPolicy{
		Kind:                   KindWorkspaceWrite,
		WritableRoots:          cloneStrings(writableRoots),
		WorkspaceNetworkAccess: networkAccess,
		ExcludeTmpdirEnvVar:    excludeTmpdirEnvVar,
		ExcludeSlashTmp:        excludeSlashTmp,
	}
}

// cloneStrings returns a new slice with the same elements as src. A nil input
// yields a nil output, matching the absence of writable roots.
func cloneStrings(src []string) []string {
	if src == nil {
		return nil
	}
	dst := make([]string, len(src))
	copy(dst, src)
	return dst
}
