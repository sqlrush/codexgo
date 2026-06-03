package readiness

import "errors"

// ReadinessError is the error type returned by the readiness API. It mirrors the
// Rust ReadinessError enum.
//
// Callers may compare returned errors against the exported sentinel values using
// errors.Is.
var (
	// ErrTokenLockFailed indicates the internal token lock could not be
	// acquired within the lock timeout. It corresponds to the Rust
	// ReadinessError::TokenLockFailed variant.
	ErrTokenLockFailed = errors.New("Failed to acquire readiness token lock")

	// ErrFlagAlreadyReady indicates the flag is already ready and therefore
	// cannot be subscribed to. It corresponds to the Rust
	// ReadinessError::FlagAlreadyReady variant.
	ErrFlagAlreadyReady = errors.New("Flag is already ready. Impossible to subscribe")
)
