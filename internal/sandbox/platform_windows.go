//go:build windows

package sandbox

// platformSandbox returns the Windows restricted-token sandbox when enabled, and
// otherwise reports that no platform sandbox is available.
func platformSandbox(windowsSandboxEnabled bool) (SandboxType, bool) {
	if windowsSandboxEnabled {
		return SandboxTypeWindowsRestrictedToken, true
	}
	return SandboxTypeNone, false
}
