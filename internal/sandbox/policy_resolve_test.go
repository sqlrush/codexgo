package sandbox

import (
	"path/filepath"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// projectRootsEntry builds a Special(ProjectRoots) entry with no subpath.
func projectRootsEntry(access protocol.FileSystemAccessMode) protocol.FileSystemSandboxEntry {
	return protocol.FileSystemSandboxEntry{
		Path:   protocol.NewFileSystemSpecialPath(protocol.NewProjectRootsSpecialPath(nil)),
		Access: access,
	}
}

// resolveAgainst joins target onto base the same way the production code does,
// so expectations line up with resolved entry paths.
func resolveAgainst(t *testing.T, target, base string) string {
	t.Helper()
	return abspath.ResolvePathAgainstBase(target, base).Path()
}

// canonicalCwd returns the symlink-resolved absolute form of cwd, matching how
// dedupPaths / get*Roots normalize paths.
func canonicalCwd(t *testing.T, cwd string) string {
	t.Helper()
	abs, err := abspath.FromAbsolutePath(cwd)
	if err != nil {
		t.Fatalf("FromAbsolutePath(%q): %v", cwd, err)
	}
	resolved, err := filepath.EvalSymlinks(abs.Path())
	if err != nil {
		return abs.Path()
	}
	return resolved
}

// TestResolveAccessWithCwdMostSpecific ports
// resolve_access_with_cwd_uses_most_specific_entry: the deepest matching entry
// wins, with deny shadowing a broader write.
func TestResolveAccessWithCwdMostSpecific(t *testing.T) {
	cwd := t.TempDir()
	docs := resolveAgainst(t, "docs", cwd)
	docsPrivate := resolveAgainst(t, "docs/private", cwd)
	docsPrivatePublic := resolveAgainst(t, "docs/private/public", cwd)

	policy := restricted(
		projectRootsEntry(protocol.FileSystemAccessModeWrite),
		pathEntry(docs, protocol.FileSystemAccessModeRead),
		pathEntry(docsPrivate, protocol.FileSystemAccessModeDeny),
		pathEntry(docsPrivatePublic, protocol.FileSystemAccessModeWrite),
	)

	cases := []struct {
		name   string
		target string
		want   protocol.FileSystemAccessMode
	}{
		{"cwd-root-write", cwd, protocol.FileSystemAccessModeWrite},
		{"docs-read", docs, protocol.FileSystemAccessModeRead},
		{"docs-private-deny", docsPrivate, protocol.FileSystemAccessModeDeny},
		{"docs-private-public-write", docsPrivatePublic, protocol.FileSystemAccessModeWrite},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveAccessWithCwd(policy, tc.target, cwd)
			if got != tc.want {
				t.Fatalf("resolveAccessWithCwd(%q) = %q, want %q", tc.target, got, tc.want)
			}
		})
	}
}

// TestResolveAccessUnrestrictedAndExternal verifies the short-circuit arms:
// unrestricted and external-sandbox policies grant write everywhere.
func TestResolveAccessUnrestrictedAndExternal(t *testing.T) {
	cwd := t.TempDir()
	for _, p := range []protocol.FileSystemSandboxPolicy{unrestricted(), externalSandboxFS()} {
		if got := resolveAccessWithCwd(p, cwd, cwd); got != protocol.FileSystemAccessModeWrite {
			t.Fatalf("kind %s: resolveAccessWithCwd = %q, want write", p.Kind, got)
		}
	}
}

// TestRootWriteWithReadOnlyChildIsNotFullDiskWrite ports
// root_write_with_read_only_child_is_not_full_disk_write.
func TestRootWriteWithReadOnlyChildIsNotFullDiskWrite(t *testing.T) {
	cwd := t.TempDir()
	docs := resolveAgainst(t, "docs", cwd)
	policy := restricted(
		rootEntry(protocol.FileSystemAccessModeWrite),
		pathEntry(docs, protocol.FileSystemAccessModeRead),
	)

	if fsHasFullDiskWriteAccess(policy) {
		t.Fatal("expected not full-disk write with a read-only child carveout")
	}
	if got := resolveAccessWithCwd(policy, docs, cwd); got != protocol.FileSystemAccessModeRead {
		t.Fatalf("docs access = %q, want read", got)
	}
	// The legacy projection cannot express a write outside the workspace root.
	if _, err := toLegacySandboxPolicy(policy, protocol.NetworkSandboxPolicyRestricted, cwd); err == nil {
		t.Fatal("expected to_legacy_sandbox_policy to error for root-write carveout policy")
	}
}

