package filesystem

import (
	"context"
	"fmt"
	"unicode/utf8"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// ExecutorFileSystem is the abstract filesystem access surface used by
// components that may operate locally or via a remote environment.
//
// Rust: the `ExecutorFileSystem` async trait. Each method accepts an optional
// sandbox context; callers that do not need sandboxing pass nil. The Go port
// adds a leading [context.Context] to each method for cancellation, in place of
// the implicit tokio task cancellation in the Rust async trait.
//
// Implementations must not mutate their arguments.
type ExecutorFileSystem interface {
	// ReadFile reads the entire file at path.
	ReadFile(ctx context.Context, path protocol.AbsolutePath, sandbox *FileSystemSandboxContext) ([]byte, error)

	// WriteFile writes contents to path, truncating any existing file.
	WriteFile(ctx context.Context, path protocol.AbsolutePath, contents []byte, sandbox *FileSystemSandboxContext) error

	// CreateDirectory creates the directory at path.
	CreateDirectory(ctx context.Context, path protocol.AbsolutePath, opts CreateDirectoryOptions, sandbox *FileSystemSandboxContext) error

	// GetMetadata returns metadata for path.
	GetMetadata(ctx context.Context, path protocol.AbsolutePath, sandbox *FileSystemSandboxContext) (FileMetadata, error)

	// ReadDirectory lists the direct children of the directory at path.
	ReadDirectory(ctx context.Context, path protocol.AbsolutePath, sandbox *FileSystemSandboxContext) ([]ReadDirectoryEntry, error)

	// Remove removes the file or directory tree at path.
	Remove(ctx context.Context, path protocol.AbsolutePath, opts RemoveOptions, sandbox *FileSystemSandboxContext) error

	// Copy copies sourcePath to destinationPath.
	Copy(ctx context.Context, sourcePath, destinationPath protocol.AbsolutePath, opts CopyOptions, sandbox *FileSystemSandboxContext) error
}

// ReadFileText reads a file via fs and decodes it as UTF-8 text.
//
// Rust: the `read_file_text` default trait method, which returns an
// InvalidData-class error on invalid UTF-8. The Go port surfaces the same
// failure as a wrapped error; because InvalidData is not InvalidInput, it maps
// to an internal-error through [MapFsError], matching Rust.
//
// It is exposed as a free function rather than an interface method so that all
// [ExecutorFileSystem] implementations share one definition (Go interfaces have
// no default methods).
func ReadFileText(ctx context.Context, fs ExecutorFileSystem, path protocol.AbsolutePath, sandbox *FileSystemSandboxContext) (string, error) {
	bytes, err := fs.ReadFile(ctx, path, sandbox)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(bytes) {
		return "", fmt.Errorf("stream did not contain valid UTF-8")
	}
	return string(bytes), nil
}
