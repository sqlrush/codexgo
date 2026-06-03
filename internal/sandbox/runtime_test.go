package sandbox

import (
	"reflect"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// boolPtr returns a pointer to the given bool.
func boolPtr(b bool) *bool { return &b }

// fsPermsFromReadWrite builds a FileSystemPermissions overlay from read/write
// root lists, mirroring FileSystemPermissions::from_read_write_roots.
func fsPermsFromReadWrite(read, write []string) protocol.FileSystemPermissions {
	var entries []protocol.FileSystemSandboxEntry
	for _, p := range read {
		entries = append(entries, protocol.FileSystemSandboxEntry{
			Path:   protocol.NewFileSystemPath(protocol.AbsolutePath(p)),
			Access: protocol.FileSystemAccessModeRead,
		})
	}
	for _, p := range write {
		entries = append(entries, protocol.FileSystemSandboxEntry{
			Path:   protocol.NewFileSystemPath(protocol.AbsolutePath(p)),
			Access: protocol.FileSystemAccessModeWrite,
		})
	}
	return protocol.FileSystemPermissions{Entries: entries}
}

// containsEntryForTest reports whether the policy holds an entry equal to want.
func containsEntryForTest(entries []protocol.FileSystemSandboxEntry, want protocol.FileSystemSandboxEntry) bool {
	for _, e := range entries {
		if entriesEqual(e, want) {
			return true
		}
	}
	return false
}

// TestEffectiveFileSystemSandboxPolicyNoOverlay ports
// effective_file_system_sandbox_policy_returns_base_policy_without_additional_permissions.
func TestEffectiveFileSystemSandboxPolicyNoOverlay(t *testing.T) {
	base := restricted(
		rootEntry(protocol.FileSystemAccessModeRead),
		pathEntry("/work/denied", protocol.FileSystemAccessModeDeny),
	)
	got := effectiveFileSystemSandboxPolicy(base, nil)
	if !reflect.DeepEqual(got, base) {
		t.Fatalf("effective policy without overlay = %+v, want base %+v", got, base)
	}
}

// TestEffectiveFileSystemSandboxPolicyMergesWriteRoots ports
// effective_file_system_sandbox_policy_merges_additional_write_roots and
// merge_file_system_policy_with_additional_permissions_preserves_unreadable_roots.
func TestEffectiveFileSystemSandboxPolicyMergesWriteRoots(t *testing.T) {
	allowed := "/work/allowed"
	denied := "/work/denied"
	base := restricted(
		rootEntry(protocol.FileSystemAccessModeRead),
		pathEntry(denied, protocol.FileSystemAccessModeDeny),
	)
	overlay := fsPermsFromReadWrite(nil, []string{allowed})
	additional := &protocol.AdditionalPermissionProfile{FileSystem: &overlay}

	got := effectiveFileSystemSandboxPolicy(base, additional)

	denyEntry := pathEntry(denied, protocol.FileSystemAccessModeDeny)
	writeEntry := pathEntry(allowed, protocol.FileSystemAccessModeWrite)
	if !containsEntryForTest(got.Entries, denyEntry) {
		t.Errorf("merged policy lost the deny entry: %+v", got.Entries)
	}
	if !containsEntryForTest(got.Entries, writeEntry) {
		t.Errorf("merged policy missing the added write entry: %+v", got.Entries)
	}
}

// TestMergeFileSystemPolicyOnlyAffectsRestricted verifies that unrestricted and
// external-sandbox base policies ignore the overlay entirely.
func TestMergeFileSystemPolicyOnlyAffectsRestricted(t *testing.T) {
	overlay := fsPermsFromReadWrite(nil, []string{"/work/allowed"})
	for _, base := range []protocol.FileSystemSandboxPolicy{unrestricted(), externalSandboxFS()} {
		got := mergeFileSystemPolicyWithAdditional(base, overlay)
		if !reflect.DeepEqual(got, base) {
			t.Errorf("kind %s: overlay altered non-restricted base: %+v", base.Kind, got)
		}
	}
}

// TestMergeFileSystemPolicyCarriesBoundedGlobDepth ports
// merge_file_system_policy_with_additional_permissions_carries_bounded_glob_scan_depth.
func TestMergeFileSystemPolicyCarriesBoundedGlobDepth(t *testing.T) {
	denyEnv := protocol.FileSystemSandboxEntry{
		Path:   protocol.NewFileSystemGlobPattern("**/*.env"),
		Access: protocol.FileSystemAccessModeDeny,
	}
	base := restricted(rootEntry(protocol.FileSystemAccessModeWrite))
	depth := uint(2)
	overlay := protocol.FileSystemPermissions{
		Entries:          []protocol.FileSystemSandboxEntry{denyEnv},
		GlobScanMaxDepth: &depth,
	}

	got := mergeFileSystemPolicyWithAdditional(base, overlay)

	if !containsEntryForTest(got.Entries, rootEntry(protocol.FileSystemAccessModeWrite)) {
		t.Error("merged policy lost the root write entry")
	}
	if !containsEntryForTest(got.Entries, denyEnv) {
		t.Error("merged policy lost the deny-glob entry")
	}
	if got.GlobScanMaxDepth == nil || *got.GlobScanMaxDepth != 2 {
		t.Fatalf("glob scan max depth = %v, want 2", got.GlobScanMaxDepth)
	}
}

// TestEffectiveNetworkSandboxPolicy covers the merge_network_access matrix.
func TestEffectiveNetworkSandboxPolicy(t *testing.T) {
	enabledOverlay := &protocol.AdditionalPermissionProfile{
		Network: &protocol.NetworkPermissions{Enabled: boolPtr(true)},
	}
	disabledOverlay := &protocol.AdditionalPermissionProfile{
		Network: &protocol.NetworkPermissions{Enabled: boolPtr(false)},
	}

	cases := []struct {
		name       string
		base       protocol.NetworkSandboxPolicy
		additional *protocol.AdditionalPermissionProfile
		want       protocol.NetworkSandboxPolicy
	}{
		{"nil-overlay-keeps-base-restricted", protocol.NetworkSandboxPolicyRestricted, nil, protocol.NetworkSandboxPolicyRestricted},
		{"nil-overlay-keeps-base-enabled", protocol.NetworkSandboxPolicyEnabled, nil, protocol.NetworkSandboxPolicyEnabled},
		{"base-enabled-overlay-disabled-stays-enabled", protocol.NetworkSandboxPolicyEnabled, disabledOverlay, protocol.NetworkSandboxPolicyEnabled},
		{"base-restricted-overlay-enables", protocol.NetworkSandboxPolicyRestricted, enabledOverlay, protocol.NetworkSandboxPolicyEnabled},
		{"base-restricted-overlay-disabled-stays-restricted", protocol.NetworkSandboxPolicyRestricted, disabledOverlay, protocol.NetworkSandboxPolicyRestricted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := effectiveNetworkSandboxPolicy(tc.base, tc.additional); got != tc.want {
				t.Fatalf("effectiveNetworkSandboxPolicy = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestEffectivePermissionProfileExternalEnablesNetwork ports the manager test
// transform_additional_permissions_enable_network_for_external_sandbox: an
// External profile with a network-enabling overlay yields External{Enabled}.
func TestEffectivePermissionProfileExternalEnablesNetwork(t *testing.T) {
	base := protocol.NewExternalPermissionProfile(protocol.NetworkSandboxPolicyRestricted)
	overlayFS := fsPermsFromReadWrite([]string{"/work/allowed"}, nil)
	additional := &protocol.AdditionalPermissionProfile{
		Network:    &protocol.NetworkPermissions{Enabled: boolPtr(true)},
		FileSystem: &overlayFS,
	}

	got := EffectivePermissionProfile(base, additional)
	want := protocol.NewExternalPermissionProfile(protocol.NetworkSandboxPolicyEnabled)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("effective profile = %+v, want %+v", got, want)
	}
}

// TestEffectivePermissionProfilePreservesDeniedEntries ports the manager test
// transform_additional_permissions_preserves_denied_entries: a Managed profile
// keeps deny entries when an allow overlay is folded in.
func TestEffectivePermissionProfilePreservesDeniedEntries(t *testing.T) {
	allowed := "/work/allowed"
	denied := "/work/denied"
	basePolicy := restricted(
		rootEntry(protocol.FileSystemAccessModeRead),
		pathEntry(denied, protocol.FileSystemAccessModeDeny),
	)
	base := protocol.NewManagedPermissionProfile(
		protocol.NewRestrictedManagedFileSystem(basePolicy.Entries, nil),
		protocol.NetworkSandboxPolicyRestricted,
	)
	overlay := fsPermsFromReadWrite(nil, []string{allowed})
	additional := &protocol.AdditionalPermissionProfile{FileSystem: &overlay}

	got := EffectivePermissionProfile(base, additional)
	if got.Kind != protocol.PermissionProfileManaged {
		t.Fatalf("profile kind = %v, want managed", got.Kind)
	}
	if got.Network != protocol.NetworkSandboxPolicyRestricted {
		t.Fatalf("network = %q, want restricted", got.Network)
	}
	entries := got.FileSystem.Entries
	if !containsEntryForTest(entries, pathEntry(denied, protocol.FileSystemAccessModeDeny)) {
		t.Errorf("effective profile lost deny entry: %+v", entries)
	}
	if !containsEntryForTest(entries, pathEntry(allowed, protocol.FileSystemAccessModeWrite)) {
		t.Errorf("effective profile missing added write entry: %+v", entries)
	}
}

// TestProfileRuntimePermissions verifies the profile <-> runtime projection arms.
func TestProfileRuntimePermissions(t *testing.T) {
	cases := []struct {
		name       string
		profile    protocol.PermissionProfile
		wantFSKind protocol.FileSystemSandboxKind
		wantNet    protocol.NetworkSandboxPolicy
	}{
		{
			"managed-restricted",
			protocol.NewManagedPermissionProfile(
				protocol.NewRestrictedManagedFileSystem(nil, nil),
				protocol.NetworkSandboxPolicyRestricted,
			),
			protocol.FileSystemSandboxKindRestricted,
			protocol.NetworkSandboxPolicyRestricted,
		},
		{
			"managed-unrestricted",
			protocol.NewManagedPermissionProfile(
				protocol.NewUnrestrictedManagedFileSystem(),
				protocol.NetworkSandboxPolicyEnabled,
			),
			protocol.FileSystemSandboxKindUnrestricted,
			protocol.NetworkSandboxPolicyEnabled,
		},
		{
			"external",
			protocol.NewExternalPermissionProfile(protocol.NetworkSandboxPolicyEnabled),
			protocol.FileSystemSandboxKindExternalSandbox,
			protocol.NetworkSandboxPolicyEnabled,
		},
		{
			// Disabled => unrestricted fs + network enabled.
			"disabled",
			protocol.NewDisabledPermissionProfile(),
			protocol.FileSystemSandboxKindUnrestricted,
			protocol.NetworkSandboxPolicyEnabled,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs, net := profileRuntimePermissions(tc.profile)
			if fs.Kind != tc.wantFSKind {
				t.Errorf("fs kind = %v, want %v", fs.Kind, tc.wantFSKind)
			}
			if net != tc.wantNet {
				t.Errorf("network = %q, want %q", net, tc.wantNet)
			}
		})
	}
}

// TestProfileFromRuntimeWithEnforcement verifies enforcement-aware reconstruction:
// an unrestricted policy collapses to Disabled only when enforcement is Disabled.
func TestProfileFromRuntimeWithEnforcement(t *testing.T) {
	unrestrictedFS := unrestricted()

	disabled := profileFromRuntimeWithEnforcement(
		protocol.SandboxEnforcementDisabled,
		unrestrictedFS,
		protocol.NetworkSandboxPolicyEnabled,
	)
	if disabled.Kind != protocol.PermissionProfileDisabled {
		t.Fatalf("disabled enforcement => kind %v, want disabled", disabled.Kind)
	}

	managed := profileFromRuntimeWithEnforcement(
		protocol.SandboxEnforcementManaged,
		unrestrictedFS,
		protocol.NetworkSandboxPolicyEnabled,
	)
	if managed.Kind != protocol.PermissionProfileManaged {
		t.Fatalf("managed enforcement => kind %v, want managed", managed.Kind)
	}
	if managed.FileSystem.Kind != protocol.ManagedFileSystemPermissionsUnrestricted {
		t.Fatalf("managed fs kind = %v, want unrestricted", managed.FileSystem.Kind)
	}

	external := profileFromRuntimeWithEnforcement(
		protocol.SandboxEnforcementExternal,
		externalSandboxFS(),
		protocol.NetworkSandboxPolicyRestricted,
	)
	if external.Kind != protocol.PermissionProfileExternal {
		t.Fatalf("external fs => kind %v, want external", external.Kind)
	}
}
