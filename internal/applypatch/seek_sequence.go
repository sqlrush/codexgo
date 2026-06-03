package applypatch

import "strings"

// seekSequence attempts to find the sequence of pattern lines within lines
// beginning at or after start. It returns the starting index of the match and
// true, or false if not found. It is a faithful port of Rust
// `seek_sequence::seek_sequence`.
//
// Matches are attempted with decreasing strictness: exact match, then ignoring
// trailing whitespace, then ignoring leading and trailing whitespace, and
// finally with common Unicode punctuation normalized to ASCII. When eof is
// true, the search begins at the position that would align pattern with the end
// of file (so end-of-file patterns are applied at the end), falling back to
// start otherwise.
//
// Special cases handled defensively, matching Codex:
//   - empty pattern -> returns (start, true) (no-op match)
//   - len(pattern) > len(lines) -> returns (0, false) (cannot match; avoids an
//     out-of-bounds panic that occurred pre-2025-04-12)
func seekSequence(lines, pattern []string, start int, eof bool) (int, bool) {
	if len(pattern) == 0 {
		return start, true
	}
	if len(pattern) > len(lines) {
		return 0, false
	}

	searchStart := start
	if eof && len(lines) >= len(pattern) {
		searchStart = len(lines) - len(pattern)
	}
	last := len(lines) - len(pattern)

	// Exact match first.
	for i := searchStart; i <= last; i++ {
		if equalLines(lines[i:i+len(pattern)], pattern) {
			return i, true
		}
	}
	// Then rstrip (trailing-whitespace-insensitive) match.
	if i, ok := matchWith(lines, pattern, searchStart, last, trimEnd); ok {
		return i, true
	}
	// Then trim both sides.
	if i, ok := matchWith(lines, pattern, searchStart, last, strings.TrimSpace); ok {
		return i, true
	}
	// Finally, the most permissive pass: normalize common Unicode punctuation to
	// ASCII so ASCII-authored diffs can match typographic source files.
	if i, ok := matchWith(lines, pattern, searchStart, last, normalisePunctuation); ok {
		return i, true
	}

	return 0, false
}

// equalLines reports whether two equal-length slices of strings are identical.
func equalLines(a, b []string) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// matchWith searches for a position where every pattern line equals the
// corresponding source line after applying transform to both.
func matchWith(lines, pattern []string, searchStart, last int, transform func(string) string) (int, bool) {
	for i := searchStart; i <= last; i++ {
		ok := true
		for pIdx, pat := range pattern {
			if transform(lines[i+pIdx]) != transform(pat) {
				ok = false
				break
			}
		}
		if ok {
			return i, true
		}
	}
	return 0, false
}

// trimEnd mirrors Rust `str::trim_end`: it removes trailing Unicode whitespace.
// Go's strings.TrimSpace / TrimRightFunc with unicode.IsSpace covers the same
// whitespace set Rust's `char::is_whitespace` uses for these purposes.
func trimEnd(s string) string {
	return strings.TrimRightFunc(s, isUnicodeWhitespace)
}

// isUnicodeWhitespace reports whether r is whitespace per Rust's notion of
// whitespace, matching Go's unicode.IsSpace for the relevant code points.
func isUnicodeWhitespace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ',
		0x85, 0xA0, 0x1680,
		0x2000, 0x2001, 0x2002, 0x2003, 0x2004, 0x2005, 0x2006, 0x2007,
		0x2008, 0x2009, 0x200A,
		0x2028, 0x2029, 0x202F, 0x205F, 0x3000:
		return true
	}
	return false
}

// normalisePunctuation is a faithful port of the `normalise` closure inside Rust
// `seek_sequence`. It trims the input, then maps a fixed set of typographic
// punctuation code points to their ASCII equivalents. Only the exact code points
// Codex special-cases are normalized; the code-point values below match the Rust
// match arms byte-for-byte.
func normalisePunctuation(s string) string {
	trimmed := strings.TrimSpace(s)
	var b strings.Builder
	b.Grow(len(trimmed))
	for _, c := range trimmed {
		switch c {
		// Various dash / hyphen code-points -> ASCII '-'
		// U+2010..U+2015, U+2212.
		case 0x2010, 0x2011, 0x2012, 0x2013, 0x2014, 0x2015, 0x2212:
			b.WriteByte('-')
		// Fancy single quotes -> '\'' (U+2018..U+201B).
		case 0x2018, 0x2019, 0x201A, 0x201B:
			b.WriteByte('\'')
		// Fancy double quotes -> '"' (U+201C..U+201F).
		case 0x201C, 0x201D, 0x201E, 0x201F:
			b.WriteByte('"')
		// Non-breaking space and other odd spaces -> normal space.
		case 0x00A0, 0x2002, 0x2003, 0x2004, 0x2005, 0x2006,
			0x2007, 0x2008, 0x2009, 0x200A, 0x202F, 0x205F, 0x3000:
			b.WriteByte(' ')
		default:
			b.WriteRune(c)
		}
	}
	return b.String()
}
