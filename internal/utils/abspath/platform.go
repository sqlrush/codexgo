package abspath

import "runtime"

// isWindows reports whether path operations should use Windows semantics (drive
// prefixes, backslash separators, verbatim device prefixes).
//
// The Rust source gates several behaviors on `cfg!(windows)`. We mirror that by
// consulting the runtime GOOS by default. It is a package variable rather than a
// plain function so tests can exercise the platform-specific grammar on any host
// (mirroring the reference crate's `_with_flag` test seams). Production code
// never reassigns it.
var isWindows = func() bool {
	return runtime.GOOS == "windows"
}

// normalizePathForPlatform mirrors Rust's `normalize_path_for_platform`.
//
// On Windows it rewrites supported verbatim / device path prefixes (the
// "dunce-style" UNC normalization) into their friendly equivalents. On other
// platforms the input is returned unchanged.
func normalizePathForPlatform(path string) string {
	if isWindows() {
		if normalized, ok := normalizeWindowsDevicePath(path); ok {
			return normalized
		}
	}
	return path
}

// normalizeWindowsDevicePath mirrors Rust's `normalize_windows_device_path`.
//
// It strips the four supported verbatim / device prefixes:
//
//	\\?\UNC\server\share  -> \\server\share
//	\\.\UNC\server\share  -> \\server\share
//	\\?\D:\path           -> D:\path   (only when a drive-absolute path follows)
//	\\.\D:\path           -> D:\path   (only when a drive-absolute path follows)
//
// Any other device path (for example \\?\GLOBALROOT\Device) returns ok=false so
// the caller leaves it untouched. The returned string is a new value; the input
// is never mutated.
func normalizeWindowsDevicePath(path string) (string, bool) {
	if rest, ok := stripPrefix(path, `\\?\UNC\`); ok {
		return `\\` + rest, true
	}
	if rest, ok := stripPrefix(path, `\\.\UNC\`); ok {
		return `\\` + rest, true
	}
	if rest, ok := stripPrefix(path, `\\?\`); ok && isWindowsDriveAbsolutePath(rest) {
		return rest, true
	}
	if rest, ok := stripPrefix(path, `\\.\`); ok && isWindowsDriveAbsolutePath(rest) {
		return rest, true
	}
	return "", false
}

// isWindowsDriveAbsolutePath mirrors Rust's `is_windows_drive_absolute_path`:
// the value begins with a drive letter, a colon, and a separator (e.g. "C:\" or
// "C:/").
func isWindowsDriveAbsolutePath(path string) bool {
	if len(path) < 3 {
		return false
	}
	return isASCIIAlpha(path[0]) && path[1] == ':' && (path[2] == '\\' || path[2] == '/')
}

// stripPrefix returns the remainder of s after prefix and true when s starts
// with prefix; otherwise it returns "" and false.
func stripPrefix(s, prefix string) (string, bool) {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):], true
	}
	return "", false
}

// isASCIIAlpha reports whether b is an ASCII letter.
func isASCIIAlpha(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}
