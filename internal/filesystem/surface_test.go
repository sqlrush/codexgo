package filesystem

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// TestLocalFileSystemCoversSurfaceArea drives every operation through
// LocalFileSystem with a nil sandbox, mirroring the Rust
// `file_system_methods_cover_surface_area` integration test.
func TestLocalFileSystemCoversSurfaceArea(t *testing.T) {
	dir := t.TempDir()
	fsys := NewLocalFileSystem()
	ctx := context.Background()

	sourceDir := filepath.Join(dir, "source")
	nestedFile := filepath.Join(sourceDir, "nested.txt")
	copiedFile := filepath.Join(dir, "copy.txt")
	copiedDir := filepath.Join(dir, "copied-dir")

	if err := fsys.CreateDirectory(ctx, ap(sourceDir), CreateDirectoryOptions{Recursive: true}, nil); err != nil {
		t.Fatalf("CreateDirectory: %v", err)
	}
	if err := fsys.WriteFile(ctx, ap(nestedFile), []byte("hello from trait"), nil); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := fsys.ReadFile(ctx, ap(nestedFile), nil)
	if err != nil || string(got) != "hello from trait" {
		t.Fatalf("ReadFile = %q (err %v)", got, err)
	}

	meta, err := fsys.GetMetadata(ctx, ap(nestedFile), nil)
	if err != nil || !meta.IsFile {
		t.Fatalf("GetMetadata = %+v (err %v)", meta, err)
	}

	if err := fsys.Copy(ctx, ap(nestedFile), ap(copiedFile), CopyOptions{}, nil); err != nil {
		t.Fatalf("Copy file: %v", err)
	}
	if err := fsys.Copy(ctx, ap(sourceDir), ap(copiedDir), CopyOptions{Recursive: true}, nil); err != nil {
		t.Fatalf("Copy dir: %v", err)
	}

	entries, err := fsys.ReadDirectory(ctx, ap(sourceDir), nil)
	if err != nil || len(entries) != 1 || entries[0].FileName != "nested.txt" {
		t.Fatalf("ReadDirectory = %+v (err %v)", entries, err)
	}

	if err := fsys.Remove(ctx, ap(sourceDir), RemoveOptions{Recursive: true, Force: true}, nil); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, statErr := os.Stat(sourceDir); statErr == nil {
		t.Fatal("source dir should be removed")
	}
}

