package sandbox

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// specialEntry builds a Special entry from a FileSystemSpecialPath value.
func specialEntry(value protocol.FileSystemSpecialPath, access protocol.FileSystemAccessMode) protocol.FileSystemSandboxEntry {
	return protocol.FileSystemSandboxEntry{
		Path:   protocol.NewFileSystemSpecialPath(value),
		Access: access,
	}
}

// TestResolveSpecialPathProjectRootsSubpath verifies a ProjectRoots special path
// with a subpath resolves against the cwd.
func TestResolveSpecialPathProjectRootsSubpath(t *testing.T) {
	cwd := t.TempDir()
	sub := "nested/dir"
	policy := restricted(
		rootEntry(protocol.FileSystemAccessModeRead),
		specialEntry(protocol.NewProjectRootsSpecialPath(&sub), protocol.FileSystemAccessModeWrite),
	)

	target := resolveAgainst(t, sub, cwd)
	if got := resolveAccessWithCwd(policy, target, cwd); got != protocol.FileSystemAccessModeWrite {
		t.Fatalf("project-roots subpath access = %q, want write", got)
	}
}

// TestResolveSpecialPathTmpdir verifies the $TMPDIR special path resolves to the
// TMPDIR env var, and is dropped when TMPDIR is unset.
func TestResolveSpecialPathTmpdir(t *testing.T) {
	cwd := t.TempDir()
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	policy := restricted(
		rootEntry(protocol.FileSystemAccessModeRead),
		specialEntry(protocol.FileSystemSpecialPath{Kind: protocol.FileSystemSpecialPathKindTmpdir}, protocol.FileSystemAccessModeWrite),
	)

	resolvedTmp := normalizeAbsolute(tmp)
	if got := resolveAccessWithCwd(policy, resolvedTmp, cwd); got != protocol.FileSystemAccessModeWrite {
		t.Fatalf("tmpdir access = %q, want write", got)
	}

	// With TMPDIR unset, the special entry resolves to nothing and the path is
	// no longer writable.
	t.Setenv("TMPDIR", "")
	if got := resolveAccessWithCwd(policy, resolvedTmp, cwd); got == protocol.FileSystemAccessModeWrite {
		t.Fatalf("tmpdir access with TMPDIR unset = %q, want non-write", got)
	}
}

// TestResolveSpecialPathSlashTmp verifies the /tmp special path resolves when
// /tmp exists as a directory.
func TestResolveSpecialPathSlashTmp(t *testing.T) {
	info, err := os.Stat("/tmp")
	if err != nil || !info.IsDir() {
		t.Skip("/tmp not available")
	}
	cwd := t.TempDir()
	policy := restricted(
		rootEntry(protocol.FileSystemAccessModeRead),
		specialEntry(protocol.FileSystemSpecialPath{Kind: protocol.FileSystemSpecialPathKindSlashTmp}, protocol.FileSystemAccessModeWrite),
	)
	if got := resolveAccessWithCwd(policy, "/tmp", cwd); got != protocol.FileSystemAccessModeWrite {
		t.Fatalf("/tmp access = %q, want write", got)
	}
}

// TestToLegacyWorkspaceWriteTmpdirAndSlashTmp verifies the tmpdir/slash-tmp
// exclusion flags flip when those roots are granted writable in a workspace-write
// projection.
func TestToLegacyWorkspaceWriteTmpdirAndSlashTmp(t *testing.T) {
	cwd := t.TempDir()
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	info, err := os.Stat("/tmp")
	slashTmpOK := err == nil && info.IsDir()

	entries := []protocol.FileSystemSandboxEntry{
		rootEntry(protocol.FileSystemAccessModeRead),
		projectRootsEntry(protocol.FileSystemAccessModeWrite),
		specialEntry(protocol.FileSystemSpecialPath{Kind: protocol.FileSystemSpecialPathKindTmpdir}, protocol.FileSystemAccessModeWrite),
	}
	if slashTmpOK {
		entries = append(entries, specialEntry(protocol.FileSystemSpecialPath{Kind: protocol.FileSystemSpecialPathKindSlashTmp}, protocol.FileSystemAccessModeWrite))
	}
	policy := restricted(entries...)

	ws, err := toLegacySandboxPolicy(policy, protocol.NetworkSandboxPolicyRestricted, cwd)
	if err != nil {
		t.Fatalf("workspace-write: %v", err)
	}
	if ws.Type != protocol.SandboxPolicyTypeWorkspaceWrite {
		t.Fatalf("type = %v, want workspace-write", ws.Type)
	}
	// TMPDIR was granted writable, so it is NOT excluded.
	if ws.ExcludeTmpdirEnvVar {
		t.Errorf("ExcludeTmpdirEnvVar = true, want false (tmpdir granted)")
	}
	if slashTmpOK && ws.ExcludeSlashTmp {
		t.Errorf("ExcludeSlashTmp = true, want false (/tmp granted)")
	}
}

