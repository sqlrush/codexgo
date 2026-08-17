package filesystem

import (
	"context"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// UnsandboxedFileSystem wraps a [DirectFileSystem] and rejects any sandbox
// context that would require a platform sandbox, then performs the operation
// directly with no sandbox.
//
// Rust: `UnsandboxedFileSystem`.
type UnsandboxedFileSystem struct {
	fileSystem DirectFileSystem
}

// NewUnsandboxedFileSystem returns a zero-value UnsandboxedFileSystem backed by
// a DirectFileSystem.
func NewUnsandboxedFileSystem() UnsandboxedFileSystem {
	return UnsandboxedFileSystem{fileSystem: DirectFileSystem{}}
}

var _ ExecutorFileSystem = UnsandboxedFileSystem{}

// ReadFile implements ExecutorFileSystem.
func (u UnsandboxedFileSystem) ReadFile(ctx context.Context, path protocol.AbsolutePath, sandbox *FileSystemSandboxContext) ([]byte, error) {
	if err := rejectPlatformSandboxContext(sandbox); err != nil {
		return nil, err
	}
	return u.fileSystem.ReadFile(ctx, path, nil)
}

// WriteFile implements ExecutorFileSystem.
func (u UnsandboxedFileSystem) WriteFile(ctx context.Context, path protocol.AbsolutePath, contents []byte, sandbox *FileSystemSandboxContext) error {
	if err := rejectPlatformSandboxContext(sandbox); err != nil {
		return err
	}
	return u.fileSystem.WriteFile(ctx, path, contents, nil)
}

// CreateDirectory implements ExecutorFileSystem.
func (u UnsandboxedFileSystem) CreateDirectory(ctx context.Context, path protocol.AbsolutePath, opts CreateDirectoryOptions, sandbox *FileSystemSandboxContext) error {
	if err := rejectPlatformSandboxContext(sandbox); err != nil {
		return err
	}
	return u.fileSystem.CreateDirectory(ctx, path, opts, nil)
}

// GetMetadata implements ExecutorFileSystem.
func (u UnsandboxedFileSystem) GetMetadata(ctx context.Context, path protocol.AbsolutePath, sandbox *FileSystemSandboxContext) (FileMetadata, error) {
	if err := rejectPlatformSandboxContext(sandbox); err != nil {
		return FileMetadata{}, err
	}
	return u.fileSystem.GetMetadata(ctx, path, nil)
}

// ReadDirectory implements ExecutorFileSystem.
func (u UnsandboxedFileSystem) ReadDirectory(ctx context.Context, path protocol.AbsolutePath, sandbox *FileSystemSandboxContext) ([]ReadDirectoryEntry, error) {
	if err := rejectPlatformSandboxContext(sandbox); err != nil {
		return nil, err
	}
	return u.fileSystem.ReadDirectory(ctx, path, nil)
}

// Remove implements ExecutorFileSystem.
func (u UnsandboxedFileSystem) Remove(ctx context.Context, path protocol.AbsolutePath, opts RemoveOptions, sandbox *FileSystemSandboxContext) error {
	if err := rejectPlatformSandboxContext(sandbox); err != nil {
		return err
	}
	return u.fileSystem.Remove(ctx, path, opts, nil)
}

// Copy implements ExecutorFileSystem.
func (u UnsandboxedFileSystem) Copy(ctx context.Context, sourcePath, destinationPath protocol.AbsolutePath, opts CopyOptions, sandbox *FileSystemSandboxContext) error {
	if err := rejectPlatformSandboxContext(sandbox); err != nil {
		return err
	}
	return u.fileSystem.Copy(ctx, sourcePath, destinationPath, opts, nil)
}

// LocalFileSystem routes each operation to the unsandboxed implementation or,
// when a sandbox context demands platform sandboxing, to a configured sandboxed
// implementation.
//
// Rust: `LocalFileSystem`. DEVIATION: this port implements only the unsandboxed
// variant (`LocalFileSystem::unsandboxed`). Platform sandboxing
// (landlock/seatbelt/Windows job objects) is out of scope and depends on
// external helper binaries that are not part of the stdlib-only budget; an
// operation that would require the sandbox therefore fails with the same
// InvalidInput-class error the Rust code raises when runtime paths are missing
// ("sandboxed filesystem operations require configured runtime paths"). The
// app-server fs/* methods always pass a nil sandbox context, so they exercise
// only the unsandboxed path, which is fully supported.
type LocalFileSystem struct {
	unsandboxed UnsandboxedFileSystem
}

// NewLocalFileSystem returns an unsandboxed LocalFileSystem.
//
// Rust: `LocalFileSystem::unsandboxed`.
func NewLocalFileSystem() LocalFileSystem {
	return LocalFileSystem{unsandboxed: NewUnsandboxedFileSystem()}
}

var _ ExecutorFileSystem = LocalFileSystem{}

// LocalFS returns the process-wide unsandboxed LocalFileSystem instance.
//
// Rust: the `LOCAL_FS` LazyLock static. The Go value is cheap to construct and
// stateless, so it is returned by value rather than memoized.
func LocalFS() LocalFileSystem { return NewLocalFileSystem() }

// fileSystemFor selects the backing implementation for an operation, mirroring
// Rust `LocalFileSystem::file_system_for`. Since no sandboxed backend is
// configured, a context that requires sandboxing yields an error.
func (l LocalFileSystem) fileSystemFor(sandbox *FileSystemSandboxContext) (ExecutorFileSystem, error) {
	if sandbox != nil && sandbox.ShouldRunInSandbox() {
		return nil, newInvalidInput("sandboxed filesystem operations require configured runtime paths")
	}
	return l.unsandboxed, nil
}

// ReadFile implements ExecutorFileSystem.
func (l LocalFileSystem) ReadFile(ctx context.Context, path protocol.AbsolutePath, sandbox *FileSystemSandboxContext) ([]byte, error) {
	fileSystem, err := l.fileSystemFor(sandbox)
	if err != nil {
		return nil, err
	}
	return fileSystem.ReadFile(ctx, path, sandbox)
}

// WriteFile implements ExecutorFileSystem.
func (l LocalFileSystem) WriteFile(ctx context.Context, path protocol.AbsolutePath, contents []byte, sandbox *FileSystemSandboxContext) error {
	fileSystem, err := l.fileSystemFor(sandbox)
	if err != nil {
		return err
	}
	return fileSystem.WriteFile(ctx, path, contents, sandbox)
}

// CreateDirectory implements ExecutorFileSystem.
func (l LocalFileSystem) CreateDirectory(ctx context.Context, path protocol.AbsolutePath, opts CreateDirectoryOptions, sandbox *FileSystemSandboxContext) error {
	fileSystem, err := l.fileSystemFor(sandbox)
	if err != nil {
		return err
	}
	return fileSystem.CreateDirectory(ctx, path, opts, sandbox)
}

// GetMetadata implements ExecutorFileSystem.
func (l LocalFileSystem) GetMetadata(ctx context.Context, path protocol.AbsolutePath, sandbox *FileSystemSandboxContext) (FileMetadata, error) {
	fileSystem, err := l.fileSystemFor(sandbox)
	if err != nil {
		return FileMetadata{}, err
	}
	return fileSystem.GetMetadata(ctx, path, sandbox)
}

// ReadDirectory implements ExecutorFileSystem.
func (l LocalFileSystem) ReadDirectory(ctx context.Context, path protocol.AbsolutePath, sandbox *FileSystemSandboxContext) ([]ReadDirectoryEntry, error) {
	fileSystem, err := l.fileSystemFor(sandbox)
	if err != nil {
		return nil, err
	}
	return fileSystem.ReadDirectory(ctx, path, sandbox)
}

// Remove implements ExecutorFileSystem.
func (l LocalFileSystem) Remove(ctx context.Context, path protocol.AbsolutePath, opts RemoveOptions, sandbox *FileSystemSandboxContext) error {
	fileSystem, err := l.fileSystemFor(sandbox)
	if err != nil {
		return err
	}
	return fileSystem.Remove(ctx, path, opts, sandbox)
}

// Copy implements ExecutorFileSystem.
func (l LocalFileSystem) Copy(ctx context.Context, sourcePath, destinationPath protocol.AbsolutePath, opts CopyOptions, sandbox *FileSystemSandboxContext) error {
	fileSystem, err := l.fileSystemFor(sandbox)
	if err != nil {
		return err
	}
	return fileSystem.Copy(ctx, sourcePath, destinationPath, opts, sandbox)
}

// rejectPlatformSandboxContext rejects a sandbox context that requires a
// platform sandbox, mirroring Rust `reject_platform_sandbox_context`.
func rejectPlatformSandboxContext(sandbox *FileSystemSandboxContext) error {
	if sandbox != nil && sandbox.ShouldRunInSandbox() {
		return newInvalidInput("sandboxed filesystem operations require configured runtime paths")
	}
	return nil
}
