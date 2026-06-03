//go:build !linux

package sandbox

// newLandlockBackend reports that the native Linux sandbox is only available on
// Linux. Mirrors the platform gate for SandboxType::LinuxSeccomp on non-Linux
// hosts.
func newLandlockBackend() (Backend, error) {
	return nil, notImplementedBackendError(SandboxTypeLinuxSeccomp, "Linux seccomp/Landlock sandbox is only available on Linux")
}
