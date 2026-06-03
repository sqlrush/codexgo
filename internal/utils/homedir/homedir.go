package homedir

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// envVar is the environment variable that overrides the Codex home directory.
const envVar = "CODEX_HOME"

// defaultDirName is the default Codex directory name placed under the user's
// home directory when CODEX_HOME is not set.
const defaultDirName = ".codex"

// Sentinel errors that mirror the std::io::ErrorKind values the upstream Rust
// implementation produces. Callers can match on these with errors.Is.
var (
	// ErrNotFound mirrors std::io::ErrorKind::NotFound. It is returned when
	// CODEX_HOME points to a path that does not exist, when reading the home
	// directory fails to find a path, or when the user's home directory cannot
	// be determined.
	ErrNotFound = errors.New("not found")

	// ErrInvalidInput mirrors std::io::ErrorKind::InvalidInput. It is returned
	// when CODEX_HOME points to a path that exists but is not a directory.
	ErrInvalidInput = errors.New("invalid input")
)

// FindCodexHome returns the path to the Codex configuration directory.
//
// The location can be overridden with the CODEX_HOME environment variable. If
// CODEX_HOME is not set (or is empty), it defaults to ~/.codex.
//
//   - If CODEX_HOME is set, the value must exist and be a directory. The value
//     is canonicalized (symlinks resolved) and a normalized absolute path is
//     returned; otherwise an error is returned.
//   - If CODEX_HOME is not set, this function does not verify that the returned
//     directory exists.
//
// This matches the precedence and behavior of Codex's find_codex_home.
func FindCodexHome() (string, error) {
	// std::env::var returns the value only if it is present AND valid UTF-8;
	// upstream then filters out the empty string. os.Getenv returns "" for an
	// unset variable, so treating "" as "unset" reproduces both the unset and
	// empty-string cases.
	val := os.Getenv(envVar)
	if val == "" {
		return findCodexHomeFromEnv(nil)
	}
	return findCodexHomeFromEnv(&val)
}

// findCodexHomeFromEnv is the testable core of FindCodexHome. A nil pointer
// represents an absent CODEX_HOME; a non-nil pointer carries the override value
// (which is always non-empty when produced by FindCodexHome).
//
// The argument is read-only and never mutated.
func findCodexHomeFromEnv(codexHomeEnv *string) (string, error) {
	if codexHomeEnv == nil {
		return defaultCodexHome()
	}
	return resolveEnvOverride(*codexHomeEnv)
}

// resolveEnvOverride validates and canonicalizes an explicit CODEX_HOME value.
//
// It reproduces the upstream sequence: stat the path (mapping NotFound to a
// dedicated message, other errors to a read failure), reject non-directories,
// then canonicalize and lexically normalize the result.
func resolveEnvOverride(val string) (string, error) {
	info, err := os.Stat(val)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("%w: %s points to %q, but that path does not exist",
				ErrNotFound, envVar, val)
		}
		return "", fmt.Errorf("failed to read %s %q: %w", envVar, val, err)
	}

	if !info.IsDir() {
		return "", fmt.Errorf("%w: %s points to %q, but that path is not a directory",
			ErrInvalidInput, envVar, val)
	}

	// Canonicalize: resolve symlinks to mirror PathBuf::canonicalize. Upstream
	// wraps canonicalization failures in a "failed to canonicalize" message.
	canonical, err := filepath.EvalSymlinks(val)
	if err != nil {
		return "", fmt.Errorf("failed to canonicalize %s %q: %w", envVar, val, err)
	}

	// AbsolutePathBuf::from_absolute_path lexically normalizes the (already
	// absolute, already canonical) path. Make it absolute defensively in case
	// EvalSymlinks returned a relative result for a relative input.
	abs, err := filepath.Abs(canonical)
	if err != nil {
		return "", fmt.Errorf("failed to canonicalize %s %q: %w", envVar, val, err)
	}
	return normalizeAbsolute(abs), nil
}

// defaultCodexHome returns <home>/.codex without verifying that it exists,
// matching the upstream behavior when CODEX_HOME is unset.
func defaultCodexHome() (string, error) {
	home, err := userHomeDir()
	if err != nil {
		return "", fmt.Errorf("%w: Could not find home directory", ErrNotFound)
	}
	joined := filepath.Join(home, defaultDirName)
	return normalizeAbsolute(joined), nil
}
