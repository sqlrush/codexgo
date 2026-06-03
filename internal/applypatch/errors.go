package applypatch

import (
	"errors"
	"fmt"
)

// ParseError is returned when a patch envelope cannot be parsed. It mirrors the
// Rust `ParseError` enum: either the patch boundaries/preamble are invalid, or a
// specific hunk is malformed (in which case a 1-based line number is reported).
//
// Use [errors.As] to distinguish the two kinds via [ParseError.Kind] or by
// inspecting [ParseError.LineNumber] (zero for InvalidPatch errors).
type ParseError struct {
	// Kind identifies which variant of the Rust enum this is.
	Kind ParseErrorKind
	// Message is the human-readable detail.
	Message string
	// LineNumber is the 1-based line number for InvalidHunk errors. It is 0 for
	// InvalidPatch errors.
	LineNumber int
}

// ParseErrorKind enumerates the variants of [ParseError], mirroring the Rust
// `ParseError` enum.
type ParseErrorKind int

const (
	// InvalidPatch corresponds to Rust `ParseError::InvalidPatchError`.
	InvalidPatch ParseErrorKind = iota
	// InvalidHunk corresponds to Rust `ParseError::InvalidHunkError`.
	InvalidHunk
)

// Error renders the error to match Codex's `Display` output:
//
//	invalid patch: {message}
//	invalid hunk at line {line_number}, {message}
func (e *ParseError) Error() string {
	switch e.Kind {
	case InvalidHunk:
		return fmt.Sprintf("invalid hunk at line %d, %s", e.LineNumber, e.Message)
	default:
		return fmt.Sprintf("invalid patch: %s", e.Message)
	}
}

// newInvalidPatchError constructs an InvalidPatch [ParseError].
func newInvalidPatchError(message string) *ParseError {
	return &ParseError{Kind: InvalidPatch, Message: message}
}

// newInvalidHunkError constructs an InvalidHunk [ParseError] at the given
// 1-based line number.
func newInvalidHunkError(message string, lineNumber int) *ParseError {
	return &ParseError{Kind: InvalidHunk, Message: message, LineNumber: lineNumber}
}

// Sentinel errors mirroring the non-IO, non-parse variants of Rust's
// `ApplyPatchError`.
var (
	// ErrImplicitInvocation mirrors Rust `ApplyPatchError::ImplicitInvocation`:
	// a raw patch body was provided without an explicit `apply_patch` call.
	ErrImplicitInvocation = errors.New(
		"patch detected without explicit call to apply_patch. Rerun as [\"apply_patch\", \"<patch>\"]",
	)
)

// ComputeReplacementsError mirrors Rust
// `ApplyPatchError::ComputeReplacements`: an error computing replacements while
// applying update chunks (for example a context or old-lines block that cannot
// be located in the target file).
type ComputeReplacementsError struct {
	Message string
}

func (e *ComputeReplacementsError) Error() string { return e.Message }

// IOError mirrors Rust `ApplyPatchError::IoError` / the `IoError` struct: an
// underlying I/O failure with human-readable context. It wraps the underlying
// error so [errors.Is]/[errors.As] continue to work.
type IOError struct {
	Context string
	Err     error
}

func (e *IOError) Error() string { return fmt.Sprintf("%s: %s", e.Context, e.Err) }

// Unwrap exposes the wrapped error.
func (e *IOError) Unwrap() error { return e.Err }