// TestDuplicateRootDenyPreventsFullDiskWrite ports
// duplicate_root_deny_prevents_full_disk_write_access.
func TestDuplicateRootDenyPreventsFullDiskWrite(t *testing.T) {
	cwd := t.TempDir()
	root := filesystemRootForPath(canonicalCwd(t, cwd))
	policy := restricted(
		rootEntry(protocol.FileSystemAccessModeWrite),
		rootEntry(protocol.FileSystemAccessModeDeny),
	)

	if fsHasFullDiskWriteAccess(policy) {
		t.Fatal("expected not full-disk write when a duplicate root deny is present")
	}
	if got := resolveAccessWithCwd(policy, root, cwd); got != protocol.FileSystemAccessModeDeny {
		t.Fatalf("root access = %q, want deny", got)
	}
}

// TestSameSpecificityWriteOverrideKeepsFullDiskWrite ports
// same_specificity_write_override_keeps_full_disk_write_access: a same-target
// write override of a read entry preserves full-disk write.
func TestSameSpecificityWriteOverrideKeepsFullDiskWrite(t *testing.T) {
	cwd := t.TempDir()
	docs := resolveAgainst(t, "docs", cwd)
	policy := restricted(
		rootEntry(protocol.FileSystemAccessModeWrite),
		pathEntry(docs, protocol.FileSystemAccessModeRead),
		pathEntry(docs, protocol.FileSystemAccessModeWrite),
	)

	if !fsHasFullDiskWriteAccess(policy) {
		t.Fatal("expected full-disk write with a same-target write override")
	}
	if got := resolveAccessWithCwd(policy, docs, cwd); got != protocol.FileSystemAccessModeWrite {
		t.Fatalf("docs access = %q, want write", got)
	}
}

// TestRootDenyDoesNotMaterializeAsUnreadableRoot ports
// root_deny_does_not_materialize_as_unreadable_root.
func TestRootDenyDoesNotMaterializeAsUnreadableRoot(t *testing.T) {
	cwd := t.TempDir()
	docs := resolveAgainst(t, "docs", cwd)
	expectedDocs := filepath.Join(canonicalCwd(t, cwd), "docs")

	policy := restricted(
		rootEntry(protocol.FileSystemAccessModeDeny),
		pathEntry(docs, protocol.FileSystemAccessModeRead),
	)

	if got := resolveAccessWithCwd(policy, docs, cwd); got != protocol.FileSystemAccessModeRead {
		t.Fatalf("docs access = %q, want read", got)
	}

	readable := getReadableRootsWithCwd(policy, cwd)
	if len(readable) != 1 || readable[0] != expectedDocs {
		t.Fatalf("readable roots = %v, want [%q]", readable, expectedDocs)
	}
	if unread := getUnreadableRootsWithCwd(policy, cwd); len(unread) != 0 {
		t.Fatalf("unreadable roots = %v, want empty", unread)
	}
}

// TestFullDiskReadWriteAccess covers has_full_disk_read_access /
// has_full_disk_write_access across kinds and deny restrictions.
func TestFullDiskReadWriteAccess(t *testing.T) {
	cases := []struct {
		name      string
		policy    protocol.FileSystemSandboxPolicy
		wantRead  bool
		wantWrite bool
	}{
		{"unrestricted", unrestricted(), true, true},
		{"external-sandbox", externalSandboxFS(), true, true},
		{"root-read-only", restricted(rootEntry(protocol.FileSystemAccessModeRead)), true, false},
		{"root-write", restricted(rootEntry(protocol.FileSystemAccessModeWrite)), true, true},
		{
			"root-read-with-deny",
			restricted(
				rootEntry(protocol.FileSystemAccessModeRead),
				pathEntry("/secret", protocol.FileSystemAccessModeDeny),
			),
			false,
			false,
		},
		{"empty-restricted", restricted(), false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := fsHasFullDiskReadAccess(tc.policy); got != tc.wantRead {
				t.Errorf("fsHasFullDiskReadAccess = %v, want %v", got, tc.wantRead)
			}
			if got := fsHasFullDiskWriteAccess(tc.policy); got != tc.wantWrite {
				t.Errorf("fsHasFullDiskWriteAccess = %v, want %v", got, tc.wantWrite)
			}
		})
	}
}

