package tools

import "fmt"

// FunctionCallErrorKind distinguishes recoverable model-facing errors from fatal
// ones. Mirrors the Rust `FunctionCallError` enum variants.
type FunctionCallErrorKind int

// FunctionCallErrorKind variants.
const (
	// FunctionCallErrorRespondToModel is a recoverable error whose message is
	// surfaced back to the model verbatim.
	FunctionCallErrorRespondToModel FunctionCallErrorKind = iota
	// FunctionCallErrorFatal is an unrecoverable error.
	FunctionCallErrorFatal
)

// FunctionCallError is an error returned while executing a model-visible tool
// invocation. It mirrors Rust `FunctionCallError`: the `RespondToModel` variant
// formats as its message, while `Fatal` is prefixed with "Fatal error: ".
type FunctionCallError struct {
	Kind    FunctionCallErrorKind
	Message string
}

// RespondToModelError constructs a recoverable, model-facing error.
func RespondToModelError(message string) *FunctionCallError {
	return &FunctionCallError{Kind: FunctionCallErrorRespondToModel, Message: message}
}

// FatalError constructs a fatal error.
func FatalError(message string) *FunctionCallError {
	return &FunctionCallError{Kind: FunctionCallErrorFatal, Message: message}
}

// Error implements the error interface, matching the Rust `Display` output.
func (e *FunctionCallError) Error() string {
	switch e.Kind {
	case FunctionCallErrorFatal:
		return fmt.Sprintf("Fatal error: %s", e.Message)
	default:
		return e.Message
	}
}
