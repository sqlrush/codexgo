package abspath

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// skipOnWindows skips tests whose expectations encode Unix path grammar. The
// reference crate gates the equivalent tests behind #[cfg(unix)].
func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Unix path-grammar test")
	}
}

func TestResolvePathAgainstBase(t *testing.T) {
	skipOnWindows(t)
	tests := []struct {
		name string
		path string
		base string
		want string
	}{
		{"absolute ignores base", "/abs/file.txt", "/base", "/abs/file.txt"},
		{"relative uses base", "file.txt", "/base", "/base/file.txt"},
		{"dot segments normalized", "./nested/../file.txt", "/base", "/base/file.txt"},
		{"absolute dots removed", "/path/to/./123/../456", "/base", "/path/to/456"},
		{"relative parent uses base parent", "../path/to/x", "/base/cwd", "/base/path/to/x"},
		{"parent above root stays at root", "../../path/to/x", "/", "/path/to/x"},
		{"empty path uses base", "", "/base/cwd", "/base/cwd"},
		{"relative current dir uses base", "./path/to/x", "/base", "/base/path/to/x"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolvePathAgainstBase(tc.path, tc.base)
			if got.Path() != tc.want {
				t.Errorf("ResolvePathAgainstBase(%q, %q) = %q, want %q", tc.path, tc.base, got.Path(), tc.want)
			}
		})
	}
}

