// Package template provides minimal, strict string templating for prompt and
// text assets. It is a faithful Go port of the codex-utils-template Rust crate
// used by Codex 0.136.0 for skills/login/models prompts.
//
// Supported syntax:
//
//   - {{ name }} placeholder interpolation (surrounding whitespace is trimmed)
//   - {{{{ for a literal {{
//   - }}}} for a literal }}
//
// The package is intentionally strict:
//
//   - parsing fails on malformed placeholders
//   - rendering fails on missing values
//   - rendering fails on duplicate values
//   - rendering fails on extra values not used by the template
package template

import (
	"errors"
	"fmt"
)

// parseErrorKind enumerates the categories of parse failure. It mirrors the
// variants of the Rust TemplateParseError enum so that error reporting stays
// behaviorally identical.
type parseErrorKind int

const (
	// parseEmptyPlaceholder indicates a placeholder that, after trimming
	// whitespace, contained no name (for example "{{   }}").
	parseEmptyPlaceholder parseErrorKind = iota
	// parseNestedPlaceholder indicates a placeholder that contained a nested
	// "{{" before its closing "}}".
	parseNestedPlaceholder
	// parseUnmatchedClosingDelimiter indicates a stray "}}" that does not
	// close any placeholder and was not escaped as "}}}}".
	parseUnmatchedClosingDelimiter
	// parseUnterminatedPlaceholder indicates a placeholder opened with "{{"
	// that was never closed by a matching "}}".
	parseUnterminatedPlaceholder
)

// ParseError describes a failure to parse a template. Start is the zero-based
// byte offset into the source where the offending construct begins, matching
// the byte offsets reported by the reference Rust implementation.
type ParseError struct {
	kind  parseErrorKind
	Start int
}

// Error implements the error interface, producing messages byte-identical to
// the Rust TemplateParseError Display implementation.
func (e *ParseError) Error() string {
	switch e.kind {
	case parseEmptyPlaceholder:
		return fmt.Sprintf("template placeholder at byte %d is empty", e.Start)
	case parseNestedPlaceholder:
		return fmt.Sprintf(
			"template placeholder starting at byte %d contains a nested `{{`",
			e.Start,
		)
	case parseUnmatchedClosingDelimiter:
		return fmt.Sprintf("template contains an unmatched `}}` at byte %d", e.Start)
	case parseUnterminatedPlaceholder:
		return fmt.Sprintf(
			"template placeholder starting at byte %d is missing `}}`",
			e.Start,
		)
	default:
		return fmt.Sprintf("unknown template parse error at byte %d", e.Start)
	}
}

// IsEmptyPlaceholder reports whether the error is an empty-placeholder failure.
func (e *ParseError) IsEmptyPlaceholder() bool { return e.kind == parseEmptyPlaceholder }

// IsNestedPlaceholder reports whether the error is a nested-placeholder failure.
func (e *ParseError) IsNestedPlaceholder() bool { return e.kind == parseNestedPlaceholder }

// IsUnmatchedClosingDelimiter reports whether the error is an unmatched "}}"
// failure.
func (e *ParseError) IsUnmatchedClosingDelimiter() bool {
	return e.kind == parseUnmatchedClosingDelimiter
}

// IsUnterminatedPlaceholder reports whether the error is an unterminated
// placeholder failure.
func (e *ParseError) IsUnterminatedPlaceholder() bool {
	return e.kind == parseUnterminatedPlaceholder
}

// renderErrorKind enumerates the categories of render failure, mirroring the
// Rust TemplateRenderError enum.
type renderErrorKind int

const (
	// renderDuplicateValue indicates the same variable name was supplied more
	// than once.
	renderDuplicateValue renderErrorKind = iota
	// renderExtraValue indicates a supplied variable that the template never
	// references.
	renderExtraValue
	// renderMissingValue indicates a template placeholder for which no value
	// was supplied.
	renderMissingValue
)

// RenderError describes a failure to render a parsed template. Name identifies
// the variable or placeholder involved.
type RenderError struct {
	kind renderErrorKind
	Name string
}

// Error implements the error interface, producing messages byte-identical to
// the Rust TemplateRenderError Display implementation.
func (e *RenderError) Error() string {
	switch e.kind {
	case renderDuplicateValue:
		return fmt.Sprintf("template value `%s` was provided more than once", e.Name)
	case renderExtraValue:
		return fmt.Sprintf("template value `%s` is not used by this template", e.Name)
	case renderMissingValue:
		return fmt.Sprintf("template placeholder `%s` is missing a value", e.Name)
	default:
		return fmt.Sprintf("unknown template render error for `%s`", e.Name)
	}
}

// IsDuplicateValue reports whether the error is a duplicate-value failure.
func (e *RenderError) IsDuplicateValue() bool { return e.kind == renderDuplicateValue }

// IsExtraValue reports whether the error is an extra-value failure.
func (e *RenderError) IsExtraValue() bool { return e.kind == renderExtraValue }

// IsMissingValue reports whether the error is a missing-value failure.
func (e *RenderError) IsMissingValue() bool { return e.kind == renderMissingValue }

// Error is the unified error type returned by the package-level Render
// function. It wraps either a *ParseError or a *RenderError, mirroring the Rust
// TemplateError enum. Use errors.As to extract the wrapped cause.
type Error struct {
	// Cause is the wrapped *ParseError or *RenderError.
	Cause error
}

// Error implements the error interface by delegating to the wrapped cause, so
// the message matches the Rust TemplateError Display implementation.
func (e *Error) Error() string {
	if e.Cause == nil {
		return "template error"
	}
	return e.Cause.Error()
}

// Unwrap returns the wrapped cause so that errors.As and errors.Is can match
// the underlying *ParseError or *RenderError.
func (e *Error) Unwrap() error { return e.Cause }

// IsParse reports whether the wrapped cause is a parse error.
func (e *Error) IsParse() bool {
	var pe *ParseError
	return errors.As(e.Cause, &pe)
}

// IsRender reports whether the wrapped cause is a render error.
func (e *Error) IsRender() bool {
	var re *RenderError
	return errors.As(e.Cause, &re)
}
