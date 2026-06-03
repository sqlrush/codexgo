//go:build darwin

package prochard

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// preMainHardening applies macOS process hardening:
//   - deny debugger (ptrace) attach,
//   - set the core file size limit to zero,
//   - clear DYLD_* environment variables, which can be used to subvert library
//     loading.
func preMainHardening() {
	// Prevent debuggers from attaching to this process.
	if err := unix.PtraceDenyAttach(); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: ptrace(PT_DENY_ATTACH) failed: %v\n", err)
		os.Exit(PtraceDenyAttachFailedExitCode)
	}

	// Set the core file size limit to 0 to prevent core dumps.
	setCoreFileSizeLimitToZero()

	// Remove all DYLD_ environment variables, which can be used to subvert
	// library loading.
	removeEnvVarsWithPrefix(dyldPrefix)
}
