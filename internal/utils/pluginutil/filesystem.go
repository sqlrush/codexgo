package pluginutil

import (
	"context"
	"fmt"
	"os"
	"unicode/utf8"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// FileMetadata mirrors the fields of the Rust `codex_file_system::FileMetadata`
// that this package depends on. Only the subset needed for plugin manifest
// discovery is modeled; the full Rust struct additionally carries timestamps and
// directory/symlink flags.
type FileMetadata struct {
	// IsDirectory reports whether the path is a directory.
	IsDirectory bool
	// IsFile reports whether the path is a regular file.
	IsFile bool
	// IsSymlink reports whether the path is a symbolic link.
	IsSymlink bool
}

// FileSystem is a minimal hand-rolled equivalent of the Rust
// `codex_exec_server::ExecutorFileSystem` trait, restricted to the methods used
// by this package.
//
// Keeping the interface small (it is consumed here, not implemented here) lets
// callers inject local, sandboxed, or remote filesystems without this package
// depending on a heavyweight executor abstraction. The full Rust trait threads a
// sandbox context through every call; here that is folded into the implementation
// (for example [LocalFS] ignores sandboxing), matching the `sandbox = None`
// call sites in the Rust source.
type FileSystem interface {
	// GetMetadata returns metadata for path. It returns an error when the path
	// does not exist or cannot be inspected.
	GetMetadata(ctx context.Context, path abspath.AbsolutePathBuf) (FileMetadata, error)

	// ReadFileText reads path and decodes it as UTF-8 text. It returns an error
	// when the path cannot be read or the contents are not valid UTF-8, mirroring
	// the Rust `read_file_text` default implementation.
	ReadFileText(ctx context.Context, path abspath.AbsolutePathBuf) (string, error)
}

// LocalFS is the default [FileSystem] backed by the local operating system,
// mirroring the Rust `codex_exec_server::LOCAL_FS`.
//
// The zero value is ready to use. It is stateless and therefore safe for
// concurrent use.
type LocalFS struct{}

// GetMetadata implements [FileSystem] using os.Lstat so that symlinks are
// reported as symlinks (mirroring the Rust local filesystem, which inspects the
// symlink itself rather than its target).
func (LocalFS) GetMetadata(_ context.Context, path abspath.AbsolutePathBuf) (FileMetadata, error) {
	info, err := os.Lstat(path.Path())
	if err != nil {
		return FileMetadata{}, fmt.Errorf("pluginutil: stat %q: %w", path.Path(), err)
	}
	mode := info.Mode()
	return FileMetadata{
		IsDirectory: mode.IsDir(),
		IsFile:      mode.IsRegular(),
		IsSymlink:   mode&os.ModeSymlink != 0,
	}, nil
}

// ReadFileText implements [FileSystem]. It reads the file and validates that the
// bytes are UTF-8, returning an error otherwise to match Rust's
// `String::from_utf8` behavior.
func (LocalFS) ReadFileText(_ context.Context, path abspath.AbsolutePathBuf) (string, error) {
	data, err := os.ReadFile(path.Path())
	if err != nil {
		return "", fmt.Errorf("pluginutil: read %q: %w", path.Path(), err)
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("pluginutil: file %q is not valid UTF-8", path.Path())
	}
	return string(data), nil
}
