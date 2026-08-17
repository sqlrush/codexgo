package agentgraph

import "fmt"

// ErrorKind classifies an [Error], mirroring the variants of the Rust
// `AgentGraphStoreError` enum.
type ErrorKind int

const (
	// ErrorKindInvalidRequest indicates the caller supplied invalid request data.
	ErrorKindInvalidRequest ErrorKind = iota
	// ErrorKindInternal is the catch-all for implementation failures that do not
	// fit a more specific category.
	ErrorKindInternal
)

// Error is the error type shared by agent graph store implementations, mirroring
// the Rust `AgentGraphStoreError`.
type Error struct {
	// Kind classifies the failure.
	Kind ErrorKind
	// Message is the user-facing explanation.
	Message string
	// Cause is the wrapped underlying error, if any. It is unwrapped via [errors.Is]
	// and [errors.As].
	Cause error
}

// Error renders the message using the same wording as the Rust `thiserror`
// `#[error(...)]` attributes.
func (e *Error) Error() string {
	switch e.Kind {
	case ErrorKindInvalidRequest:
		return fmt.Sprintf("invalid agent graph store request: %s", e.Message)
	case ErrorKindInternal:
		return fmt.Sprintf("agent graph store internal error: %s", e.Message)
	default:
		return e.Message
	}
}

// Unwrap exposes the wrapped cause for [errors.Is] and [errors.As].
func (e *Error) Unwrap() error { return e.Cause }

// NewInvalidRequestError builds an [ErrorKindInvalidRequest] error.
func NewInvalidRequestError(format string, args ...any) *Error {
	return &Error{Kind: ErrorKindInvalidRequest, Message: fmt.Sprintf(format, args...)}
}

// NewInternalError wraps cause as an [ErrorKindInternal] error, mirroring the Rust
// `internal_error` helper that stringifies the source.
func NewInternalError(cause error, format string, args ...any) *Error {
	return &Error{
		Kind:    ErrorKindInternal,
		Message: fmt.Sprintf(format, args...),
		Cause:   cause,
	}
}
