// Package fuzzymatch provides a simple, case-insensitive subsequence matcher
// used for fuzzy filtering in @mention lookups and pickers.
//
// It is a faithful, drop-in-compatible Go port of the Rust crate
// codex-utils-fuzzy-match. Given a haystack and a needle it reports the
// character positions (in the original haystack) of the matched characters
// together with a score where smaller is better.
//
// Unicode correctness: matching is performed on full, unconditional lowercase
// mappings of both the haystack and the needle, while a mapping back to the
// original character index of the haystack is maintained. This lets callers use
// the returned indices directly with rune-by-rune highlighting consumers even
// when lowercasing expands certain characters (e.g. İ → i + combining dot
// above).
package fuzzymatch

import (
	"math"
	"sort"
)

// scoreEmptyNeedle is the score returned for an empty needle. It mirrors the
// Rust implementation's use of i32::MAX (the worst possible score, so empty
// needles rank last) while still reporting a successful match.
const scoreEmptyNeedle = math.MaxInt32

// prefixBonus is subtracted from the score when the first matched character is
// at the very start of the (lowered) haystack, strongly rewarding prefix
// matches.
const prefixBonus = 100

// Match is the successful result of FuzzyMatch.
type Match struct {
	// Indices holds the character (rune) positions in the ORIGINAL haystack of
	// the matched characters, sorted ascending and de-duplicated.
	Indices []int
	// Score ranks the match; smaller is better.
	Score int
}

// FuzzyMatch reports whether needle is a case-insensitive subsequence of
// haystack and, if so, returns the matched original-haystack character indices
// together with a score (smaller is better). The boolean result reports whether
// a match was found; it corresponds to the Rust API returning Option.
//
// Behavioral contract (identical to the reference Rust crate):
//   - An empty needle always matches, with no indices and Score == math.MaxInt32.
//   - Matching is case-insensitive using full unconditional Unicode lowercase
//     mappings.
//   - The score is the span between the first and last matched lowered
//     characters minus the needle length, clamped to be non-negative, with an
//     additional prefix bonus of -100 applied when the first match is at the
//     start of the lowered haystack.
//
// FuzzyMatch never mutates its arguments and returns freshly allocated values.
func FuzzyMatch(haystack, needle string) (Match, bool) {
	if needle == "" {
		return Match{Indices: []int{}, Score: scoreEmptyNeedle}, true
	}

	loweredChars, loweredToOrig := lowerWithIndexMap(haystack)
	loweredNeedle := lowerString(needle)

	indices, lastLowerPos, ok := matchSubsequence(loweredChars, loweredToOrig, loweredNeedle)
	if !ok {
		return Match{}, false
	}

	firstLowerPos := firstLoweredPositionForOrig(loweredToOrig, indices)
	score := computeScore(firstLowerPos, lastLowerPos, len(loweredNeedle))

	return Match{Indices: dedupSorted(indices), Score: score}, true
}

// lowerWithIndexMap returns the full lowercase rune mapping of haystack along
// with a parallel slice mapping each lowered rune back to the original
// character (rune) index in haystack.
func lowerWithIndexMap(haystack string) (lowered []rune, loweredToOrig []int) {
	lowered = make([]rune, 0, len(haystack))
	loweredToOrig = make([]int, 0, len(haystack))
	origIdx := 0
	for _, r := range haystack {
		before := len(lowered)
		lowered = fullToLowerInto(lowered, r)
		for range lowered[before:] {
			loweredToOrig = append(loweredToOrig, origIdx)
		}
		origIdx++
	}
	return lowered, loweredToOrig
}

// matchSubsequence walks the lowered haystack greedily, matching each lowered
// needle rune in order. It returns the matched original indices (in match
// order, possibly with duplicates), the lowered position of the final match,
// and whether every needle rune was matched.
func matchSubsequence(loweredChars []rune, loweredToOrig []int, loweredNeedle []rune) (origIndices []int, lastLowerPos int, ok bool) {
	origIndices = make([]int, 0, len(loweredNeedle))
	lastLowerPos = 0
	cur := 0
	for _, nc := range loweredNeedle {
		foundAt := -1
		for cur < len(loweredChars) {
			if loweredChars[cur] == nc {
				foundAt = cur
				cur++
				break
			}
			cur++
		}
		if foundAt < 0 {
			return nil, 0, false
		}
		origIndices = append(origIndices, loweredToOrig[foundAt])
		lastLowerPos = foundAt
	}
	return origIndices, lastLowerPos, true
}

// firstLoweredPositionForOrig finds the lowered position of the first lowered
// rune that maps back to the first matched original index. This mirrors the
// Rust implementation, which re-scans for the earliest lowered slot of the
// first matched original character so that the prefix bonus is computed
// consistently across multi-rune lowercase expansions.
func firstLoweredPositionForOrig(loweredToOrig []int, matchedOrigIndices []int) int {
	if len(matchedOrigIndices) == 0 {
		return 0
	}
	target := matchedOrigIndices[0]
	for pos, oi := range loweredToOrig {
		if oi == target {
			return pos
		}
	}
	return 0
}

// computeScore reproduces the reference scoring: the inclusive window between
// the first and last lowered match positions minus the needle length, clamped
// to be non-negative, with a prefix bonus when the first match is at position 0.
func computeScore(firstLowerPos, lastLowerPos, neededLen int) int {
	window := (lastLowerPos - firstLowerPos + 1) - neededLen
	score := window
	if score < 0 {
		score = 0
	}
	if firstLowerPos == 0 {
		score -= prefixBonus
	}
	return score
}

// dedupSorted returns a new slice containing the sorted, de-duplicated elements
// of in. The input slice is not mutated.
func dedupSorted(in []int) []int {
	if len(in) == 0 {
		return []int{}
	}
	cp := make([]int, len(in))
	copy(cp, in)
	sort.Ints(cp)
	out := cp[:1]
	for _, v := range cp[1:] {
		if v != out[len(out)-1] {
			out = append(out, v)
		}
	}
	return out
}
