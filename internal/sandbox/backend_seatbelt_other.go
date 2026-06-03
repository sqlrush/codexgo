//go:build !darwin

package sandbox

// newSeatbeltBackend reports that the Seatbelt sandbox is only available on
// macOS. Mirrors SandboxTransformError::SeatbeltUnavailable for non-macOS
// platforms.
func newSeatbeltBackend() (Backend, error) {
	return nil, notImplementedBackendError(SandboxTypeMacosSeatbelt, "macOS Seatbelt sandbox is only available on macOS")
}