// TestFsHasWriteNarrowingEntries exercises has_write_narrowing_entries via
// has_full_disk_write_access: a same-target write override does not narrow, but a
// distinct read carveout does.
func TestFsHasWriteNarrowingEntries(t *testing.T) {
	cases := []struct {
		name       string
		policy     protocol.FileSystemSandboxPolicy
		wantNarrow bool
	}{
		{
			// Root write only => no narrowing.
			name:       "root-write-only",
			policy:     restricted(rootEntry(protocol.FileSystemAccessModeWrite)),
			wantNarrow: false,
		},
		{
			// A read carveout on a distinct path narrows.
			name: "read-carveout-narrows",
			policy: restricted(
				rootEntry(protocol.FileSystemAccessModeWrite),
				pathEntry("/work/docs", protocol.FileSystemAccessModeRead),
			),
			wantNarrow: true,
		},
		{
			// A deny glob narrows.
			name: "deny-glob-narrows",
			policy: restricted(
				rootEntry(protocol.FileSystemAccessModeWrite),
				protocol.FileSystemSandboxEntry{
					Path:   protocol.NewFileSystemGlobPattern("**/*.env"),
					Access: protocol.FileSystemAccessModeDeny,
				},
			),
			wantNarrow: true,
		},
		{
			// A same-target write override of a read entry does not narrow.
			name: "same-target-write-override-no-narrow",
			policy: restricted(
				rootEntry(protocol.FileSystemAccessModeWrite),
				pathEntry("/work/docs", protocol.FileSystemAccessModeRead),
				pathEntry("/work/docs", protocol.FileSystemAccessModeWrite),
			),
			wantNarrow: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := fsHasWriteNarrowingEntries(tc.policy); got != tc.wantNarrow {
				t.Fatalf("fsHasWriteNarrowingEntries = %v, want %v", got, tc.wantNarrow)
			}
		})
	}
}

// TestPathsShareTarget covers the share-target equivalence used by write
// narrowing: identical paths, special-vs-special, and special-vs-absolute.
func TestPathsShareTarget(t *testing.T) {
	pathA := protocol.NewFileSystemPath("/a/b")
	pathA2 := protocol.NewFileSystemPath("/a/b")
	pathB := protocol.NewFileSystemPath("/a/c")
	rootSpecial := protocol.NewFileSystemSpecialPath(protocol.FileSystemSpecialPath{Kind: protocol.FileSystemSpecialPathKindRoot})
	tmpSpecial := protocol.NewFileSystemSpecialPath(protocol.FileSystemSpecialPath{Kind: protocol.FileSystemSpecialPathKindTmpdir})
	slashTmpSpecial := protocol.NewFileSystemSpecialPath(protocol.FileSystemSpecialPath{Kind: protocol.FileSystemSpecialPathKindSlashTmp})
	slashTmpPath := protocol.NewFileSystemPath("/tmp")
	globA := protocol.NewFileSystemGlobPattern("**/*.env")
	globA2 := protocol.NewFileSystemGlobPattern("**/*.env")
	globB := protocol.NewFileSystemGlobPattern("**/*.txt")

	cases := []struct {
		name        string
		left, right protocol.FileSystemPath
		want        bool
	}{
		{"identical-paths", pathA, pathA2, true},
		{"distinct-paths", pathA, pathB, false},
		{"same-special-tmpdir", tmpSpecial, tmpSpecial, true},
		{"different-special", rootSpecial, tmpSpecial, false},
		{"slash-tmp-special-vs-tmp-path", slashTmpSpecial, slashTmpPath, true},
		{"identical-globs", globA, globA2, true},
		{"distinct-globs", globA, globB, false},
		{"glob-vs-path", globA, pathA, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pathsShareTarget(tc.left, tc.right); got != tc.want {
				t.Fatalf("pathsShareTarget = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestDefaultReadOnlySubpathsForWritableRoot verifies that existing .git/.codexgo
// directories under a writable root are reported as default read-only subpaths.
func TestDefaultReadOnlySubpathsForWritableRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".codexgo"), 0o755); err != nil {
		t.Fatalf("mkdir .codexgo: %v", err)
	}

	subs := defaultReadOnlySubpathsForWritableRoot(root, false)
	wantGit := filepath.Join(root, ".git")
	wantCodex := filepath.Join(root, ".codexgo")
	haveGit, haveCodex := false, false
	for _, s := range subs {
		if s == wantGit {
			haveGit = true
		}
		if s == wantCodex {
			haveCodex = true
		}
	}
	if !haveGit || !haveCodex {
		t.Fatalf("subpaths = %v, want both %q and %q", subs, wantGit, wantCodex)
	}
}