// TestSpecialPathNarrowing exercises the special-path branches of
// hasWriteNarrowingEntries (narrowsForSpecial, specialPathsShareTarget).
func TestSpecialPathNarrowing(t *testing.T) {
	tmpEntry := func(access protocol.FileSystemAccessMode) protocol.FileSystemSandboxEntry {
		return protocol.FileSystemSandboxEntry{
			Path:   protocol.NewFileSystemSpecialPath(protocol.FileSystemSpecialPath{Kind: protocol.FileSystemSpecialPathKindTmpdir}),
			Access: access,
		}
	}

	tests := []struct {
		name   string
		policy protocol.FileSystemSandboxPolicy
		want   bool // expected hasFullDiskWriteAccess
	}{
		{
			// A read-only tmpdir entry with no same-target write override
			// narrows the root-write grant.
			name:   "tmpdir read narrows root write",
			policy: restricted(rootEntry(protocol.FileSystemAccessModeWrite), tmpEntry(protocol.FileSystemAccessModeRead)),
			want:   false,
		},
		{
			// A same-target tmpdir write override cancels the narrowing.
			name: "tmpdir read with tmpdir write override stays full",
			policy: restricted(
				rootEntry(protocol.FileSystemAccessModeWrite),
				tmpEntry(protocol.FileSystemAccessModeRead),
				tmpEntry(protocol.FileSystemAccessModeWrite),
			),
			want: true,
		},
		{
			// Minimal special path never narrows.
			name: "minimal read does not narrow",
			policy: restricted(rootEntry(protocol.FileSystemAccessModeWrite), protocol.FileSystemSandboxEntry{
				Path:   protocol.NewFileSystemSpecialPath(protocol.FileSystemSpecialPath{Kind: protocol.FileSystemSpecialPathKindMinimal}),
				Access: protocol.FileSystemAccessModeRead,
			}),
			want: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasFullDiskWriteAccess(tc.policy); got != tc.want {
				t.Fatalf("hasFullDiskWriteAccess = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSpecialPathMatchesAbsolutePath(t *testing.T) {
	tests := []struct {
		name  string
		kind  protocol.FileSystemSpecialPathKind
		path  protocol.AbsolutePath
		match bool
	}{
		{"slash tmp matches /tmp", protocol.FileSystemSpecialPathKindSlashTmp, "/tmp", true},
		{"slash tmp rejects other", protocol.FileSystemSpecialPathKindSlashTmp, "/tmpx", false},
		{"minimal never matches", protocol.FileSystemSpecialPathKindMinimal, "/tmp", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			special := &protocol.FileSystemSpecialPath{Kind: tc.kind}
			if got := specialPathMatchesAbsolutePath(special, tc.path); got != tc.match {
				t.Fatalf("specialPathMatchesAbsolutePath = %v, want %v", got, tc.match)
			}
		})
	}
}

func strptr(s string) *string { return &s }

func TestSpecialPathsShareTarget(t *testing.T) {
	tests := []struct {
		name  string
		left  *protocol.FileSystemSpecialPath
		right *protocol.FileSystemSpecialPath
		share bool
	}{
		{
			"same root",
			&protocol.FileSystemSpecialPath{Kind: protocol.FileSystemSpecialPathKindRoot},
			&protocol.FileSystemSpecialPath{Kind: protocol.FileSystemSpecialPathKindRoot},
			true,
		},
		{
			"different kinds",
			&protocol.FileSystemSpecialPath{Kind: protocol.FileSystemSpecialPathKindRoot},
			&protocol.FileSystemSpecialPath{Kind: protocol.FileSystemSpecialPathKindTmpdir},
			false,
		},
		{
			"project roots equal subpath",
			ptrProjectRoots(strptr("src")),
			ptrProjectRoots(strptr("src")),
			true,
		},
		{
			"project roots differing subpath",
			ptrProjectRoots(strptr("src")),
			ptrProjectRoots(strptr("docs")),
			false,
		},
		{
			"project roots nil vs set subpath",
			ptrProjectRoots(nil),
			ptrProjectRoots(strptr("src")),
			false,
		},
		{
			"project roots both nil subpath",
			ptrProjectRoots(nil),
			ptrProjectRoots(nil),
			true,
		},
		{
			"unknown equal path and subpath",
			ptrUnknown("/x", strptr("a")),
			ptrUnknown("/x", strptr("a")),
			true,
		},
		{
			"unknown differing path",
			ptrUnknown("/x", nil),
			ptrUnknown("/y", nil),
			false,
		},
		{"nil left", nil, &protocol.FileSystemSpecialPath{Kind: protocol.FileSystemSpecialPathKindRoot}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := specialPathsShareTarget(tc.left, tc.right); got != tc.share {
				t.Fatalf("specialPathsShareTarget = %v, want %v", got, tc.share)
			}
		})
	}
}

func ptrProjectRoots(subpath *string) *protocol.FileSystemSpecialPath {
	v := protocol.NewProjectRootsSpecialPath(subpath)
	return &v
}

func ptrUnknown(path string, subpath *string) *protocol.FileSystemSpecialPath {
	v := protocol.NewUnknownSpecialPath(path, subpath)
	return &v
}

func TestFileSystemPathsShareTargetMixed(t *testing.T) {
	rootSpecial := protocol.NewFileSystemSpecialPath(protocol.FileSystemSpecialPath{Kind: protocol.FileSystemSpecialPathKindRoot})
	// Root special matches the filesystem root absolute path.
	rootAbs := protocol.NewFileSystemPath("/")
	if !fileSystemPathsShareTarget(rootSpecial, rootAbs) {
		t.Fatal("root special should share target with filesystem root path")
	}
	if !fileSystemPathsShareTarget(rootAbs, rootSpecial) {
		t.Fatal("filesystem root path should share target with root special (reversed)")
	}
	// A non-root absolute path does not match the root special.
	if fileSystemPathsShareTarget(rootSpecial, protocol.NewFileSystemPath("/etc")) {
		t.Fatal("root special should not match /etc")
	}
	// Glob vs non-glob never shares.
	glob := protocol.NewFileSystemGlobPattern("*.go")
	if fileSystemPathsShareTarget(glob, rootAbs) {
		t.Fatal("glob should not share target with a path")
	}
	if !fileSystemPathsShareTarget(glob, protocol.NewFileSystemGlobPattern("*.go")) {
		t.Fatal("identical globs should share target")
	}
}

func TestWrapTaskFailureUnwraps(t *testing.T) {
	base := errors.New("cancelled")
	wrapped := wrapTaskFailure(base)
	if !errors.Is(wrapped, base) {
		t.Fatal("wrapTaskFailure should wrap with %w")
	}
	if wrapped.Error() != "filesystem task failed: cancelled" {
		t.Fatalf("message = %q", wrapped.Error())
	}
}
