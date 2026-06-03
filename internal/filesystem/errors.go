package filesystem

import (
	"errors"
	"fmt"
)

// JSON-RPC error codes used by the app-server `fs/*` methods.
//
// Rust: app-server `error_code.rs`.
const (
	// InvalidRequestErrorCode is the JSON-RPC code for invalid requests.
	InvalidRequestErrorCode int64 = -32600
	// InternalErrorCode is the JSON-RPC code for internal errors.
	InternalErrorCode int64 = -32603
)

// JSONRPCError is the error payload returned by the app-server `fs/*` methods.
//
// Rust: `JSONRPCErrorError { code, message, data }`. The data field is always
// nil for filesystem errors, mirroring the Rust `error` helper.
type JSONRPCError struct {
	// Code is the JSON-RPC error code.
	Code int64
	// Message is the human-readable error message.
	Message string
}

// Error implements the error interface.
func (e *JSONRPCError) Error() string { return e.Message }

// invalidRequest builds an invalid-request JSON-RPC error (Rust
// `invalid_request`).
func invalidRequest(message string) *JSONRPCError {
	return &JSONRPCError{Code: InvalidRequestErrorCode, Message: message}
}

// internalError builds an internal JSON-RPC error (Rust `internal_error`).
func internalError(message string) *JSONRPCError {
	return &JSONRPCError{Code: InternalErrorCode, Message: message}
}

// InvalidInputError marks a filesystem error that the Rust crate raises with
// `io::ErrorKind::InvalidInput`. These are the errors that [MapFsError] maps to
// the JSON-RPC invalid-request code; all other errors map to internal-error.
//
// Wrapping these as a distinct type lets [MapFsError] reproduce the Rust kind
// check (`err.kind() == io::ErrorKind::InvalidInput`) without depending on
// platform-specific syscall error inspection.
type InvalidInputError struct {
	// Message is the underlying message.
	Message string
}

// Error implements the error interface.
func (e *InvalidInputError) Error() string { return e.Message }

// newInvalidInput builds an InvalidInputError with a fixed message.
func newInvalidInput(message string) *InvalidInputError {
	return &InvalidInputError{Message: message}
}

// MapFsError converts a filesystem operation error into the JSON-RPC error
// returned by the app-server `fs/*` methods.
//
// Rust `map_fs_error`:
//
//	if err.kind() == io::ErrorKind::InvalidInput {
//	    invalid_request(err.to_string())
//	} else {
//	    internal_error(err.to_string())
//	}
//
// Errors raised by this package via [InvalidInputError] are treated as the
// InvalidInput class; everything else (NotFound, PermissionDenied, raw OS
// errors, etc.) maps to internal-error, matching the Rust behavior where only
// the crate's own InvalidInput errors take the invalid-request branch.
//
// A nil error returns nil.
func MapFsError(err error) *JSONRPCError {
	if err == nil {
		return nil
	}
	var invalidInput *InvalidInputError
	if errors.As(err, &invalidInput) {
		return invalidRequest(err.Error())
	}
	return internalError(err.Error())
}

// wrapTaskFailure wraps an error from a background filesystem task, matching the
// Rust `io::Error::other(format!("filesystem task failed: {err}"))` used when a
// spawned blocking task fails (for example via context cancellation).
func wrapTaskFailure(err error) error {
	return fmt.Errorf("filesystem task failed: %w", err)
}
