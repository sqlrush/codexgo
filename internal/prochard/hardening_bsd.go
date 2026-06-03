//go:build freebsd || openbsd

package prochard

// preMainHardening applies FreeBSD/OpenBSD process hardening. Unlike Linux and
// macOS there is no ptrace self-protection step here; matching codex, it sets
// the core file size limit to zero and clears LD_* environment variables.
func preMainHardening() {
	// FreeBSD/OpenBSD: set RLIMIT_CORE to 0 and clear LD_* env vars.
	setCoreFileSizeLimitToZero()

	removeEnvVarsWithPrefix(ldPrefix)
}
