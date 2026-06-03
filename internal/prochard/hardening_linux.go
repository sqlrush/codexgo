//go:build linux || android

package prochard

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// preMainHardening applies Linux/Android process hardening:
//   - mark the process non-dumpable (disables ptrace attach by same-user
//     processes),
//   - set the core file size limit to zero (defense in depth),
//   - clear LD_* environment variables.
func preMainHardening() {
	// Disable ptrace attach / mark process non-dumpable.
	if err := disableProcessDumping(); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: prctl(PR_SET_DUMPABLE, 0) failed: %v\n", err)
		os.Exit(PrctlFailedExitCode)
	}

	// For "defense in depth," set the core file size limit to 0.
	setCoreFileSizeLimitToZero()

	// Official codex releases are MUSL-linked, which means that variables such
	// as LD_PRELOAD are ignored anyway, but just to be sure, clear them here.
	removeEnvVarsWithPrefix(ldPrefix)
}

// DisableProcessDumping marks the current Linux process non-dumpable so that
// same-user processes cannot attach to it with ptrace. It returns an error
// wrapping the underlying prctl failure.
func DisableProcessDumping() error {
	return disableProcessDumping()
}

func disableProcessDumping() error {
	if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		return fmt.Errorf("prctl(PR_SET_DUMPABLE, 0): %w", err)
	}
	return nil
}
