package execpolicy

import (
	"fmt"
	"strings"
)

// ErrorKind classifies a policy [Error]. It mirrors the variants of the Rust
// `execpolicy::Error` enum.
type ErrorKind int

const (
	// ErrInvalidDecision corresponds to Rust `Error::InvalidDecision`.
	ErrInvalidDecision ErrorKind = iota
	// ErrInvalidPattern corresponds to Rust `Error::InvalidPattern`.
	ErrInvalidPattern
	// ErrInvalidExample corresponds to Rust `Error::InvalidExample`.
	ErrInvalidExample
	// ErrInvalidRule corresponds to Rust `Error::InvalidRule`.
	ErrInvalidRule
	// ErrExampleDidNotMatch corresponds to Rust `Error::ExampleDidNotMatch`.
	ErrExampleDidNotMatch
	// ErrExampleDidMatch corresponds to Rust `Error::ExampleDidMatch`.
	ErrExampleDidMatch
	// ErrStarlark corresponds to Rust `Error::Starlark`.
	ErrStarlark
)

// TextPosition is a one-based line/column position within a policy file. It
// mirrors Rust's `TextPosition`.
type TextPosition struct {
	Line   int
	Column int
}

// TextRange is a half-open span between two [TextPosition] values. It mirrors
// Rust's `TextRange`.
type TextRange struct {
	Start TextPosition
	End   TextPosition
}

// ErrorLocation identifies where in a policy file an error occurred. It mirrors
// Rust's `ErrorLocation`.
type ErrorLocation struct {
	Path  string
	Range TextRange
}

// Error is the error type returned by the policy parser and loader. It mirrors
// the Rust `execpolicy::Error` enum: a single struct discriminated by [Kind]
// carries the fields relevant to each variant.
type Error struct {
	// Kind selects which variant this error represents.
	Kind ErrorKind

	// Message holds the free-form text for the InvalidDecision,
	// InvalidPattern, InvalidExample, and InvalidRule variants.
	Message string

	// Rules holds the rendered rules for the ExampleDidNotMatch variant.
	Rules []string
	// Examples holds the unmatched examples for the ExampleDidNotMatch variant.
	Examples []string

	// Rule holds the rendered rule for the ExampleDidMatch variant.
	Rule string
	// Example holds the matched example for the ExampleDidMatch variant.
	Example string

	// Location is the optional source span. It applies to the example
	// validation variants and the Starlark variant.
	Location *ErrorLocation

	// Wrapped is the underlying error for the Starlark variant.
	Wrapped error
}

// Error formats the policy error using the same wording as the Rust
// `#[error(...)]` attributes so that callers matching on substrings behave
// identically.
func (e *Error) Error() string {
	switch e.Kind {
	case ErrInvalidDecision:
		return "invalid decision: " + e.Message
	case ErrInvalidPattern:
		return "invalid pattern element: " + e.Message
	case ErrInvalidExample:
		return "invalid example: " + e.Message
	case ErrInvalidRule:
		return "invalid rule: " + e.Message
	case ErrExampleDidNotMatch:
		return fmt.Sprintf(
			"expected every example to match at least one rule. rules: %s; unmatched examples: %s",
			renderStringSlice(e.Rules),
			renderStringSlice(e.Examples),
		)
	case ErrExampleDidMatch:
		return fmt.Sprintf("expected example to not match rule `%s`: %s", e.Rule, e.Example)
	case ErrStarlark:
		return "starlark error: " + errString(e.Wrapped)
	default:
		return "execpolicy: unknown error"
	}
}

// Unwrap exposes the wrapped Starlark error for errors.Is/errors.As.
func (e *Error) Unwrap() error {
	return e.Wrapped
}

// withLocation returns a copy of the error annotated with location, mirroring
// Rust's `Error::with_location`. Only the example-validation variants without a
// prior location are annotated; all other errors are returned unchanged.
func (e *Error) withLocation(location ErrorLocation) *Error {
	switch e.Kind {
	case ErrExampleDidNotMatch, ErrExampleDidMatch:
		if e.Location != nil {
			return e
		}
		clone := *e
		loc := location
		clone.Location = &loc
		return &clone
	default:
		return e
	}
}

// errString renders a possibly-nil error as a string.
func errString(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

// renderStringSlice formats a slice of strings using Rust's `{:?}` debug
// representation for a `Vec<String>`, e.g. `["a", "b"]`. This keeps error
// messages byte-identical to the Rust implementation.
func renderStringSlice(values []string) string {
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = fmt.Sprintf("%q", value)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
