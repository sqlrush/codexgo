package abspath

import (
	"os"
	"strings"
)

// homeDir returns the current user's home directory, mirroring Rust's
// `dirs::home_dir()`. It returns ok=false when the home directory cannot be
// determined, which causes home-expansion to be skipped (matching the Rust code,
// which only expands when `home_dir()` is Some).
//
// The stdlib `os.UserHomeDir` consults $HOME on Unix and the appropriate
// USERPROFILE / drive variables on Windows, which is the same precedence used by
// the `dirs` crate.
var homeDir = func() (string, bool) {
	h, err := os.UserHomeDir()
	if err != nil || h == "" {
		return "", false
	}
	return h, true
}

// maybeExpandHomeDirectory mirrors Rust's
// `AbsolutePathBuf::maybe_expand_home_directory`.
//
// It expands a leading "~" using the resolved home directory:
//
//	"~"        -> home
//	"~/rest"   -> home/rest          (leading slashes in rest are stripped)
//	"~\\rest"  -> home/rest          (Windows only; leading backslashes stripped)
//
// Any other input (including "~user" forms) is returned unchanged. The input is
// never mutated; a new string is returned.
func maybeExpandHomeDirectory(path string) string {
	rest, ok := stripPrefix(path, "~")
	if !ok {
		return path
	}
	home, hasHome := homeDir()
	if !hasHome {
		return path
	}

	if rest == "" {
		return home
	}
	if r, ok := stripPrefix(rest, "/"); ok {
		return joinHome(home, strings.TrimLeft(r, "/"))
	}
	if isWindows() {
		if r, ok := stripPrefix(rest, `\`); ok {
			return joinHome(home, strings.TrimLeft(r, `\`))
		}
	}
	return path
}

// joinHome joins home with rest using Path::join semantics for the active
// platform. rest has already had its leading separators stripped.
func joinHome(home, rest string) string {
	if isWindows() {
		return joinWindows(home, rest)
	}
	return joinUnix(home, rest)
}
