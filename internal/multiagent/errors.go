package multiagent

import (
	"errors"
	"fmt"
)

// Sentinel errors mirror the relevant variants of the Rust `CodexErr` that the
// agent control plane raises. They are wrapped with %w so callers can match them
// with [errors.Is].
var (
	// ErrThreadEngineDropped mirrors the Rust upgrade failure
	// (`CodexErr::UnsupportedOperation("thread manager dropped")`) raised when the
	// thread engine is no longer available.
	ErrThreadEngineDropped = errors.New("multiagent: thread engine is unavailable")

	// ErrUnsupportedOperation mirrors `CodexErr::UnsupportedOperation`. It is the
	// base error for invalid agent references, exhausted nicknames, and duplicate
	// agent paths.
	ErrUnsupportedOperation = errors.New("multiagent: unsupported operation")

	// ErrFatal mirrors `CodexErr::Fatal` raised for malformed fork requests.
	ErrFatal = errors.New("multiagent: fatal error")
)

// AgentLimitError mirrors the Rust `CodexErr::AgentLimitReached { max_threads }`
// raised when a session has reached its sub-agent ceiling.
type AgentLimitError struct {
	// MaxThreads is the configured per-session sub-agent ceiling.
	MaxThreads uint64
}

// Error renders the limit message using the Rust wording.
func (e *AgentLimitError) Error() string {
	return fmt.Sprintf("multiagent: agent limit reached (max_threads=%d)", e.MaxThreads)
}

// unsupportedOperationf wraps [ErrUnsupportedOperation] with a formatted detail.
func unsupportedOperationf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrUnsupportedOperation, fmt.Sprintf(format, args...))
}

// fatalf wraps [ErrFatal] with a formatted detail.
func fatalf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrFatal, fmt.Sprintf(format, args...))
}
