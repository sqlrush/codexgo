package tui

import (
	"sort"
	"strings"

	"github.com/mattn/go-runewidth"
)

// unicodeWidthCond is a fixed display-width condition that matches the Rust
// `unicode-width` crate codex uses: ambiguous-width characters (e.g. the "…"
// ellipsis, box-drawing) are treated as NARROW (width 1), independent of the
// process locale. runewidth's DefaultCondition auto-detects East Asian Width from
// the environment, which would mis-size the session-header card; the card/path
// math must use this fixed condition to stay byte-identical to codex.
var unicodeWidthCond = func() *runewidth.Condition {
	c := runewidth.NewCondition()
	c.EastAsianWidth = false
	return c
}()

// uwidth returns the display width of s under the fixed unicode-width policy.
func uwidth(s string) int { return unicodeWidthCond.StringWidth(s) }

// urunewidth returns the display width of r under the fixed unicode-width policy.
func urunewidth(r rune) int { return unicodeWidthCond.RuneWidth(r) }

// centerTruncatePath is a faithful port of
// codex-rs/tui/src/text_formatting.rs::center_truncate_path. It shortens a path
// to at most maxWidth display cells while preserving the most informative leading
// and trailing path segments, inserting "…" where segments are dropped and
// front-truncating individual segments (with a leading "…") only as a last
// resort.
//
// The byte-for-byte behavior matters for the session-header card: codex sizes the
// card border to the widest content row, which is the truncated directory; to
// reproduce the card's exact width and characters, codexgo must truncate the
// directory the same way codex does for the same path.
func centerTruncatePath(path string, maxWidth int) string {
	if maxWidth == 0 {
		return ""
	}
	if uwidth(path) <= maxWidth {
		return path
	}

	const sep = "/"
	hasLeadingSep := strings.HasPrefix(path, sep)
	hasTrailingSep := strings.HasSuffix(path, sep)
	rawSegments := strings.Split(path, sep)
	if hasLeadingSep && len(rawSegments) > 0 && rawSegments[0] == "" {
		rawSegments = rawSegments[1:]
	}
	if hasTrailingSep && len(rawSegments) > 0 && rawSegments[len(rawSegments)-1] == "" {
		rawSegments = rawSegments[:len(rawSegments)-1]
	}

	if len(rawSegments) == 0 {
		if hasLeadingSep {
			if uwidth(sep) <= maxWidth {
				return sep
			}
		}
		return "…"
	}

	segmentCount := len(rawSegments)

	assemble := func(leading bool, segments []truncSegment) string {
		var b strings.Builder
		if leading {
			b.WriteString(sep)
		}
		for _, segment := range segments {
			cur := b.String()
			if cur != "" && !strings.HasSuffix(cur, sep) {
				b.WriteString(sep)
			}
			b.WriteString(segment.text)
		}
		return b.String()
	}

	// Build the (left,right) split combinations, prioritizing those that keep the
	// desired suffix segments, then sorting by widest split.
	type combo struct{ left, right int }
	var combos []combo
	for left := 1; left <= segmentCount; left++ {
		minRight := 1
		if left == segmentCount {
			minRight = 0
		}
		for right := minRight; right <= segmentCount-left; right++ {
			combos = append(combos, combo{left, right})
		}
	}
	desiredSuffix := 0
	if segmentCount > 1 {
		desiredSuffix = min2(2, segmentCount-1)
	}
	var prioritized, fallback []combo
	for _, c := range combos {
		if c.right >= desiredSuffix {
			prioritized = append(prioritized, c)
		} else {
			fallback = append(fallback, c)
		}
	}
	sortCombos := func(items []combo) {
		sort.SliceStable(items, func(i, j int) bool {
			a, b := items[i], items[j]
			if a.left != b.left {
				return a.left > b.left
			}
			if a.right != b.right {
				return a.right > b.right
			}
			return (a.left + a.right) > (b.left + b.right)
		})
	}
	sortCombos(prioritized)
	sortCombos(fallback)

	for _, c := range append(prioritized, fallback...) {
		segments := make([]truncSegment, 0, c.left+c.right+1)
		for _, seg := range rawSegments[:c.left] {
			segments = append(segments, truncSegment{original: seg, text: seg, truncatable: true, isSuffix: false})
		}
		needEllipsis := c.left+c.right < segmentCount
		if needEllipsis {
			segments = append(segments, truncSegment{original: "…", text: "…", truncatable: false, isSuffix: false})
		}
		if c.right > 0 {
			for _, seg := range rawSegments[segmentCount-c.right:] {
				segments = append(segments, truncSegment{original: seg, text: seg, truncatable: true, isSuffix: true})
			}
		}
		allowFront := needEllipsis || segmentCount <= 2
		if candidate, ok := fitSegments(segments, allowFront, hasLeadingSep, segmentCount, maxWidth, assemble); ok {
			return candidate
		}
	}

	return frontTruncate(path, maxWidth)
}

