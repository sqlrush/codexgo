//go:build !windows

package sandbox

// newWindowsBackend reports that the Windows restricted-token sandbox is only
// available on Windows. Mirrors the platform gate for
// SandboxType::WindowsRestrictedToken on non-Windows hosts.
func newWindowsBackend() (Backend, error) {
	return nil, notImplementedBackendError(SandboxTypeWindowsRestrictedToken, "Windows restricted-token sandbox is only available on Windows")
}