// TestCanReadCanWritePath exercises can_read_path_with_cwd / can_write_path_with_cwd
// for a read-only-root-with-writable-cwd policy.
func TestCanReadCanWritePath(t *testing.T) {
	cwd := t.TempDir()
	outside := t.TempDir()
	child := resolveAgainst(t, "child", cwd)

	policy := restricted(
		rootEntry(protocol.FileSystemAccessModeRead),
		projectRootsEntry(protocol.FileSystemAccessModeWrite),
	)

	cases := []struct {
		name     string
		target   string
		canRead  bool
		canWrite bool
	}{
		{"cwd-readwrite", cwd, true, true},
		{"child-readwrite", child, true, true},
		{"outside-read-only", outside, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := canReadPathWithCwd(policy, tc.target, cwd); got != tc.canRead {
				t.Errorf("canReadPathWithCwd(%q) = %v, want %v", tc.target, got, tc.canRead)
			}
			if got := canWritePathWithCwd(policy, tc.target, cwd); got != tc.canWrite {
				t.Errorf("canWritePathWithCwd(%q) = %v, want %v", tc.target, got, tc.canWrite)
			}
		})
	}
}

// TestProtectedMetadataWriteDenied ports
// filesystem_policy_blocks_protected_metadata_path_writes_by_default: writes
// into .git/.agents/.codexgo under a writable root are denied, and the writable
// root surfaces all three protected metadata names.
func TestProtectedMetadataWriteDenied(t *testing.T) {
	cwd := t.TempDir()
	root := canonicalCwd(t, cwd)
	policy := restricted(pathEntry(root, protocol.FileSystemAccessModeWrite))

	for _, sub := range []string{".git/config", ".agents/config", ".codexgo/config.toml"} {
		target := filepath.Join(root, sub)
		if canWritePathWithCwd(policy, target, cwd) {
			t.Errorf("expected write to %q to be denied", target)
		}
	}

	roots := getWritableRootsWithCwd(policy, cwd)
	if len(roots) != 1 {
		t.Fatalf("writable roots = %d, want 1 (%+v)", len(roots), roots)
	}
	want := []string{".git", ".agents", ".codexgo"}
	got := roots[0].ProtectedMetadataNames
	if len(got) != len(want) {
		t.Fatalf("protected metadata names = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("protected metadata names = %v, want %v", got, want)
		}
	}
}

// TestUnreadableGlobsWithCwd checks deny-glob patterns are resolved against the
// cwd and returned sorted/deduped, while non-restricted policies return none.
func TestUnreadableGlobsWithCwd(t *testing.T) {
	cwd := t.TempDir()
	policy := restricted(
		rootEntry(protocol.FileSystemAccessModeRead),
		protocol.FileSystemSandboxEntry{
			Path:   protocol.NewFileSystemGlobPattern("**/*.env"),
			Access: protocol.FileSystemAccessModeDeny,
		},
		// A non-deny glob must be ignored.
		protocol.FileSystemSandboxEntry{
			Path:   protocol.NewFileSystemGlobPattern("**/*.txt"),
			Access: protocol.FileSystemAccessModeRead,
		},
	)

	globs := getUnreadableGlobsWithCwd(policy, cwd)
	want := resolveAgainst(t, "**/*.env", cwd)
	if len(globs) != 1 || globs[0] != want {
		t.Fatalf("unreadable globs = %v, want [%q]", globs, want)
	}

	if got := getUnreadableGlobsWithCwd(unrestricted(), cwd); got != nil {
		t.Fatalf("unrestricted unreadable globs = %v, want nil", got)
	}
}

// TestGetWritableRootsFullDiskWriteReturnsNil verifies full-disk write policies
// surface no writable roots (the whole disk is writable).
func TestGetWritableRootsFullDiskWriteReturnsNil(t *testing.T) {
	cwd := t.TempDir()
	if got := getWritableRootsWithCwd(unrestricted(), cwd); got != nil {
		t.Fatalf("unrestricted writable roots = %v, want nil", got)
	}
	if got := getWritableRootsWithCwd(restricted(rootEntry(protocol.FileSystemAccessModeWrite)), cwd); got != nil {
		t.Fatalf("root-write writable roots = %v, want nil", got)
	}
}
