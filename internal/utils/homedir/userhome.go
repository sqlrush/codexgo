package homedir

import (
	"os"
	"runtime"
)

// userHomeDir resolves the current user's home directory, mirroring the
// precedence of the Rust `dirs::home_dir` function used upstream.
//
// On non-Windows platforms `dirs::home_dir` honors the HOME environment
// variable first and falls back to the system password database when HOME is
// unset or empty. On Windows it uses the standard known-folder lookup.
//
// Go's os.UserHomeDir implements equivalent platform logic (HOME on Unix,
// USERPROFILE on Windows). We honor a non-empty HOME explicitly first so that
// tests and callers can override the location the same way the Rust code does,
// then defer to os.UserHomeDir (which consults the password database / known
// folders) for the fallback case.
//
// The returned string is the resolved home directory; the process environment
// is not modified.
func userHomeDir() (string, error) {
	if runtime.GOOS != "windows" {
		if home := os.Getenv("HOME"); home != "" {
			return home, nil
		}
	}
	return os.UserHomeDir()
}
