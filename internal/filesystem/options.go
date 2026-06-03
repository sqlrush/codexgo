// Package filesystem is a faithful, drop-in-compatible Go port of the OpenAI
// Codex Rust crate `codex-rs/file-system` (Codex 0.136.0).
//
// It provides the [ExecutorFileSystem] abstraction plus the concrete local
// filesystem operations (read/write/create-directory/metadata/read-directory/
// remove/copy) and the io-error-to-JSON-RPC error mapping used by the
// app-server `fs/*` methods.
//
// The Rust crate routes operations through one of three layered
// implementations:
//
//   - [DirectFileSystem]   performs the raw filesystem I/O.
//   - [UnsandboxedFileSystem] wraps DirectFileSystem and rejects any sandbox
//     context that would require platform sandboxing.
//   - [LocalFileSystem]    selects between the unsandboxed implementation and
//     a platform sandbox depending on the supplied sandbox context.
//
// This port faithfully reproduces the unsandboxed path (the only path exercised
// by the app-server `fs/*` methods, which always pass a nil sandbox context).
// Platform sandboxing (landlock / seatbelt / Windows job objects) is
// intentionally out of scope; see the deviation notes in [LocalFileSystem].
//
// All operations accept a [context.Context] so callers can cancel or bound
// long-running directory walks; the Rust crate relies on tokio task
// cancellation for the same purpose. Inputs are never mutated.
package filesystem

// MaxReadFileBytes is the maximum size of a file that [DirectFileSystem.ReadFile]
// will read. Reading a larger file fails with an InvalidInput-class error.
//
// Rust: `const MAX_READ_FILE_BYTES: u64 = 512 * 1024 * 1024;`.
const MaxReadFileBytes int64 = 512 * 1024 * 1024

// CreateDirectoryOptions controls directory creation.
//
// Rust: `CreateDirectoryOptions { recursive: bool }`.
type CreateDirectoryOptions struct {
	// Recursive creates parent directories as needed when true.
	Recursive bool
}

// RemoveOptions controls removal of a path.
//
// Rust: `RemoveOptions { recursive: bool, force: bool }`.
type RemoveOptions struct {
	// Recursive removes directory contents recursively when true.
	Recursive bool
	// Force suppresses the not-found error when true.
	Force bool
}

// CopyOptions controls copying of a path.
//
// Rust: `CopyOptions { recursive: bool }`.
type CopyOptions struct {
	// Recursive is required to copy a directory; ignored for files.
	Recursive bool
}

// FileMetadata describes a path resolved through symlinks, except for the
// is-symlink flag which reflects the path itself.
//
// Rust: `FileMetadata { is_directory, is_file, is_symlink, created_at_ms,
// modified_at_ms }`. The created/modified timestamps are Unix milliseconds, or 0
// when unavailable.
type FileMetadata struct {
	// IsDirectory reports whether the resolved path is a directory.
	IsDirectory bool
	// IsFile reports whether the resolved path is a regular file.
	IsFile bool
	// IsSymlink reports whether the path itself is a symbolic link.
	IsSymlink bool
	// CreatedAtMs is the creation time in Unix milliseconds, or 0.
	CreatedAtMs int64
	// ModifiedAtMs is the modification time in Unix milliseconds, or 0.
	ModifiedAtMs int64
}

// ReadDirectoryEntry is a single direct child of a directory.
//
// Rust: `ReadDirectoryEntry { file_name, is_directory, is_file }`.
type ReadDirectoryEntry struct {
	// FileName is the entry name only, not a path.
	FileName string
	// IsDirectory reports whether the entry resolves to a directory.
	IsDirectory bool
	// IsFile reports whether the entry resolves to a regular file.
	IsFile bool
}
