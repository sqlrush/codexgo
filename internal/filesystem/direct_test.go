package filesystem

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

func ap(path string) protocol.AbsolutePath { return protocol.AbsolutePath(path) }

func TestDirectReadWriteRoundTrip(t *testing.T) {
	dir := t.TempDir()
	fsys := NewDirectFileSystem()
	ctx := context.Background()

	target := filepath.Join(dir, "note.txt")
	want := []byte("hello from trait")
	if err := fsys.WriteFile(ctx, ap(target), want, nil); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := fsys.ReadFile(ctx, ap(target), nil)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("ReadFile = %q, want %q", got, want)
	}
}

func TestReadFileText(t *testing.T) {
	dir := t.TempDir()
	fsys := NewDirectFileSystem()
	ctx := context.Background()
	target := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(target, []byte("hello text"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFileText(ctx, fsys, ap(target), nil)
	if err != nil {
		t.Fatalf("ReadFileText: %v", err)
	}
	if got != "hello text" {
		t.Fatalf("ReadFileText = %q", got)
	}

	// Invalid UTF-8 is rejected.
	bad := filepath.Join(dir, "bad.bin")
	if err := os.WriteFile(bad, []byte{0xff, 0xfe, 0xfd}, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFileText(ctx, fsys, ap(bad), nil); err == nil {
		t.Fatal("ReadFileText accepted invalid UTF-8")
	}
}

func TestWriteFileMissingParentIsNotFound(t *testing.T) {
	dir := t.TempDir()
	fsys := NewDirectFileSystem()
	ctx := context.Background()
	missing := filepath.Join(dir, "missing", "note.txt")
	err := fsys.WriteFile(ctx, ap(missing), []byte("x"), nil)
	if err == nil {
		t.Fatal("expected error writing to missing parent")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected NotExist, got %v", err)
	}
	if _, statErr := os.Stat(missing); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatal("missing parent should not have been created")
	}
}

func TestCreateDirectory(t *testing.T) {
	dir := t.TempDir()
	fsys := NewDirectFileSystem()
	ctx := context.Background()

	// Non-recursive fails when the parent is absent.
	nested := filepath.Join(dir, "a", "b")
	if err := fsys.CreateDirectory(ctx, ap(nested), CreateDirectoryOptions{Recursive: false}, nil); err == nil {
		t.Fatal("non-recursive create with missing parent should fail")
	}

	// Recursive succeeds.
	if err := fsys.CreateDirectory(ctx, ap(nested), CreateDirectoryOptions{Recursive: true}, nil); err != nil {
		t.Fatalf("recursive CreateDirectory: %v", err)
	}
	info, err := os.Stat(nested)
	if err != nil || !info.IsDir() {
		t.Fatalf("expected directory at %s: %v", nested, err)
	}
}

func TestGetMetadataFollowsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require privileges on Windows")
	}
	dir := t.TempDir()
	fsys := NewDirectFileSystem()
	ctx := context.Background()

	file := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(file, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}

	meta, err := fsys.GetMetadata(ctx, ap(file), nil)
	if err != nil {
		t.Fatalf("GetMetadata file: %v", err)
	}
	if meta.IsDirectory || !meta.IsFile || meta.IsSymlink {
		t.Fatalf("file metadata mismatch: %+v", meta)
	}

	linkMeta, err := fsys.GetMetadata(ctx, ap(link), nil)
	if err != nil {
		t.Fatalf("GetMetadata link: %v", err)
	}
	// Follows the link for type, but reports is_symlink for the path itself.
	if linkMeta.IsDirectory || !linkMeta.IsFile || !linkMeta.IsSymlink {
		t.Fatalf("symlink metadata mismatch: %+v", linkMeta)
	}

	dirLink := filepath.Join(dir, "dirlink")
	if err := os.Symlink(dir, dirLink); err != nil {
		t.Fatal(err)
	}
	dirLinkMeta, err := fsys.GetMetadata(ctx, ap(dirLink), nil)
	if err != nil {
		t.Fatalf("GetMetadata dirlink: %v", err)
	}
	if !dirLinkMeta.IsDirectory || dirLinkMeta.IsFile || !dirLinkMeta.IsSymlink {
		t.Fatalf("dir symlink metadata mismatch: %+v", dirLinkMeta)
	}
}

