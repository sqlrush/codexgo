package template

import (
	"sort"
	"strings"
)

// Delimiter constants used throughout parsing. Mirrors the Rust string literals
// so escaping and offset arithmetic stay identical.
const (
	openDelim       = "{{"
	closeDelim      = "}}"
	escapedOpen     = "{{{{"
	escapedClose    = "}}}}"
	openDelimLen    = len(openDelim)
	closeDelimLen   = len(closeDelim)
	escapedOpenLen  = len(escapedOpen)
	escapedCloseLen = len(escapedClose)
)

// Template is a parsed, reusable template. The zero value is not usable; obtain
// a Template via Parse. Templates are immutable once parsed and safe for
// concurrent reads.
type Template struct {
	// placeholders is the sorted, deduplicated set of placeholder names, in the
	// same order the Rust BTreeSet would yield them.
	placeholders []string
	segments     []segment
}

// Parse compiles source into a reusable Template, validating the syntax and
// returning a *ParseError describing the first malformed construct found.
func Parse(source string) (*Template, error) {
	var placeholderSet = make(map[string]struct{})
	var segments []segment
	literalStart := 0
	cursor := 0

	for cursor < len(source) {
		rest := source[cursor:]

		// Escaped literal open delimiter "{{{{" -> "{{".
		if strings.HasPrefix(rest, escapedOpen) {
			segments = appendLiteral(segments, source[literalStart:cursor])
			segments = appendLiteral(segments, openDelim)
			cursor += escapedOpenLen
			literalStart = cursor
			continue
		}

		// Escaped literal close delimiter "}}}}" -> "}}".
		if strings.HasPrefix(rest, escapedClose) {
			segments = appendLiteral(segments, source[literalStart:cursor])
			segments = appendLiteral(segments, closeDelim)
			cursor += escapedCloseLen
			literalStart = cursor
			continue
		}

		// Placeholder open delimiter.
		if strings.HasPrefix(rest, openDelim) {
			segments = appendLiteral(segments, source[literalStart:cursor])
			name, next, err := parsePlaceholder(source, cursor)
			if err != nil {
				return nil, err
			}
			placeholderSet[name] = struct{}{}
			segments = append(segments, segment{kind: segmentPlaceholder, text: name})
			cursor = next
			literalStart = cursor
			continue
		}

		// Unescaped close delimiter with no matching open: error.
		if strings.HasPrefix(rest, closeDelim) {
			return nil, &ParseError{kind: parseUnmatchedClosingDelimiter, Start: cursor}
		}

		// Advance by one UTF-8 rune so byte offsets stay aligned with the
		// reference implementation's char-based cursor advancement.
		_, size := decodeRune(source, cursor)
		if size == 0 {
			break
		}
		cursor += size
	}

	segments = appendLiteral(segments, source[literalStart:])

	placeholders := make([]string, 0, len(placeholderSet))
	for name := range placeholderSet {
		placeholders = append(placeholders, name)
	}
	sort.Strings(placeholders)

	return &Template{placeholders: placeholders, segments: segments}, nil
}

// Placeholders returns the sorted, unique set of placeholder names referenced by
// the template. A fresh slice is returned on each call so callers cannot mutate
// the template's internal state.
func (t *Template) Placeholders() []string {
	out := make([]string, len(t.placeholders))
	copy(out, t.placeholders)
	return out
}

// Render substitutes the supplied variables into the template, returning the
// rendered string. Rendering is strict: every placeholder must have a value,
// every supplied value must be used, and no name may be supplied twice.
// Failures return a *RenderError. The variables map is treated as read-only.
func (t *Template) Render(variables map[string]string) (string, error) {
	// Every placeholder must have a value.
	for _, placeholder := range t.placeholders {
		if _, ok := variables[placeholder]; !ok {
			return "", &RenderError{kind: renderMissingValue, Name: placeholder}
		}
	}

	// Every supplied value must be used. Iterate in sorted order so the first
	// reported ExtraValue is deterministic, matching the Rust BTreeMap.
	names := make([]string, 0, len(variables))
	for name := range variables {
		names = append(names, name)
	}
	sort.Strings(names)
	placeholderLookup := make(map[string]struct{}, len(t.placeholders))
	for _, p := range t.placeholders {
		placeholderLookup[p] = struct{}{}
	}
	for _, name := range names {
		if _, ok := placeholderLookup[name]; !ok {
			return "", &RenderError{kind: renderExtraValue, Name: name}
		}
	}

	var b strings.Builder
	for _, seg := range t.segments {
		switch seg.kind {
		case segmentLiteral:
			b.WriteString(seg.text)
		case segmentPlaceholder:
			value, ok := variables[seg.text]
			if !ok {
				return "", &RenderError{kind: renderMissingValue, Name: seg.text}
			}
			b.WriteString(value)
		}
	}
	return b.String(), nil
}

// RenderPairs is an ordered-input variant of Render that accepts variables as a
// slice of name/value pairs. It rejects duplicate names with a
// *RenderError (DuplicateValue), matching the Rust render's behavior when a
// caller passes the same key twice in an iterator. The supplied slice is not
// mutated.
func (t *Template) RenderPairs(pairs []Pair) (string, error) {
	variables, err := buildVariableMap(pairs)
	if err != nil {
		return "", err
	}
	return t.Render(variables)
}

// Pair is a single name/value binding used by RenderPairs and the package-level
// Render function. It preserves the ability to detect duplicate names, which a
// plain map cannot express.
type Pair struct {
	Name  string
	Value string
}

// buildVariableMap converts ordered pairs into a map, returning a
// *RenderError (DuplicateValue) on the first repeated name. Mirrors the Rust
// build_variable_map helper.
func buildVariableMap(pairs []Pair) (map[string]string, error) {
	m := make(map[string]string, len(pairs))
	for _, p := range pairs {
		if _, exists := m[p.Name]; exists {
			return nil, &RenderError{kind: renderDuplicateValue, Name: p.Name}
		}
		m[p.Name] = p.Value
	}
	return m, nil
}

// Render parses template and renders it with the supplied pairs in one step.
// Any error is wrapped in a *Error whose Cause is the underlying *ParseError or
// *RenderError, mirroring the Rust top-level render function and its
// TemplateError return type.
func Render(template string, pairs []Pair) (string, error) {
	parsed, err := Parse(template)
	if err != nil {
		return "", &Error{Cause: err}
	}
	rendered, err := parsed.RenderPairs(pairs)
	if err != nil {
		return "", &Error{Cause: err}
	}
	return rendered, nil
}

// RenderMap is the map-based companion to Render: it parses template and renders
// it with the supplied map in one step, wrapping any error in a *Error. Because
// a map cannot carry duplicate keys, DuplicateValue can never be produced by
// this entry point.
func RenderMap(template string, variables map[string]string) (string, error) {
	parsed, err := Parse(template)
	if err != nil {
		return "", &Error{Cause: err}
	}
	rendered, err := parsed.Render(variables)
	if err != nil {
		return "", &Error{Cause: err}
	}
	return rendered, nil
}
