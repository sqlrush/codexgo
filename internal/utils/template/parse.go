package template

import (
	"strings"
	"unicode/utf8"
)

// parsePlaceholder parses a placeholder beginning at byte offset start, where
// source[start:] is known to begin with "{{". It returns the trimmed
// placeholder name and the byte offset immediately after the closing "}}".
//
// Errors mirror the Rust parse_placeholder helper:
//   - a nested "{{" before the close yields a NestedPlaceholder error;
//   - an empty (whitespace-only) name yields an EmptyPlaceholder error;
//   - reaching end-of-input without "}}" yields an UnterminatedPlaceholder
//     error.
//
// All reported byte offsets are relative to the placeholder's opening "{{"
// (start), exactly as the reference implementation reports them.
func parsePlaceholder(source string, start int) (string, int, error) {
	placeholderStart := start + openDelimLen
	cursor := placeholderStart

	for cursor < len(source) {
		rest := source[cursor:]

		if strings.HasPrefix(rest, openDelim) {
			return "", 0, &ParseError{kind: parseNestedPlaceholder, Start: start}
		}

		if strings.HasPrefix(rest, closeDelim) {
			name := strings.TrimSpace(source[placeholderStart:cursor])
			if name == "" {
				return "", 0, &ParseError{kind: parseEmptyPlaceholder, Start: start}
			}
			return name, cursor + closeDelimLen, nil
		}

		_, size := decodeRune(source, cursor)
		if size == 0 {
			break
		}
		cursor += size
	}

	return "", 0, &ParseError{kind: parseUnterminatedPlaceholder, Start: start}
}

// decodeRune decodes the UTF-8 rune at source[offset:], returning the rune and
// its byte width. A width of 0 signals end-of-input. Invalid UTF-8 advances by
// one byte (width 1) so the scanner always makes progress, matching how the
// reference cursor never stalls.
func decodeRune(source string, offset int) (rune, int) {
	if offset >= len(source) {
		return 0, 0
	}
	r, size := utf8.DecodeRuneInString(source[offset:])
	if r == utf8.RuneError && size == 0 {
		return 0, 0
	}
	return r, size
}