func TestGetMetadataModifiedAtPositive(t *testing.T) {
	dir := t.TempDir()
	fsys := NewDirectFileSystem()
	ctx := context.Background()
	file := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(file, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	meta, err := fsys.GetMetadata(ctx, ap(file), nil)
	if err != nil {
		t.Fatal(err)
	}
	if meta.ModifiedAtMs <= 0 {
		t.Fatalf("expected positive modified time, got %d", meta.ModifiedAtMs)
	}
}

func TestReadDirectory(t *testing.T) {
	dir := t.TempDir()
	fsys := NewDirectFileSystem()
	ctx := context.Background()

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	entries, err := fsys.ReadDirectory(ctx, ap(dir), nil)
	if err != nil {
		t.Fatalf("ReadDirectory: %v", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].FileName < entries[j].FileName })
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(entries), entries)
	}
	if entries[0].FileName != "a.txt" || !entries[0].IsFile || entries[0].IsDirectory {
		t.Fatalf("entry 0 mismatch: %+v", entries[0])
	}
	if entries[1].FileName != "sub" || !entries[1].IsDirectory || entries[1].IsFile {
		t.Fatalf("entry 1 mismatch: %+v", entries[1])
	}
}

func TestRemove(t *testing.T) {
	fsys := NewDirectFileSystem()
	ctx := context.Background()

	t.Run("file", func(t *testing.T) {
		dir := t.TempDir()
		file := filepath.Join(dir, "f.txt")
		if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := fsys.Remove(ctx, ap(file), RemoveOptions{}, nil); err != nil {
			t.Fatalf("Remove file: %v", err)
		}
		if _, err := os.Stat(file); !errors.Is(err, fs.ErrNotExist) {
			t.Fatal("file should be gone")
		}
	})

	t.Run("dir recursive", func(t *testing.T) {
		dir := t.TempDir()
		nested := filepath.Join(dir, "d", "e")
		if err := os.MkdirAll(nested, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := fsys.Remove(ctx, ap(filepath.Join(dir, "d")), RemoveOptions{Recursive: true}, nil); err != nil {
			t.Fatalf("Remove dir recursive: %v", err)
		}
	})

	t.Run("dir non-recursive non-empty fails", func(t *testing.T) {
		dir := t.TempDir()
		sub := filepath.Join(dir, "d")
		if err := os.MkdirAll(filepath.Join(sub, "child"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := fsys.Remove(ctx, ap(sub), RemoveOptions{Recursive: false}, nil); err == nil {
			t.Fatal("expected error removing non-empty dir without recursive")
		}
	})

	t.Run("missing with force", func(t *testing.T) {
		dir := t.TempDir()
		if err := fsys.Remove(ctx, ap(filepath.Join(dir, "nope")), RemoveOptions{Force: true}, nil); err != nil {
			t.Fatalf("Remove missing with force should succeed: %v", err)
		}
	})

	t.Run("missing without force fails", func(t *testing.T) {
		dir := t.TempDir()
		err := fsys.Remove(ctx, ap(filepath.Join(dir, "nope")), RemoveOptions{Force: false}, nil)
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("expected NotExist, got %v", err)
		}
	})
}

func TestRejectSandboxContextOnDirect(t *testing.T) {
	fsys := NewDirectFileSystem()
	ctx := context.Background()
	sandbox := FromPermissionProfile(protocol.DefaultPermissionProfile())
	_, err := fsys.ReadFile(ctx, ap("/tmp/x"), &sandbox)
	if err == nil {
		t.Fatal("expected rejection of sandbox context on DirectFileSystem")
	}
	var invalid *InvalidInputError
	if !errors.As(err, &invalid) {
		t.Fatalf("expected InvalidInputError, got %T: %v", err, err)
	}
}

func TestContextCancellation(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	fsys := NewDirectFileSystem()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fsys.ReadFile(ctx, ap(file), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
