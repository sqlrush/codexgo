package sandbox

import "github.com/sqlrush/codexgo/internal/protocol"

// This file ports the slice of codex-protocol's permission-resolution engine
// that the sandboxing crate depends on but that internal/protocol intentionally
// does not reimplement: PermissionProfile <-> (FileSystemSandboxPolicy,
// NetworkSandboxPolicy) conversions and the effective_* overlay folding from
// policy_transforms.rs.
//
// These helpers operate purely on the protocol serde types so internal/protocol
// is left untouched.

// profileRuntimePermissions mirrors PermissionProfile::to_runtime_permissions:
// it projects a profile to its (filesystem, network) runtime policies.
func profileRuntimePermissions(p protocol.PermissionProfile) (protocol.FileSystemSandboxPolicy, protocol.NetworkSandboxPolicy) {
	return profileFileSystemSandboxPolicy(p), profileNetworkSandboxPolicy(p)
}

// profileFileSystemSandboxPolicy mirrors PermissionProfile::file_system_sandbox_policy.
func profileFileSystemSandboxPolicy(p protocol.PermissionProfile) protocol.FileSystemSandboxPolicy {
	switch p.Kind {
	case protocol.PermissionProfileManaged:
		return p.FileSystem.ToSandboxPolicy()
	case protocol.PermissionProfileExternal:
		return protocol.FileSystemSandboxPolicy{Kind: protocol.FileSystemSandboxKindExternalSandbox}
	default: // Disabled
		return protocol.FileSystemSandboxPolicy{Kind: protocol.FileSystemSandboxKindUnrestricted}
	}
}

// profileNetworkSandboxPolicy mirrors PermissionProfile::network_sandbox_policy.
func profileNetworkSandboxPolicy(p protocol.PermissionProfile) protocol.NetworkSandboxPolicy {
	switch p.Kind {
	case protocol.PermissionProfileManaged, protocol.PermissionProfileExternal:
		return p.Network
	default: // Disabled
		return protocol.NetworkSandboxPolicyEnabled
	}
}

// profileFromRuntimeWithEnforcement mirrors
// PermissionProfile::from_runtime_permissions_with_enforcement: it rebuilds a
// tagged profile from runtime policies while honoring the requested enforcement.
func profileFromRuntimeWithEnforcement(
	enforcement protocol.SandboxEnforcement,
	fsPolicy protocol.FileSystemSandboxPolicy,
	network protocol.NetworkSandboxPolicy,
) protocol.PermissionProfile {
	switch fsPolicy.Kind {
	case protocol.FileSystemSandboxKindExternalSandbox:
		return protocol.NewExternalPermissionProfile(network)
	case protocol.FileSystemSandboxKindUnrestricted:
		if enforcement == protocol.SandboxEnforcementDisabled {
			return protocol.NewDisabledPermissionProfile()
		}
		return protocol.NewManagedPermissionProfile(managedFromSandboxPolicy(fsPolicy), network)
	default: // Restricted
		return protocol.NewManagedPermissionProfile(managedFromSandboxPolicy(fsPolicy), network)
	}
}

// managedFromSandboxPolicy mirrors ManagedFileSystemPermissions::from_sandbox_policy.
// ExternalSandbox is unreachable here because callers route it to External above.
func managedFromSandboxPolicy(fsPolicy protocol.FileSystemSandboxPolicy) protocol.ManagedFileSystemPermissions {
	switch fsPolicy.Kind {
	case protocol.FileSystemSandboxKindUnrestricted:
		return protocol.NewUnrestrictedManagedFileSystem()
	default: // Restricted
		return protocol.NewRestrictedManagedFileSystem(fsPolicy.Entries, fsPolicy.GlobScanMaxDepth)
	}
}

