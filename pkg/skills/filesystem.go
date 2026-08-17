package skills

import (
	"context"
	"fmt"
	"os"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
	"github.com/sqlrush/codexgo/internal/utils/pluginutil"
)

// FileMetadata reuses the plugin filesystem metadata shape: it records whether a
// path is a directory, file, or symlink. It mirrors the subset of the Rust
// `codex_exec_server::FileMetadata` used by skill discovery.
type FileMetadata = pluginutil.FileMetadata

// DirEntry is a single entry returned by [FileSystem.ReadDirectory]. It mirrors
// the Rust `codex_exec_server::DirectoryEntry` fields consumed by the loader.
type DirEntry struct {
	// FileName is the entry's name within its parent directory (not a full path).
	FileName string
}

// FileSystem abstracts the directory traversal and file reads that skill
// discovery and injection need. It is the Go analog of the subset of
// `codex_exec_server::ExecutorFileSystem` used by codex-core-skills, with the
// Rust sandbox argument folded into the implementation (callers pass
// `sandbox = None`).
//
// Keeping the interface small lets callers inject local, sandboxed, or in-memory
// filesystems (the tests use an in-memory implementation).
type FileSystem interface {
	// GetMetadata returns metadata for path, or an error when it cannot be
	// inspected. Implementations should not follow the final symlink so that
	// symlinks are reported as symlinks.
	GetMetadata(ctx context.Context, path abspath.AbsolutePathBuf) (FileMetadata, error)

	// ReadFileText reads path and decodes it as UTF-8 text.
	ReadFileText(ctx context.Context, path abspath.AbsolutePathBuf) (string, error)

	// ReadDirectory lists the immediate children of dir.
	ReadDirectory(ctx context.Context, dir abspath.AbsolutePathBuf) ([]DirEntry, error)
}

// LocalFS is the default [FileSystem] backed by the local operating system,
// mirroring the Rust `codex_exec_server::LOCAL_FS`. The zero value is ready to
// use and is safe for concurrent use.
type LocalFS struct{}

// localPluginFS satisfies the narrower pluginutil.FileSystem interface so we can
// reuse the plugin namespace resolver with LocalFS-backed reads.
var localPluginFS pluginutil.FileSystem = pluginutil.LocalFS{}

// GetMetadata implements [FileSystem].
func (LocalFS) GetMetadata(ctx context.Context, path abspath.AbsolutePathBuf) (FileMetadata, error) {
	return pluginutil.LocalFS{}.GetMetadata(ctx, path)
}

// ReadFileText implements [FileSystem].
func (LocalFS) ReadFileText(ctx context.Context, path abspath.AbsolutePathBuf) (string, error) {
	return pluginutil.LocalFS{}.ReadFileText(ctx, path)
}

// ReadDirectory implements [FileSystem] using os.ReadDir.
func (LocalFS) ReadDirectory(_ context.Context, dir abspath.AbsolutePathBuf) ([]DirEntry, error) {
	entries, err := os.ReadDir(dir.Path())
	if err != nil {
		return nil, fmt.Errorf("skills: read dir %q: %w", dir.Path(), err)
	}
	out := make([]DirEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, DirEntry{FileName: entry.Name()})
	}
	return out, nil
}

// pluginFSAdapter adapts a skills [FileSystem] to the narrower
// pluginutil.FileSystem interface used by the plugin namespace resolver.
type pluginFSAdapter struct {
	fs FileSystem
}

func (a pluginFSAdapter) GetMetadata(ctx context.Context, path abspath.AbsolutePathBuf) (pluginutil.FileMetadata, error) {
	return a.fs.GetMetadata(ctx, path)
}

func (a pluginFSAdapter) ReadFileText(ctx context.Context, path abspath.AbsolutePathBuf) (string, error) {
	return a.fs.ReadFileText(ctx, path)
}

// asPluginFS returns a pluginutil.FileSystem view over fs, preferring the
// concrete LocalFS singleton when possible.
func asPluginFS(fs FileSystem) pluginutil.FileSystem {
	if _, ok := fs.(LocalFS); ok {
		return localPluginFS
	}
	return pluginFSAdapter{fs: fs}
}
