//go:build darwin || linux || freebsd || openbsd || netbsd

package prochard

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// removeEnvVarsWithPrefix unsets every environment variable whose name begins
// with prefix. Unsetting a non-existent variable is harmless, so any error from
// os.Unsetenv (which does not occur for valid keys on Unix) is ignored.
func removeEnvVarsWithPrefix(prefix string) {
	for _, key := range envKeysWithPrefix(os.Environ(), prefix) {
		_ = os.Unsetenv(key)
	}
}

// setCoreFileSizeLimitToZero sets RLIMIT_CORE to zero so that the process
// cannot produce core dumps. On failure it prints an error and exits with
// SetRlimitCoreFailedExitCode, mirroring codex.
func setCoreFileSizeLimitToZero() {
	rlim := unix.Rlimit{Cur: 0, Max: 0}
	if err := unix.Setrlimit(unix.RLIMIT_CORE, &rlim); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: setrlimit(RLIMIT_CORE) failed: %v\n", err)
		os.Exit(SetRlimitCoreFailedExitCode)
	}
}
