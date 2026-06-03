package pathutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// --- Symlink resolution -----------------------------------------------------

// TestResolveSymlinkWritePaths_Cycle ports symlink_cycles_fall_back_to_root_write_path.
func TestResolveSymlinkWritePaths_Cycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on Windows")
	}
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")

	if err := os.Symlink(b, a); err != nil {
		t.Fatalf("symlink a->b: %v", err)
	}
	if err := os.Symlink(a, b); err != nil {
		t.Fatalf("symlink b->a: %v", err)
	}

	resolved, err := ResolveSymlinkWritePaths(a)
	if err != nil {
		t.Fatalf("ResolveSymlinkWritePaths: %v", err)
	}
	if resolved.HasReadPath {
		t.Errorf("HasReadPath = true, want false (read path = %q)", resolved.ReadPath)
	}
	if resolved.WritePath != a {
		t.Errorf("WritePath = %q, want %q", resolved.WritePath, a)
	}
}

func TestResolveSymlinkWritePaths_RegularFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(file, []byte("hi"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	resolved, err := ResolveSymlinkWritePaths(file)
	if err != nil {
		t.Fatalf("ResolveSymlinkWritePaths: %v", err)
	}
	if !resolved.HasReadPath || resolved.ReadPath != file {
		t.Errorf("ReadPath = %q (has=%v), want %q", resolved.ReadPath, resolved.HasReadPath, file)
	}
	if resolved.WritePath != file {
		t.Errorf("WritePath = %q, want %q", resolved.WritePath, file)
	}
}

func TestResolveSymlinkWritePaths_MissingPathIsWriteTarget(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.txt")

	resolved, err := ResolveSymlinkWritePaths(missing)
	if err != nil {
		t.Fatalf("ResolveSymlinkWritePaths: %v", err)
	}
	if !resolved.HasReadPath || resolved.ReadPath != missing {
		t.Errorf("ReadPath = %q (has=%v), want %q", resolved.ReadPath, resolved.HasReadPath, missing)
	}
	if resolved.WritePath != missing {
		t.Errorf("WritePath = %q, want %q", resolved.WritePath, missing)
	}
}

func TestResolveSymlinkWritePaths_FollowsChainToFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on Windows")
	}
	dir := t.TempDir()
	real := filepath.Join(dir, "real.txt")
	if err := os.WriteFile(real, []byte("data"), 0o644); err != nil {
		t.Fatalf("write real: %v", err)
	}
	link1 := filepath.Join(dir, "link1")
	link2 := filepath.Join(dir, "link2")
	if err := os.Symlink(real, link1); err != nil {
		t.Fatalf("symlink link1: %v", err)
	}
	// Relative target to exercise resolve-against-parent.
	if err := os.Symlink("link1", link2); err != nil {
		t.Fatalf("symlink link2: %v", err)
	}

	resolved, err := ResolveSymlinkWritePaths(link2)
	if err != nil {
		t.Fatalf("ResolveSymlinkWritePaths: %v", err)
	}
	if !resolved.HasReadPath || resolved.ReadPath != real {
		t.Errorf("ReadPath = %q (has=%v), want %q", resolved.ReadPath, resolved.HasReadPath, real)
	}
	if resolved.WritePath != real {
		t.Errorf("WritePath = %q, want %q", resolved.WritePath, real)
	}
}

// --- WSL normalization ------------------------------------------------------

// TestNormalizeForWSLWithFlag ports the wsl test module. The case-folding logic
// only activates on Linux, so the lowercasing expectation is Linux-specific;
// everywhere else the path is returned unchanged.
func TestNormalizeForWSLWithFlag(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		isWSL     bool
		wantLinux string // expected output on Linux
	}{
		{
			name:      "mnt drive path lowercased",
			path:      "/mnt/C/Users/Dev",
			isWSL:     true,
			wantLinux: "/mnt/c/users/dev",
		},
		{
			name:      "non drive path unchanged",
			path:      "/mnt/cc/Users/Dev",
			isWSL:     true,
			wantLinux: "/mnt/cc/Users/Dev",
		},
		{
			name:      "non mnt path unchanged",
			path:      "/home/Dev",
			isWSL:     true,
			wantLinux: "/home/Dev",
		},
		{
			name:      "not wsl is unchanged",
			path:      "/mnt/C/Users/Dev",
			isWSL:     false,
			wantLinux: "/mnt/C/Users/Dev",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeForWSLWithFlag(tt.path, tt.isWSL)
			want := tt.path
			if runtime.GOOS == "linux" {
				want = tt.wantLinux
			}
			if got != want {
				t.Errorf("normalizeForWSLWithFlag(%q, %v) = %q, want %q", tt.path, tt.isWSL, got, want)
			}
		})
	}
}

// --- Native workdir normalization -------------------------------------------

