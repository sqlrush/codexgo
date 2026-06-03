package filesystem

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	fsys := NewDirectFileSystem()
	ctx := context.Background()

	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	if err := os.WriteFile(src, []byte("hello from trait"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := fsys.Copy(ctx, ap(src), ap(dst), CopyOptions{}, nil); err != nil {
		t.Fatalf("Copy file: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "hello from trait" {
		t.Fatalf("copied contents = %q (err %v)", got, err)
	}
}

func TestCopyDirectoryRequiresRecursive(t *testing.T) {
	dir := t.TempDir()
	fsys := NewDirectFileSystem()
	ctx := context.Background()

	src := filepath.Join(dir, "source")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	err := fsys.Copy(ctx, ap(src), ap(filepath.Join(dir, "dest")), CopyOptions{Recursive: false}, nil)
	if err == nil {
		t.Fatal("copy of directory without recursive should fail")
	}
	var invalid *InvalidInputError
	if !errors.As(err, &invalid) {
		t.Fatalf("expected InvalidInputError, got %T", err)
	}
	if err.Error() != "fs/copy requires recursive: true when sourcePath is a directory" {
		t.Fatalf("unexpected message: %q", err.Error())
	}
}

func TestCopyDirectoryRecursive(t *testing.T) {
	dir := t.TempDir()
	fsys := NewDirectFileSystem()
	ctx := context.Background()

	src := filepath.Join(dir, "source")
	if err := os.MkdirAll(filepath.Join(src, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "nested", "f.txt"), []byte("deep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "top.txt"), []byte("top"), 0o600); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "dest")
	if err := fsys.Copy(ctx, ap(src), ap(dst), CopyOptions{Recursive: true}, nil); err != nil {
		t.Fatalf("Copy dir recursive: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dst, "nested", "f.txt"))
	if err != nil || string(got) != "deep" {
		t.Fatalf("nested file = %q (err %v)", got, err)
	}
	got, err = os.ReadFile(filepath.Join(dst, "top.txt"))
	if err != nil || string(got) != "top" {
		t.Fatalf("top file = %q (err %v)", got, err)
	}
}

func TestCopyRejectsDescendantDestination(t *testing.T) {
	dir := t.TempDir()
	fsys := NewDirectFileSystem()
	ctx := context.Background()

	src := filepath.Join(dir, "source")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(src, "inner")
	err := fsys.Copy(ctx, ap(src), ap(dst), CopyOptions{Recursive: true}, nil)
	if err == nil {
		t.Fatal("copy into descendant should fail")
	}
	var invalid *InvalidInputError
	if !errors.As(err, &invalid) {
		t.Fatalf("expected InvalidInputError, got %T", err)
	}
	if err.Error() != "fs/copy cannot copy a directory to itself or one of its descendants" {
		t.Fatalf("unexpected message: %q", err.Error())
	}
}

func TestCopySymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require privileges on Windows")
	}
	dir := t.TempDir()
	fsys := NewDirectFileSystem()
	ctx := context.Background()

	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("t"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "link-copy")
	if err := fsys.Copy(ctx, ap(link), ap(dst), CopyOptions{}, nil); err != nil {
		t.Fatalf("Copy symlink: %v", err)
	}
	got, err := os.Readlink(dst)
	if err != nil {
		t.Fatalf("Readlink copy: %v", err)
	}
	if got != target {
		t.Fatalf("symlink target = %q, want %q", got, target)
	}
}

func TestResolveExistingPath(t *testing.T) {
	dir := t.TempDir()
	// dir exists; appended non-existent suffix must be preserved.
	resolved, err := resolveExistingPath(filepath.Join(dir, "missing", "leaf.txt"))
	if err != nil {
		t.Fatalf("resolveExistingPath: %v", err)
	}
	base, err := resolveExistingPath(dir)
	if err != nil {
		t.Fatalf("resolveExistingPath base: %v", err)
	}
	want := filepath.Join(base, "missing", "leaf.txt")
	if resolved != want {
		t.Fatalf("resolveExistingPath = %q, want %q", resolved, want)
	}
}

func TestPathStartsWith(t *testing.T) {
	sep := string(filepath.Separator)
	root := sep + "a" + sep + "b"
	tests := []struct {
		name  string
		child string
		want  bool
	}{
		{"same", root, true},
		{"descendant", root + sep + "c", true},
		{"sibling prefix not descendant", sep + "a" + sep + "bc", false},
		{"parent not descendant", sep + "a", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := pathStartsWith(tc.child, root); got != tc.want {
				t.Fatalf("pathStartsWith(%q, %q) = %v, want %v", tc.child, root, got, tc.want)
			}
		})
	}
}
