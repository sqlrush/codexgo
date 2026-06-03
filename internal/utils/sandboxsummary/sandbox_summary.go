package sandboxsummary

import (
	"fmt"
	"strings"
)

// networkAccessEnabledSuffix is appended when a policy permits outbound
// network traffic. It matches the Rust literal exactly.
const networkAccessEnabledSuffix = " (network access enabled)"

// SummarizeSandboxPolicy renders a one-line, human-readable summary of a
// sandbox policy.
//
// It is a faithful port of the Rust `summarize_sandbox_policy`. The produced
// strings are byte-for-byte identical to the Rust output:
//
//   - danger-full-access  -> "danger-full-access"
//   - read-only           -> "read-only" [+ network suffix]
//   - external-sandbox    -> "external-sandbox" [+ network suffix]
//   - workspace-write     -> "workspace-write [<roots>]" [+ network suffix]
//
// An unrecognized Kind yields its raw string form, which keeps the function
// total and never panics on unexpected input.
func SummarizeSandboxPolicy(policy SandboxPolicy) string {
	switch policy.Kind {
	case KindDangerFullAccess:
		return string(KindDangerFullAccess)
	case KindReadOnly:
		summary := string(KindReadOnly)
		if policy.ReadOnlyNetworkAccess {
			summary += networkAccessEnabledSuffix
		}
		return summary
	case KindExternalSandbox:
		summary := string(KindExternalSandbox)
		if policy.ExternalNetworkAccess.IsEnabled() {
			summary += networkAccessEnabledSuffix
		}
		return summary
	case KindWorkspaceWrite:
		return workspaceWriteSummary(
			policy.WritableRoots,
			policy.WorkspaceNetworkAccess,
			policy.ExcludeTmpdirEnvVar,
			policy.ExcludeSlashTmp,
		)
	default:
		// Unknown variant: fall back to the raw tag so the function stays
		// total. This cannot happen for values built via the constructors.
		return string(policy.Kind)
	}
}

// workspaceWriteSummary builds the workspace-write summary line shared by
// SummarizeSandboxPolicy and SummarizePermissionProfile.
//
// The writable-entry ordering mirrors the Rust source precisely:
//
//	workdir, [/tmp], [$TMPDIR], <writable roots...>
//
// "/tmp" is included unless excludeSlashTmp is set, and "$TMPDIR" is included
// unless excludeTmpdirEnvVar is set. The roots slice is appended verbatim and
// is never mutated.
func workspaceWriteSummary(roots []string, networkAccess, excludeTmpdirEnvVar, excludeSlashTmp bool) string {
	entries := make([]string, 0, len(roots)+3)
	entries = append(entries, "workdir")
	if !excludeSlashTmp {
		entries = append(entries, "/tmp")
	}
	if !excludeTmpdirEnvVar {
		entries = append(entries, "$TMPDIR")
	}
	entries = append(entries, roots...)

	summary := fmt.Sprintf("%s [%s]", string(KindWorkspaceWrite), strings.Join(entries, ", "))
	if networkAccess {
		summary += networkAccessEnabledSuffix
	}
	return summary
}
