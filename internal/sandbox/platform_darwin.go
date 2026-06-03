//go:build darwin

package sandbox

// platformSandbox returns the macOS default sandbox: Seatbelt.
func platformSandbox(_ bool) (SandboxType, bool) {
	return SandboxTypeMacosSeatbelt, true
}