// truncSegment is one path segment during fitting.
type truncSegment struct {
	original    string
	text        string
	truncatable bool
	isSuffix    bool
}

// fitSegments repeatedly assembles the candidate and front-truncates the
// widest-eligible segment until it fits or no progress is possible. It ports the
// inner `fit_segments` closure.
func fitSegments(
	segments []truncSegment,
	allowFrontTruncate bool,
	hasLeadingSep bool,
	segmentCount int,
	maxWidth int,
	assemble func(bool, []truncSegment) string,
) (string, bool) {
	for {
		candidate := assemble(hasLeadingSep, segments)
		width := uwidth(candidate)
		if width <= maxWidth {
			return candidate, true
		}
		if !allowFrontTruncate {
			return "", false
		}

		// Suffix segments first (reversed), then non-suffix (reversed).
		var indices []int
		for idx := len(segments) - 1; idx >= 0; idx-- {
			if segments[idx].truncatable && segments[idx].isSuffix {
				indices = append(indices, idx)
			}
		}
		for idx := len(segments) - 1; idx >= 0; idx-- {
			if segments[idx].truncatable && !segments[idx].isSuffix {
				indices = append(indices, idx)
			}
		}
		if len(indices) == 0 {
			return "", false
		}

		changed := false
		for _, idx := range indices {
			originalWidth := uwidth(segments[idx].original)
			if originalWidth <= maxWidth && segmentCount > 2 {
				continue
			}
			segWidth := uwidth(segments[idx].text)
			otherWidth := width - segWidth
			if otherWidth < 0 {
				otherWidth = 0
			}
			allowedWidth := maxWidth - otherWidth
			if allowedWidth < 1 {
				allowedWidth = 1
			}
			newText := frontTruncate(segments[idx].original, allowedWidth)
			if newText != segments[idx].text {
				segments[idx].text = newText
				changed = true
				break
			}
		}
		if !changed {
			return "", false
		}
	}
}

// frontTruncate keeps the trailing characters of original that fit in
// allowedWidth, prefixing a "…". It ports the inner `front_truncate` closure.
func frontTruncate(original string, allowedWidth int) string {
	if allowedWidth == 0 {
		return ""
	}
	if uwidth(original) <= allowedWidth {
		return original
	}
	if allowedWidth == 1 {
		return "…"
	}
	runes := []rune(original)
	used := 1 // reserve space for the leading ellipsis
	var kept []rune
	for i := len(runes) - 1; i >= 0; i-- {
		w := urunewidth(runes[i])
		if used+w > allowedWidth {
			break
		}
		used += w
		kept = append([]rune{runes[i]}, kept...)
	}
	return "…" + string(kept)
}

// min2 returns the smaller of a and b.
func min2(a, b int) int {
	if a < b {
		return a
	}
	return b
}
