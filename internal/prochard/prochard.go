// Package prochard implements process hardening, mirroring codex's
// process-hardening crate.
//
// It is designed to be called as early as possible in program startup
// (ideally the first thing in main) to perform several hardening steps:
//
//   - disabling core dumps,
//   - disabling ptrace attach on Linux and macOS,
//   - removing dangerous environment variables such as LD_PRELOAD and DYLD_*.
//
// On platforms where a particular step is not supported the corresponding
// operation is a graceful no-op.
//
// # Failure behavior
//
// PreMainHardening mirrors the upstream crate's "fail closed" posture: if a
// security-critical syscall fails, it prints an error to stderr and exits the
// process with a distinct, stable exit code (see the *ExitCode constants). This
// matches codex's pre-main behavior where a hardening failure must not be
// silently ignored. The lower-level functions (for example DisableProcessDumping)
// return errors instead, so callers that need softer handling can use those.
package prochard

import "strings"

// Exit codes used when a security-critical hardening step fails. These mirror
// the constants used by codex so behavior is drop-in compatible.
const (
	// PrctlFailedExitCode is used when prctl(PR_SET_DUMPABLE, 0) fails on Linux.
	PrctlFailedExitCode = 5

	// PtraceDenyAttachFailedExitCode is used when ptrace(PT_DENY_ATTACH) fails
	// on macOS.
	PtraceDenyAttachFailedExitCode = 6

	// SetRlimitCoreFailedExitCode is used when setrlimit(RLIMIT_CORE, 0) fails
	// on any Unix platform.
	SetRlimitCoreFailedExitCode = 7
)

// Environment variable prefixes that are stripped during hardening.
const (
	ldPrefix   = "LD_"
	dyldPrefix = "DYLD_"
)

// PreMainHardening performs all applicable process hardening steps for the
// current operating system.
//
// It should be called as early as possible in program startup. On unsupported
// platforms it is a no-op. If a security-critical step fails, the process is
// terminated with a stable exit code (see the *ExitCode constants).
func PreMainHardening() {
	preMainHardening()
}

// envKeysWithPrefix returns, in iteration order, the keys of the given
// "KEY=VALUE" environment entries whose key begins with prefix.
//
// entries are in the form returned by os.Environ. An entry without an '='
// separator is treated as a bare key (its whole string is the key), matching
// the conservative interpretation that such a variable should still be
// considered for removal if it matches the prefix.
//
// The input slice is not modified.
func envKeysWithPrefix(entries []string, prefix string) []string {
	keys := make([]string, 0)
	for _, entry := range entries {
		key := entry
		if i := strings.IndexByte(entry, '='); i >= 0 {
			key = entry[:i]
		}
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	return keys
}
