//go:build linux

package sandbox

// platformSandbox returns the Linux default sandbox: seccomp/Landlock.
func platformSandbox(_ bool) (SandboxType, bool) {
	return SandboxTypeLinuxSeccomp, true
}
