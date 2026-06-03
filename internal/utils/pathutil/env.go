// Package pathutil provides path normalization, symlink resolution, and atomic
// writes shared across Codex crates.
//
// It is a faithful Go port of the codex-utils-path Rust crate. Externally
// observable behavior and formats are preserved so callers behave identically
// across the two implementations.
//
// The crate's behavior is platform-sensitive in three ways that this port
// mirrors:
//   - WSL detection and case-folding of /mnt/<drive> paths (Linux only).
//   - Windows verbatim ("\\?\") prefix simplification (Windows only).
//   - Symlink chain resolution (all platforms, with cycle detection).
package pathutil

import (
	"os"
	"runtime"
	"strings"
)

// IsWSL reports whether the current process is running under Windows Subsystem
// for Linux.
//
// Detection only applies on Linux: it first checks the WSL_DISTRO_NAME
// environment variable, then falls back to scanning /proc/version for the
// substring "microsoft" (case-insensitive). On every other platform it returns
// false.
func IsWSL() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	if _, ok := os.LookupEnv("WSL_DISTRO_NAME"); ok {
		return true
	}
	version, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(version)), "microsoft")
}
