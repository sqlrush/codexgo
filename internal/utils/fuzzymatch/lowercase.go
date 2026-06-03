package fuzzymatch

import "unicode"

// latinCapitalIWithDotAbove is U+0130 (İ), the only character whose full,
// unconditional Unicode lowercase mapping expands to more than one rune.
//
// Go's standard library exposes only the *simple* (1:1) lowercase mapping via
// unicode.ToLower, which maps İ to a single 'i' (U+0069). Rust's
// char::to_lowercase, which the reference implementation relies on, applies the
// *full* unconditional mapping from Unicode's SpecialCasing data and expands İ
// to 'i' (U+0069) followed by a combining dot above (U+0307).
//
// Replicating that single expansion is what makes this port behave identically
// to the Rust crate for inputs such as "İstanbul".
const latinCapitalIWithDotAbove = 'İ'

// combiningDotAbove is U+0307, the second rune produced by the full lowercase
// mapping of U+0130.
const combiningDotAbove = '̇'

// fullToLowerInto appends the full, unconditional Unicode lowercase mapping of
// r to dst and returns the extended slice.
//
// For every rune except U+0130 this is identical to the simple mapping returned
// by unicode.ToLower. U+0130 expands to two runes to match Rust's
// char::to_lowercase behavior. The function never mutates dst's existing
// elements; it only appends, returning a (possibly newly allocated) slice.
func fullToLowerInto(dst []rune, r rune) []rune {
	if r == latinCapitalIWithDotAbove {
		return append(dst, 'i', combiningDotAbove)
	}
	return append(dst, unicode.ToLower(r))
}

// lowerString returns the full, unconditional Unicode lowercase mapping of s as
// a slice of runes. It is the moral equivalent of Rust's
// `s.to_lowercase().chars().collect()`.
func lowerString(s string) []rune {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		out = fullToLowerInto(out, r)
	}
	return out
}
