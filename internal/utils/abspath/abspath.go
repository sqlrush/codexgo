// Package abspath ports the OpenAI Codex Rust crate
// `codex-rs/utils/absolute-path` (the `AbsolutePathBuf` type) to Go as part of a
// faithful, drop-in-compatible reimplementation of Codex 0.136.0.
//
// An [AbsolutePathBuf] is a path that is guaranteed to be absolute and
// lexically normalized (dots and parent references resolved), though it is not
// guaranteed to be canonicalized or to exist on the filesystem. Values are
// immutable: every operation returns a new value and never mutates its receiver
// or its arguments.
//
// # Serialization
//
// To match Codex's serde representation (where `AbsolutePathBuf` wraps a
// `PathBuf`, which serializes as its path string), an [AbsolutePathBuf] marshals
// to and from a single JSON string.
//
// When unmarshaling, a base path is required to resolve relative inputs. Go has
// no equivalent of Rust's thread-local `AbsolutePathBufGuard`, so this package
// exposes the base explicitly:
//
//   - Plain [AbsolutePathBuf.UnmarshalJSON] mirrors the "no base" branch of the
//     Rust deserializer: it accepts only inputs that are already absolute and
//     errors otherwise.
//   - [Unmarshal] (and [Decoder]) mirror the "guarded" branch: a caller-supplied
//     base path resolves relative inputs.
//
// # Platform behavior
//
// Normalization is platform-aware (selected via runtime.GOOS) to mirror the Rust
// `cfg!(windows)` gating: Windows drive/UNC prefixes and dunce-style verbatim
// device prefixes are handled, while Unix uses '/'-rooted semantics.
package abspath

import (
	"fmt"
	"os"
)

// AbsolutePathBuf is an absolute, lexically normalized path.
//
// The zero value is not valid; construct values through the package
// constructors (for example [FromAbsolutePath], [ResolvePathAgainstBase], or
// [RelativeToCurrentDir]). The wrapped path string is unexported so values are
// effectively immutable.
type AbsolutePathBuf struct {
	path string
}

// ResolvePathAgainstBase mirrors Rust's
// `AbsolutePathBuf::resolve_path_against_base`.
//
// It expands a leading "~", applies platform normalization to both inputs, and
// then resolves path against basePath, normalizing the result. An absolute path
// ignores basePath. This operation is infallible.
func ResolvePathAgainstBase(path, basePath string) AbsolutePathBuf {
	expanded := normalizePathForPlatform(maybeExpandHomeDirectory(path))
	base := normalizePathForPlatform(basePath)
	return AbsolutePathBuf{path: absolutizeFrom(expanded, base)}
}

// FromAbsolutePath mirrors Rust's `AbsolutePathBuf::from_absolute_path`.
//
// It expands a leading "~", applies platform normalization, and then
// absolutizes the result. A relative path is resolved against the process
// working directory (and may therefore fail if that directory is unavailable).
func FromAbsolutePath(path string) (AbsolutePathBuf, error) {
	expanded := normalizePathForPlatform(maybeExpandHomeDirectory(path))
	if isAbsolute(expanded) {
		return AbsolutePathBuf{path: normalizePath(expanded)}, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return AbsolutePathBuf{}, fmt.Errorf("abspath: resolve relative path %q: %w", path, err)
	}
	return AbsolutePathBuf{path: absolutizeFrom(expanded, cwd)}, nil
}

// FromAbsolutePathChecked mirrors Rust's
// `AbsolutePathBuf::from_absolute_path_checked`.
//
// Unlike [FromAbsolutePath], it never consults the working directory: it requires
// the (home-expanded, platform-normalized) input to already be absolute and
// returns an error tagged with [ErrNotAbsolute] otherwise. The path is
// normalized against the root.
func FromAbsolutePathChecked(path string) (AbsolutePathBuf, error) {
	expanded := normalizePathForPlatform(maybeExpandHomeDirectory(path))
	if !isAbsolute(expanded) {
		return AbsolutePathBuf{}, fmt.Errorf("%w: %s", ErrNotAbsolute, path)
	}
	return AbsolutePathBuf{path: absolutizeFrom(expanded, rootPath())}, nil
}

// CurrentDir mirrors Rust's `AbsolutePathBuf::current_dir`: it returns the
// process working directory as an [AbsolutePathBuf].
func CurrentDir() (AbsolutePathBuf, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return AbsolutePathBuf{}, fmt.Errorf("abspath: read current dir: %w", err)
	}
	return FromAbsolutePath(cwd)
}

// RelativeToCurrentDir mirrors Rust's
// `AbsolutePathBuf::relative_to_current_dir`: it resolves path against the
// process working directory.
func RelativeToCurrentDir(path string) (AbsolutePathBuf, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return AbsolutePathBuf{}, fmt.Errorf("abspath: read current dir: %w", err)
	}
	return ResolvePathAgainstBase(path, cwd), nil
}

// Join mirrors Rust's `AbsolutePathBuf::join`: it resolves path against the
// receiver and returns a new [AbsolutePathBuf]. The receiver is not modified.
func (a AbsolutePathBuf) Join(path string) AbsolutePathBuf {
	return ResolvePathAgainstBase(path, a.path)
}

// Parent mirrors Rust's `AbsolutePathBuf::parent`: it returns the parent path
// and ok=true, or ok=false when the path has no parent (i.e. it is a root).
func (a AbsolutePathBuf) Parent() (AbsolutePathBuf, bool) {
	parent, ok := parentOf(a.path)
	if !ok {
		return AbsolutePathBuf{}, false
	}
	return AbsolutePathBuf{path: parent}, true
}

// Ancestors mirrors Rust's `AbsolutePathBuf::ancestors`: it returns the path
// followed by each successive parent, ending at the root. The result is a fresh
// slice.
func (a AbsolutePathBuf) Ancestors() []AbsolutePathBuf {
	var out []AbsolutePathBuf
	cur := a.path
	for {
		out = append(out, AbsolutePathBuf{path: cur})
		parent, ok := parentOf(cur)
		if !ok {
			break
		}
		cur = parent
	}
	return out
}

// String mirrors Rust's `Display`/`to_string_lossy`: it returns the path string.
func (a AbsolutePathBuf) String() string {
	return a.path
}

// Path is the Go analog of `as_path`/`into_path_buf`/`to_path_buf`: it returns
// the underlying path string, suitable for the os and path/filepath packages.
func (a AbsolutePathBuf) Path() string {
	return a.path
}
