package threadstore

import (
	"fmt"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// ErrorKind classifies a [Error], mirroring the variants of the Rust
// `ThreadStoreError` enum.
type ErrorKind int

const (
	// ErrorKindThreadNotFound indicates the requested thread does not exist in the
	// store.
	ErrorKindThreadNotFound ErrorKind = iota
	// ErrorKindInvalidRequest indicates the caller supplied invalid request data.
	ErrorKindInvalidRequest
	// ErrorKindConflict indicates the operation conflicted with current store
	// state.
	ErrorKindConflict
	// ErrorKindUnsupported indicates the store does not support this operation.
	ErrorKindUnsupported
	// ErrorKindInternal is the catch-all for implementation failures.
	ErrorKindInternal
)

// Error is the error type shared by thread-store implementations, mirroring the
// Rust `ThreadStoreError`.
type Error struct {
	// Kind classifies the failure.
	Kind ErrorKind
	// ThreadID is set for [ErrorKindThreadNotFound].
	ThreadID protocol.ThreadID
	// Operation is the stable operation name for [ErrorKindUnsupported].
	Operation string
	// Message is the user-facing explanation for the request/conflict/internal
	// variants.
	Message string
	// Cause is the wrapped underlying error, if any.
	Cause error
}

// Error renders the message using the same wording as the Rust `thiserror`
// `#[error(...)]` attributes.
func (e *Error) Error() string {
	switch e.Kind {
	case ErrorKindThreadNotFound:
		return fmt.Sprintf("thread %s not found", e.ThreadID)
	case ErrorKindInvalidRequest:
		return fmt.Sprintf("invalid thread-store request: %s", e.Message)
	case ErrorKindConflict:
		return fmt.Sprintf("thread-store conflict: %s", e.Message)
	case ErrorKindUnsupported:
		return fmt.Sprintf("thread-store unsupported operation: %s", e.Operation)
	case ErrorKindInternal:
		return fmt.Sprintf("thread-store internal error: %s", e.Message)
	default:
		return e.Message
	}
}

// Unwrap exposes the wrapped cause for [errors.Is] and [errors.As].
func (e *Error) Unwrap() error { return e.Cause }

// NewThreadNotFoundError builds an [ErrorKindThreadNotFound] error.
func NewThreadNotFoundError(threadID protocol.ThreadID) *Error {
	return &Error{Kind: ErrorKindThreadNotFound, ThreadID: threadID}
}

// NewInvalidRequestError builds an [ErrorKindInvalidRequest] error.
func NewInvalidRequestError(format string, args ...any) *Error {
	return &Error{Kind: ErrorKindInvalidRequest, Message: fmt.Sprintf(format, args...)}
}

// NewUnsupportedError builds an [ErrorKindUnsupported] error for the named
// operation (the Rust `ThreadStoreError::Unsupported { operation }`).
func NewUnsupportedError(operation string) *Error {
	return &Error{Kind: ErrorKindUnsupported, Operation: operation}
}

// unsupportedError is the package-internal spelling of [NewUnsupportedError].
func unsupportedError(operation string) *Error { return NewUnsupportedError(operation) }

// NewInternalError wraps cause as an [ErrorKindInternal] error.
func NewInternalError(cause error, format string, args ...any) *Error {
	return &Error{
		Kind:    ErrorKindInternal,
		Message: fmt.Sprintf(format, args...),
		Cause:   cause,
	}
}