// TestNormalizeForNativeWorkdirWithFlag ports the native_workdir test module.
func TestNormalizeForNativeWorkdirWithFlag(t *testing.T) {
	verbatim := `\\?\D:\c\x\worktrees\2508\swift-base`

	t.Run("windows verbatim drive simplified", func(t *testing.T) {
		got := normalizeForNativeWorkdirWithFlag(verbatim, true)
		want := `D:\c\x\worktrees\2508\swift-base`
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("windows verbatim UNC simplified", func(t *testing.T) {
		got := normalizeForNativeWorkdirWithFlag(`\\?\UNC\server\share\ws`, true)
		want := `\\server\share\ws`
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("non windows unchanged", func(t *testing.T) {
		got := normalizeForNativeWorkdirWithFlag(verbatim, false)
		if got != verbatim {
			t.Errorf("got %q, want %q", got, verbatim)
		}
	})
}

// --- Path comparison --------------------------------------------------------

// TestPathsMatchAfterNormalization ports the path_comparison test module.
func TestPathsMatchAfterNormalization(t *testing.T) {
	dir := t.TempDir()

	t.Run("matches identical existing paths", func(t *testing.T) {
		if !PathsMatchAfterNormalization(dir, dir) {
			t.Errorf("expected identical existing paths to match")
		}
	})

	t.Run("falls back to raw equality for missing equal paths", func(t *testing.T) {
		if !PathsMatchAfterNormalization("missing", "missing") {
			t.Errorf("expected identical missing paths to match via raw equality")
		}
	})

	t.Run("falls back to raw inequality for missing distinct paths", func(t *testing.T) {
		if PathsMatchAfterNormalization("missing-a", "missing-b") {
			t.Errorf("expected distinct missing paths not to match")
		}
	})
}

func TestNormalizeForPathComparison_MissingPathErrors(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing")
	if _, err := NormalizeForPathComparison(missing); err == nil {
		t.Errorf("expected error for missing path")
	}
}

// --- Atomic write -----------------------------------------------------------

func TestWriteAtomically(t *testing.T) {
	dir := t.TempDir()

	t.Run("creates nested directories and writes contents", func(t *testing.T) {
		target := filepath.Join(dir, "nested", "deep", "out.txt")
		if err := WriteAtomically(target, "hello world"); err != nil {
			t.Fatalf("WriteAtomically: %v", err)
		}
		got, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("read back: %v", err)
		}
		if string(got) != "hello world" {
			t.Errorf("contents = %q, want %q", string(got), "hello world")
		}
	})

	t.Run("overwrites existing file", func(t *testing.T) {
		target := filepath.Join(dir, "over.txt")
		if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
			t.Fatalf("seed file: %v", err)
		}
		if err := WriteAtomically(target, "new"); err != nil {
			t.Fatalf("WriteAtomically: %v", err)
		}
		got, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("read back: %v", err)
		}
		if string(got) != "new" {
			t.Errorf("contents = %q, want %q", string(got), "new")
		}
	})

	t.Run("leaves no temp files behind", func(t *testing.T) {
		sub := t.TempDir()
		target := filepath.Join(sub, "clean.txt")
		if err := WriteAtomically(target, "x"); err != nil {
			t.Fatalf("WriteAtomically: %v", err)
		}
		entries, err := os.ReadDir(sub)
		if err != nil {
			t.Fatalf("read dir: %v", err)
		}
		if len(entries) != 1 || entries[0].Name() != "clean.txt" {
			names := make([]string, len(entries))
			for i, e := range entries {
				names[i] = e.Name()
			}
			t.Errorf("directory entries = %v, want only [clean.txt]", names)
		}
	})
}

// --- Absolutize / normalize core --------------------------------------------

func TestNormalizeUnixPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix path normalization")
	}
	tests := []struct {
		in   string
		want string
	}{
		{"/path/to/123/456", "/path/to/123/456"},
		{"/path/to/./123/../456", "/path/to/456"},
		{"/a//b/./c", "/a/b/c"},
		{"/../../path", "/path"},
		{"./nested/../file.txt", "file.txt"},
		{"", "."},
		{".", "."},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := normalizePath(tt.in); got != tt.want {
				t.Errorf("normalizePath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestResolvePathAgainstBase(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix base resolution")
	}
	tests := []struct {
		name string
		path string
		base string
		want string
	}{
		{"absolute ignores base", "/abs/file.txt", "/base", "/abs/file.txt"},
		{"relative joins base", "file.txt", "/base", "/base/file.txt"},
		{"dots normalized against base", "./nested/../file.txt", "/base", "/base/file.txt"},
		{"parent dir uses base parent", "../path/to", "/base/cwd", "/base/path/to"},
		{"parent above root stays at root", "../../path/to", "/", "/path/to"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolvePathAgainstBase(tt.path, tt.base); got != tt.want {
				t.Errorf("resolvePathAgainstBase(%q, %q) = %q, want %q", tt.path, tt.base, got, tt.want)
			}
		})
	}
}

func TestNormalizeWindowsDevicePath(t *testing.T) {
	tests := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{`\\?\D:\c\x\worktrees\2508\swift-base`, `D:\c\x\worktrees\2508\swift-base`, true},
		{`\\.\D:\c\x\worktrees\2508\swift-base`, `D:\c\x\worktrees\2508\swift-base`, true},
		{`\\?\UNC\server\share\workspace`, `\\server\share\workspace`, true},
		{`\\.\UNC\server\share\workspace`, `\\server\share\workspace`, true},
		{`\\?\GLOBALROOT\Device`, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, ok := normalizeWindowsDevicePath(tt.in)
			if ok != tt.wantOK || got != tt.want {
				t.Errorf("normalizeWindowsDevicePath(%q) = (%q, %v), want (%q, %v)",
					tt.in, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestMaybeExpandHomeDirectory(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home directory available")
	}
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"tilde alone", "~", home},
		{"tilde slash subpath", "~/code", filepath.Join(home, "code")},
		{"tilde double slash subpath", "~//code", filepath.Join(home, "code")},
		{"no tilde unchanged", "/abs/path", "/abs/path"},
		{"tilde-prefixed name unchanged", "~user/x", "~user/x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := maybeExpandHomeDirectory(tt.in); got != tt.want {
				t.Errorf("maybeExpandHomeDirectory(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestIsWSL_NonLinux verifies that the live detector reports false off Linux.
func TestIsWSL_NonLinux(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("behavior is environment-dependent on Linux")
	}
	if IsWSL() {
		t.Errorf("IsWSL() = true on %s, want false", runtime.GOOS)
	}
}
