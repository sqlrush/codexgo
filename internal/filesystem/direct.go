package filesystem

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// DirectFileSystem performs the raw filesystem I/O. It rejects any non-nil
// sandbox context, since direct operations cannot apply a platform sandbox.
//
// Rust: `DirectFileSystem`.
type DirectFileSystem struct{}

// NewDirectFileSystem returns a zero-value DirectFileSystem.
func NewDirectFileSystem() DirectFileSystem { return DirectFileSystem{} }

var _ ExecutorFileSystem = DirectFileSystem{}

// ReadFile reads the entire file at path, failing for files larger than
// [MaxReadFileBytes].
//
// Rust: `DirectFileSystem::read_file`.
func (DirectFileSystem) ReadFile(ctx context.Context, path protocol.AbsolutePath, sandbox *FileSystemSandboxContext) ([]byte, error) {
	if err := rejectSandboxContext(sandbox); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := os.Stat(string(path))
	if err != nil {
		return nil, err
	}
	if info.Size() > MaxReadFileBytes {
		return nil, newInvalidInput(
			"file is too large to read: limit is " + strconv.FormatInt(MaxReadFileBytes, 10) + " bytes",
		)
	}
	return os.ReadFile(string(path))
}

// WriteFile writes contents to path, truncating any existing file. It does not
// create parent directories (matching `tokio::fs::write`).
//
// Rust: `DirectFileSystem::write_file`.
func (DirectFileSystem) WriteFile(ctx context.Context, path protocol.AbsolutePath, contents []byte, sandbox *FileSystemSandboxContext) error {
	if err := rejectSandboxContext(sandbox); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// 0o666 before umask matches the default mode used by tokio::fs::write /
	// std::fs::write.
	return os.WriteFile(string(path), contents, 0o666)
}

// CreateDirectory creates the directory at path.
//
// Rust: `DirectFileSystem::create_directory` (create_dir_all when recursive,
// create_dir otherwise).
func (DirectFileSystem) CreateDirectory(ctx context.Context, path protocol.AbsolutePath, opts CreateDirectoryOptions, sandbox *FileSystemSandboxContext) error {
	if err := rejectSandboxContext(sandbox); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// 0o777 before umask matches std::fs::create_dir{,_all}.
	if opts.Recursive {
		return os.MkdirAll(string(path), 0o777)
	}
	return os.Mkdir(string(path), 0o777)
}

// GetMetadata returns metadata for path. Type flags follow symlinks (via Stat),
// while is-symlink reflects the path itself (via Lstat), matching the Rust use
// of metadata + symlink_metadata.
//
// Rust: `DirectFileSystem::get_metadata`.
func (DirectFileSystem) GetMetadata(ctx context.Context, path protocol.AbsolutePath, sandbox *FileSystemSandboxContext) (FileMetadata, error) {
	if err := rejectSandboxContext(sandbox); err != nil {
		return FileMetadata{}, err
	}
	if err := ctx.Err(); err != nil {
		return FileMetadata{}, err
	}
	info, err := os.Stat(string(path))
	if err != nil {
		return FileMetadata{}, err
	}
	linkInfo, err := os.Lstat(string(path))
	if err != nil {
		return FileMetadata{}, err
	}
	return FileMetadata{
		IsDirectory:  info.IsDir(),
		IsFile:       info.Mode().IsRegular(),
		IsSymlink:    linkInfo.Mode()&fs.ModeSymlink != 0,
		CreatedAtMs:  createdAtUnixMs(info),
		ModifiedAtMs: systemTimeToUnixMs(info.ModTime()),
	}, nil
}

// ReadDirectory lists the direct children of the directory at path. Entries
// whose metadata cannot be resolved are skipped, matching the Rust behavior.
//
// Rust: `DirectFileSystem::read_directory`.
func (DirectFileSystem) ReadDirectory(ctx context.Context, path protocol.AbsolutePath, sandbox *FileSystemSandboxContext) ([]ReadDirectoryEntry, error) {
	if err := rejectSandboxContext(sandbox); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dirEntries, err := os.ReadDir(string(path))
	if err != nil {
		return nil, err
	}
	entries := make([]ReadDirectoryEntry, 0, len(dirEntries))
	for _, dirEntry := range dirEntries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// Rust resolves each child via tokio::fs::metadata (which follows
		// symlinks) and skips entries that fail to resolve.
		info, statErr := os.Stat(filepath.Join(string(path), dirEntry.Name()))
		if statErr != nil {
			continue
		}
		entries = append(entries, ReadDirectoryEntry{
			FileName:    dirEntry.Name(),
			IsDirectory: info.IsDir(),
			IsFile:      info.Mode().IsRegular(),
		})
	}
	return entries, nil
}

// Remove removes the file or directory tree at path.
//
// Rust: `DirectFileSystem::remove`. A missing path is ignored only when
// opts.Force is set.
func (DirectFileSystem) Remove(ctx context.Context, path protocol.AbsolutePath, opts RemoveOptions, sandbox *FileSystemSandboxContext) error {
	if err := rejectSandboxContext(sandbox); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := os.Lstat(string(path))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && opts.Force {
			return nil
		}
		return err
	}
	if info.IsDir() {
		if opts.Recursive {
			return os.RemoveAll(string(path))
		}
		return os.Remove(string(path))
	}
	return os.Remove(string(path))
}

// Copy copies sourcePath to destinationPath. Directory copies require
// opts.Recursive and reject copying into the source subtree. Symlinks are
// copied as links; regular files are copied by content.
//
// Rust: `DirectFileSystem::copy`. The Rust impl runs the work on a blocking
// task; the Go port runs synchronously but honors context cancellation between
// filesystem steps.
func (DirectFileSystem) Copy(ctx context.Context, sourcePath, destinationPath protocol.AbsolutePath, opts CopyOptions, sandbox *FileSystemSandboxContext) error {
	if err := rejectSandboxContext(sandbox); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return wrapTaskFailure(err)
	}
	return copyPath(ctx, string(sourcePath), string(destinationPath), opts)
}

// rejectSandboxContext rejects a non-nil sandbox context, matching the Rust
// `reject_sandbox_context` guard on DirectFileSystem.
func rejectSandboxContext(sandbox *FileSystemSandboxContext) error {
	if sandbox != nil {
		return newInvalidInput("direct filesystem operations do not accept sandbox context")
	}
	return nil
}

// systemTimeToUnixMs converts a time to Unix milliseconds, returning 0 for the
// zero time or pre-epoch values, matching `system_time_to_unix_ms`.
func systemTimeToUnixMs(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	ms := t.UnixMilli()
	if ms < 0 {
		return 0
	}
	return ms
}
