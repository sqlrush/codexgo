package applypatch

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"unicode/utf8"
)

// FileMetadata describes a filesystem entry, mirroring the fields of Codex's
// exec-server metadata that the apply logic consults.
type FileMetadata struct {
	IsFile      bool
	IsDirectory bool
	IsSymlink   bool
}

// FileSystem abstracts the filesystem operations needed to apply a patch. It is
// the Go analogue of Codex's `ExecutorFileSystem` trait, minus the sandbox
// parameter (sandboxing is part of the exec-server infrastructure and is a
// follow-on; see the package doc comment).
//
// Implementations must report not-found conditions with errors for which
// errors.Is(err, fs.ErrNotExist) is true, because the apply logic distinguishes
// "missing" from other failures to match Codex's delta-exactness semantics.
type FileSystem interface {
	// ReadFileText reads the entire file at path as a UTF-8 string.
	ReadFileText(path string) (string, error)
	// WriteFile writes contents to path, creating or truncating it.
	WriteFile(path string, contents []byte) error
	// Remove deletes the file at path (non-recursive, non-forced).
	Remove(path string) error
	// Metadata returns metadata for path. Symlinks are reported without
	// following them (lstat semantics).
	Metadata(path string) (FileMetadata, error)
	// CreateDirAll creates path and any missing parents.
	CreateDirAll(path string) error
}

// OSFileSystem is the default [FileSystem] backed by the os package. It mirrors
// Codex's local filesystem (`LOCAL_FS`).
type OSFileSystem struct{}

// ReadFileText implements [FileSystem]. Mirroring Rust's `read_file_text`
// (which converts the bytes into a `String`), it requires the file to be valid
// UTF-8 and returns an error otherwise. The not-found behaviour is preserved so
// callers can distinguish missing files via [errors.Is].
func (OSFileSystem) ReadFileText(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("read file %s: invalid UTF-8 contents", path)
	}
	return string(data), nil
}

// WriteFile implements [FileSystem].
func (OSFileSystem) WriteFile(path string, contents []byte) error {
	return os.WriteFile(path, contents, 0o644)
}

// Remove implements [FileSystem]. It mirrors a non-recursive, non-forced remove:
// it refuses to remove directories and surfaces not-found errors.
func (OSFileSystem) Remove(path string) error {
	return os.Remove(path)
}

// Metadata implements [FileSystem] using lstat so symlinks are not followed.
func (OSFileSystem) Metadata(path string) (FileMetadata, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return FileMetadata{}, err
	}
	mode := info.Mode()
	return FileMetadata{
		IsFile:      mode.IsRegular(),
		IsDirectory: mode.IsDir(),
		IsSymlink:   mode&fs.ModeSymlink != 0,
	}, nil
}

// CreateDirAll implements [FileSystem].
func (OSFileSystem) CreateDirAll(path string) error {
	return os.MkdirAll(path, 0o755)
}

// isNotFound reports whether err represents a not-found condition, mirroring the
// `io::ErrorKind::NotFound` checks in Codex.
func isNotFound(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}

// parentDir returns the parent directory of an absolute path, or ("", false) if
// it has no parent.
func parentDir(path string) (string, bool) {
	dir := filepath.Dir(path)
	if dir == path || dir == "" {
		return "", false
	}
	return dir, true
}