// effectiveFileSystemSandboxPolicy mirrors
// policy_transforms::effective_file_system_sandbox_policy: it folds the optional
// filesystem overlay into the base policy.
func effectiveFileSystemSandboxPolicy(
	base protocol.FileSystemSandboxPolicy,
	additional *protocol.AdditionalPermissionProfile,
) protocol.FileSystemSandboxPolicy {
	if additional == nil || additional.FileSystem == nil || additional.FileSystem.IsEmpty() {
		return base
	}
	return mergeFileSystemPolicyWithAdditional(base, *additional.FileSystem)
}

// mergeFileSystemPolicyWithAdditional mirrors
// merge_file_system_policy_with_additional_permissions. Only Restricted base
// policies absorb the overlay; Unrestricted and ExternalSandbox are returned
// unchanged.
func mergeFileSystemPolicyWithAdditional(
	base protocol.FileSystemSandboxPolicy,
	overlay protocol.FileSystemPermissions,
) protocol.FileSystemSandboxPolicy {
	if base.Kind != protocol.FileSystemSandboxKindRestricted {
		return base
	}

	entries := make([]protocol.FileSystemSandboxEntry, len(base.Entries))
	copy(entries, base.Entries)
	for _, entry := range overlay.Entries {
		if !containsEntry(entries, entry) {
			entries = append(entries, entry)
		}
	}
	return protocol.FileSystemSandboxPolicy{
		Kind:             protocol.FileSystemSandboxKindRestricted,
		GlobScanMaxDepth: mergeGlobScanMaxDepth(base.Entries, base.GlobScanMaxDepth, overlay.Entries, overlay.GlobScanMaxDepth),
		Entries:          entries,
	}
}

// effectiveNetworkSandboxPolicy mirrors
// policy_transforms::effective_network_sandbox_policy.
func effectiveNetworkSandboxPolicy(
	base protocol.NetworkSandboxPolicy,
	additional *protocol.AdditionalPermissionProfile,
) protocol.NetworkSandboxPolicy {
	if additional != nil && mergeNetworkAccess(base.IsEnabled(), *additional) {
		return protocol.NetworkSandboxPolicyEnabled
	}
	if additional != nil {
		return protocol.NetworkSandboxPolicyRestricted
	}
	return base
}

// mergeNetworkAccess mirrors policy_transforms::merge_network_access.
func mergeNetworkAccess(baseEnabled bool, additional protocol.AdditionalPermissionProfile) bool {
	if baseEnabled {
		return true
	}
	return additional.Network != nil && additional.Network.Enabled != nil && *additional.Network.Enabled
}

// containsEntry reports whether entries already holds an equal entry, mirroring
// Vec::contains on FileSystemSandboxEntry.
func containsEntry(entries []protocol.FileSystemSandboxEntry, target protocol.FileSystemSandboxEntry) bool {
	for i := range entries {
		if entriesEqual(entries[i], target) {
			return true
		}
	}
	return false
}

// entriesEqual mirrors the derived PartialEq on FileSystemSandboxEntry.
func entriesEqual(a, b protocol.FileSystemSandboxEntry) bool {
	return a.Access == b.Access && filesystemPathsEqual(a.Path, b.Path)
}

// filesystemPathsEqual mirrors the derived PartialEq on FileSystemPath.
func filesystemPathsEqual(a, b protocol.FileSystemPath) bool {
	if a.Type != b.Type {
		return false
	}
	switch a.Type {
	case protocol.FileSystemPathTypePath:
		return a.Path == b.Path
	case protocol.FileSystemPathTypeGlobPattern:
		return a.Pattern == b.Pattern
	default: // Special
		return specialPathsEqual(a.Special, b.Special)
	}
}

// specialPathsEqual mirrors the derived PartialEq on FileSystemSpecialPath.
func specialPathsEqual(a, b *protocol.FileSystemSpecialPath) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case protocol.FileSystemSpecialPathKindProjectRoots:
		return ptrStringEqual(a.Subpath, b.Subpath)
	case protocol.FileSystemSpecialPathKindUnknown:
		return a.Path == b.Path && ptrStringEqual(a.Subpath, b.Subpath)
	default:
		return true
	}
}
