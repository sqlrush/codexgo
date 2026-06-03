package sandboxsummary

// PermissionProfile is the minimal behavior SummarizePermissionProfile needs
// from a permission profile.
//
// The Rust crate calls two methods on `PermissionProfile`:
//
//   - to_legacy_sandbox_policy(cwd) -> io::Result<SandboxPolicy>
//   - network_sandbox_policy().is_enabled() -> bool
//
// Reproducing the full legacy-policy conversion would amount to reimplementing
// the protocol crate, so this package consumes those results through a small
// interface. The conversion is supplied by the (separately ported) protocol
// types, while this package owns the summary formatting exactly as the Rust
// source does.
type PermissionProfile interface {
	// ToLegacySandboxPolicy converts the profile to a legacy SandboxPolicy for
	// the given working directory. The boolean ok mirrors Rust's
	// `io::Result`: true corresponds to Ok, false to Err.
	ToLegacySandboxPolicy(cwd string) (policy SandboxPolicy, ok bool)

	// NetworkSandboxPolicyEnabled reports whether the profile's network policy
	// permits outbound access. It corresponds to
	// `network_sandbox_policy().is_enabled()` and is consulted only on the
	// error path.
	NetworkSandboxPolicyEnabled() bool
}

// SummarizePermissionProfile renders a one-line summary of a permission
// profile resolved against a working directory and the active runtime
// workspace roots.
//
// It is a faithful port of the Rust `summarize_permission_profile`:
//
//   - When the profile resolves to a workspace-write policy, the summary uses
//     the runtime workspace roots (excluding cwd) rather than the policy's own
//     writable roots, hiding internal writes. The /tmp and $TMPDIR entries
//     follow the resolved policy's exclude flags.
//   - Any other successfully resolved policy is delegated to
//     SummarizeSandboxPolicy.
//   - When resolution fails, the summary is "custom permissions", with the
//     network suffix appended when the profile's network policy is enabled.
//
// cwd is the working directory in string form. workspaceRoots is never
// mutated; the function only reads it.
func SummarizePermissionProfile(profile PermissionProfile, cwd string, workspaceRoots []string) string {
	policy, ok := profile.ToLegacySandboxPolicy(cwd)
	if !ok {
		if profile.NetworkSandboxPolicyEnabled() {
			return "custom permissions" + networkAccessEnabledSuffix
		}
		return "custom permissions"
	}

	if policy.Kind == KindWorkspaceWrite {
		roots := filterRoots(workspaceRoots, cwd)
		return workspaceWriteSummary(
			roots,
			policy.WorkspaceNetworkAccess,
			policy.ExcludeTmpdirEnvVar,
			policy.ExcludeSlashTmp,
		)
	}

	return SummarizeSandboxPolicy(policy)
}

// filterRoots returns a new slice containing every root that is not equal to
// cwd, preserving order. It mirrors the Rust `filter(|root| *root != cwd)`.
func filterRoots(roots []string, cwd string) []string {
	filtered := make([]string, 0, len(roots))
	for _, root := range roots {
		if root != cwd {
			filtered = append(filtered, root)
		}
	}
	return filtered
}
