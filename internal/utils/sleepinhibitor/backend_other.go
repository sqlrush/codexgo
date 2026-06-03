//go:build !darwin && !linux && !windows

package sleepinhibitor

// dummyBackend is the no-op backend used on platforms without a supported
// sleep-prevention mechanism. It mirrors the Rust `dummy` module: every
// operation succeeds and does nothing.
type dummyBackend struct{}

// newBackend returns the no-op backend for unsupported platforms.
func newBackend() backend {
	return &dummyBackend{}
}

func (d *dummyBackend) acquire() {}

func (d *dummyBackend) release() {}