func TestNormalizePathLexical(t *testing.T) {
	skipOnWindows(t)
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain absolute unchanged", "/path/to/123/456", "/path/to/123/456"},
		{"trailing dot", "/a/b/.", "/a/b"},
		{"double slash collapsed", "/a//b", "/a/b"},
		{"only dots collapses to root", "/..", "/"},
		{"empty becomes dot", "", "."},
		{"relative only dots becomes dot", "a/..", "."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizePath(tc.in); got != tc.want {
				t.Errorf("normalizePath(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestFromAbsolutePathChecked(t *testing.T) {
	skipOnWindows(t)
	t.Run("rejects relative", func(t *testing.T) {
		_, err := FromAbsolutePathChecked("relative/path")
		if !errors.Is(err, ErrNotAbsolute) {
			t.Fatalf("expected ErrNotAbsolute, got %v", err)
		}
	})
	t.Run("accepts absolute and normalizes", func(t *testing.T) {
		got, err := FromAbsolutePathChecked("/tmp/codex/../codex-home/plugins/cache")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := "/tmp/codex-home/plugins/cache"; got.Path() != want {
			t.Errorf("got %q, want %q", got.Path(), want)
		}
	})
}

func TestAncestors(t *testing.T) {
	skipOnWindows(t)
	buf, err := FromAbsolutePathChecked("/tmp/one/two")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"/tmp/one/two", "/tmp/one", "/tmp", "/"}
	got := buf.Ancestors()
	if len(got) != len(want) {
		t.Fatalf("got %d ancestors %v, want %d %v", len(got), pathsOf(got), len(want), want)
	}
	for i := range want {
		if got[i].Path() != want[i] {
			t.Errorf("ancestor[%d] = %q, want %q", i, got[i].Path(), want[i])
		}
	}
}

func TestParent(t *testing.T) {
	skipOnWindows(t)
	tests := []struct {
		name   string
		in     string
		want   string
		hasPar bool
	}{
		{"normal parent", "/a/b/c", "/a/b", true},
		{"single level parent is root", "/a", "/", true},
		{"root has no parent", "/", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf, err := FromAbsolutePathChecked(tc.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			par, ok := buf.Parent()
			if ok != tc.hasPar {
				t.Fatalf("Parent() ok = %v, want %v", ok, tc.hasPar)
			}
			if ok && par.Path() != tc.want {
				t.Errorf("Parent() = %q, want %q", par.Path(), tc.want)
			}
		})
	}
}

func TestHomeExpansion(t *testing.T) {
	skipOnWindows(t)
	home, ok := homeDir()
	if !ok {
		t.Skip("no home directory available")
	}
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"tilde only", "~", home},
		{"tilde subpath", "~/code", filepath.Join(home, "code")},
		{"tilde double slash", "~//code", filepath.Join(home, "code")},
		{"tilde not expanded for user form", "~other/x", "~other/x"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := maybeExpandHomeDirectory(tc.in); got != tc.want {
				t.Errorf("maybeExpandHomeDirectory(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeWindowsDevicePath(t *testing.T) {
	// Pure-string logic: testable on any host.
	tests := []struct {
		name   string
		in     string
		want   string
		wantOk bool
	}{
		{"verbatim drive", `\\?\D:\c\x\worktrees\2508\swift-base`, `D:\c\x\worktrees\2508\swift-base`, true},
		{"device drive", `\\.\D:\c\x\worktrees\2508\swift-base`, `D:\c\x\worktrees\2508\swift-base`, true},
		{"verbatim UNC", `\\?\UNC\server\share\workspace`, `\\server\share\workspace`, true},
		{"device UNC", `\\.\UNC\server\share\workspace`, `\\server\share\workspace`, true},
		{"globalroot unsupported", `\\?\GLOBALROOT\Device`, "", false},
		{"plain path unsupported", `D:\already\friendly`, "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := normalizeWindowsDevicePath(tc.in)
			if ok != tc.wantOk || got != tc.want {
				t.Errorf("normalizeWindowsDevicePath(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.wantOk)
			}
		})
	}
}

func TestIsWindowsDriveAbsolutePath(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{`C:\x`, true},
		{`c:/x`, true},
		{`C:x`, false},
		{`C:`, false},
		{`1:\x`, false},
		{``, false},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			if got := isWindowsDriveAbsolutePath(tc.in); got != tc.want {
				t.Errorf("isWindowsDriveAbsolutePath(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestJSONRoundTrip(t *testing.T) {
	skipOnWindows(t)
	buf, err := FromAbsolutePathChecked("/tmp/one/two")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := buf.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if want := `"/tmp/one/two"`; string(data) != want {
		t.Fatalf("MarshalJSON = %s, want %s", data, want)
	}
	var got AbsolutePathBuf
	if err := got.UnmarshalJSON(data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Path() != buf.Path() {
		t.Errorf("round trip = %q, want %q", got.Path(), buf.Path())
	}
}

func TestUnmarshalJSONRejectsRelativeWithoutBase(t *testing.T) {
	skipOnWindows(t)
	var got AbsolutePathBuf
	err := got.UnmarshalJSON([]byte(`"subdir/file.txt"`))
	if !errors.Is(err, ErrNoBasePath) {
		t.Fatalf("expected ErrNoBasePath, got %v", err)
	}
}

func TestUnmarshalWithBase(t *testing.T) {
	skipOnWindows(t)
	got, err := Unmarshal([]byte(`"subdir/file.txt"`), "/base")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "/base/subdir/file.txt"; got.Path() != want {
		t.Errorf("Unmarshal = %q, want %q", got.Path(), want)
	}
}

func TestDecoder(t *testing.T) {
	skipOnWindows(t)
	dec := NewDecoder("/base")
	got, err := dec.Decode([]byte(`"./a/../b"`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "/base/b"; got.Path() != want {
		t.Errorf("Decoder.Decode = %q, want %q", got.Path(), want)
	}
}

func TestJoin(t *testing.T) {
	skipOnWindows(t)
	buf, err := FromAbsolutePathChecked("/base/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := buf.Join("../sibling/./file.txt")
	if want := "/base/sibling/file.txt"; got.Path() != want {
		t.Errorf("Join = %q, want %q", got.Path(), want)
	}
	// Joining an absolute path ignores the receiver.
	if got := buf.Join("/elsewhere"); got.Path() != "/elsewhere" {
		t.Errorf("Join(absolute) = %q, want %q", got.Path(), "/elsewhere")
	}
}

func TestCanonicalizeResolvesSymlinkFreeExistingPath(t *testing.T) {
	dir := t.TempDir()
	two := filepath.Join(dir, "two")
	if err := os.MkdirAll(two, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	file := filepath.Join(two, "file.txt")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf, err := FromAbsolutePath(filepath.Join(dir, "one", "..", "two", ".", "file.txt"))
	if err != nil {
		t.Fatalf("from absolute: %v", err)
	}
	got, err := buf.Canonicalize()
	if err != nil {
		t.Fatalf("canonicalize: %v", err)
	}
	want, err := canonicalize(file)
	if err != nil {
		t.Fatalf("expected canonicalize: %v", err)
	}
	if got.Path() != want {
		t.Errorf("Canonicalize = %q, want %q", got.Path(), want)
	}
}

func TestCanonicalizeMissingPathErrors(t *testing.T) {
	dir := t.TempDir()
	buf, err := FromAbsolutePath(filepath.Join(dir, "missing.txt"))
	if err != nil {
		t.Fatalf("from absolute: %v", err)
	}
	if _, err := buf.Canonicalize(); err == nil {
		t.Fatal("expected canonicalize to fail for missing path")
	}
}

func TestCanonicalizePreservingSymlinksKeepsLogicalPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	dir := t.TempDir()
	// Make the temp dir nested enough that the symlink is not a top-level alias.
	nested := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	real := filepath.Join(nested, "real")
	link := filepath.Join(nested, "link")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatalf("mkdir real: %v", err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	got, err := CanonicalizePreservingSymlinks(link)
	if err != nil {
		t.Fatalf("canonicalize preserving: %v", err)
	}
	// The logical path is what FromAbsolutePath produces for the link.
	logical, err := FromAbsolutePath(link)
	if err != nil {
		t.Fatalf("from absolute: %v", err)
	}
	if got != logical.Path() {
		t.Errorf("CanonicalizePreservingSymlinks = %q, want logical %q", got, logical.Path())
	}
}

func TestCanonicalizePreservingSymlinksFallsBackForMissing(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing")
	got, err := CanonicalizePreservingSymlinks(missing)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	logical, err := FromAbsolutePath(missing)
	if err != nil {
		t.Fatalf("from absolute: %v", err)
	}
	if got != logical.Path() {
		t.Errorf("got %q, want logical %q", got, logical.Path())
	}
}

func TestCanonicalizeExistingPreservingSymlinksErrorsForMissing(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing")
	if _, err := CanonicalizeExistingPreservingSymlinks(missing); err == nil {
		t.Fatal("expected error for missing path")
	}
}

func TestRelativeToCurrentDir(t *testing.T) {
	skipOnWindows(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	got, err := RelativeToCurrentDir("file.txt")
	if err != nil {
		t.Fatalf("relative to current dir: %v", err)
	}
	if want := filepath.Join(cwd, "file.txt"); got.Path() != want {
		t.Errorf("RelativeToCurrentDir = %q, want %q", got.Path(), want)
	}
}

func TestStringMatchesPath(t *testing.T) {
	skipOnWindows(t)
	buf, err := FromAbsolutePathChecked("/a/b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if buf.String() != buf.Path() {
		t.Errorf("String() = %q, Path() = %q; want equal", buf.String(), buf.Path())
	}
	if buf.String() != "/a/b" {
		t.Errorf("String() = %q, want %q", buf.String(), "/a/b")
	}
}

func TestCurrentDir(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	got, err := CurrentDir()
	if err != nil {
		t.Fatalf("current dir: %v", err)
	}
	want, err := FromAbsolutePath(cwd)
	if err != nil {
		t.Fatalf("from absolute: %v", err)
	}
	if got.Path() != want.Path() {
		t.Errorf("CurrentDir = %q, want %q", got.Path(), want.Path())
	}
}

func TestFromAbsolutePathResolvesRelativeAgainstCwd(t *testing.T) {
	skipOnWindows(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	got, err := FromAbsolutePath("sub/./file.txt")
	if err != nil {
		t.Fatalf("from absolute: %v", err)
	}
	if want := filepath.Join(cwd, "sub", "file.txt"); got.Path() != want {
		t.Errorf("FromAbsolutePath = %q, want %q", got.Path(), want)
	}
}

func pathsOf(bufs []AbsolutePathBuf) []string {
	out := make([]string, len(bufs))
	for i, b := range bufs {
		out[i] = b.Path()
	}
	return out
}
