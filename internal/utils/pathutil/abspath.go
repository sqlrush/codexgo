package pathutil

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// This file ports the minimal subset of the codex-utils-absolute-path crate
// (AbsolutePathBuf) that codex-utils-path depends on, namely:
//
//   - fromAbsolutePath:        AbsolutePathBuf::from_absolute_path
//   - resolvePathAgainstBase:  AbsolutePathBuf::resolve_path_against_base
//
// The full crate is a separate port; only the pieces used by symlink resolution
// in resolve_symlink_write_paths are reproduced here so this package depends on
// the standard library alone. See externalDeps for what would otherwise be used.

// fromAbsolutePath mirrors AbsolutePathBuf::from_absolute_path.
//
// It expands a leading "~", applies platform normalization, and then
// absolutizes: absolute inputs are normalized in place; relative inputs are
// resolved against the process current working directory. The boolean result is
// false when the current working directory is required but unavailable, matching
// the Rust function's io::Result error path.
func fromAbsolutePath(path string) (string, bool) {
	expanded := normalizePathForPlatform(maybeExpandHomeDirectory(path))
	if isAbsolutePath(expanded) {
		return normalizePath(expanded), true
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", false
	}
	return resolvePathAgainstBaseNormalized(expanded, cwd), true
}

// resolvePathAgainstBase mirrors AbsolutePathBuf::resolve_path_against_base.
//
// The path is home-expanded and platform-normalized, the base is
// platform-normalized, and the result is absolutized against the base. This is
// infallible, as in Rust.
func resolvePathAgainstBase(path string, base string) string {
	expanded := normalizePathForPlatform(maybeExpandHomeDirectory(path))
	normBase := normalizePathForPlatform(base)
	return resolvePathAgainstBaseNormalized(expanded, normBase)
}

// resolvePathAgainstBaseNormalized assumes its inputs are already
// platform-normalized and home-expanded. It joins (when relative) then
// normalizes dot components.
func resolvePathAgainstBaseNormalized(path string, base string) string {
	return normalizePath(pathWithBase(path, base))
}

// maybeExpandHomeDirectory mirrors AbsolutePathBuf::maybe_expand_home_directory.
//
// A leading "~" is replaced with the user's home directory. "~" alone maps to
// the home directory; "~/rest" (and "~\rest" on Windows) maps to home joined
// with the trailing component. If the home directory cannot be determined, or
// the path does not begin with "~", the input is returned unchanged.
func maybeExpandHomeDirectory(path string) string {
	rest, ok := strings.CutPrefix(path, "~")
	if !ok {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if rest == "" {
		return home
	}
	if r, ok := strings.CutPrefix(rest, "/"); ok {
		return filepath.Join(home, strings.TrimLeft(r, "/"))
	}
	if runtime.GOOS == "windows" {
		if r, ok := strings.CutPrefix(rest, `\`); ok {
			return filepath.Join(home, strings.TrimLeft(r, `\`))
		}
	}
	return path
}

// normalizePathForPlatform mirrors normalize_path_for_platform.
//
// On Windows it rewrites supported verbatim/device prefixes ("\\?\", "\\.\")
// into their plain forms. On every other platform the path is returned
// unchanged.
func normalizePathForPlatform(path string) string {
	if runtime.GOOS != "windows" {
		return path
	}
	if normalized, ok := normalizeWindowsDevicePath(path); ok {
		return normalized
	}
	return path
}

// normalizeWindowsDevicePath mirrors normalize_windows_device_path. It returns
// the rewritten path and true when a supported prefix is recognized.
func normalizeWindowsDevicePath(path string) (string, bool) {
	if unc, ok := strings.CutPrefix(path, `\\?\UNC\`); ok {
		return `\\` + unc, true
	}
	if unc, ok := strings.CutPrefix(path, `\\.\UNC\`); ok {
		return `\\` + unc, true
	}
	if rest, ok := strings.CutPrefix(path, `\\?\`); ok && isWindowsDriveAbsolutePath(rest) {
		return rest, true
	}
	if rest, ok := strings.CutPrefix(path, `\\.\`); ok && isWindowsDriveAbsolutePath(rest) {
		return rest, true
	}
	return "", false
}

// isWindowsDriveAbsolutePath mirrors is_windows_drive_absolute_path: a string of
// the form "X:\" or "X:/" where X is an ASCII letter.
func isWindowsDriveAbsolutePath(path string) bool {
	if len(path) < 3 {
		return false
	}
	return isASCIIAlpha(path[0]) && path[1] == ':' && (path[2] == '\\' || path[2] == '/')
}
