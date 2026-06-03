//go:build !darwin && !linux && !windows

package sandbox

// platformSandbox reports that no platform sandbox is available on unsupported
// operating systems, mirroring the fall-through arm of get_platform_sandbox.
func platformSandbox(_ bool) (SandboxType, bool) {
	return SandboxTypeNone, false
}
