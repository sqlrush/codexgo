package tui

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

// runeWidth returns the terminal cell width of a rune, honoring wide (CJK,
// emoji) characters. It is the per-rune analogue of [Capabilities.StringWidth]
// and is used by the wrapper so wide glyphs never overflow a column budget.
func runeWidth(r rune) int {
	if r == '\t' {
		// Tabs are expanded to a single cell for wrap accounting; the renderer
		// does not emit literal tabs in transcript lines.
		return 1
	}
	w := runewidth.RuneWidth(r)
	if w < 0 {
		return 0
	}
	return w
}

// spanWidth returns the display width of a span's text.
func spanWidth(s Span) int { return runewidth.StringWidth(s.Text) }

// lineWidth returns the display width of a styled line.
func lineWidth(l Line) int {
	w := 0
	for _, s := range l.Spans {
		w += spanWidth(s)
	}
	return w
}

// styledCell is a single rune carrying its span style, used by the wrapper to
// re-segment a line freely across wrap boundaries.
type styledCell struct {
	r     rune
	style Style
}

// WordWrapLine wraps a styled [Line] to the given width, preserving each span's
// style across wrap boundaries. It is the Go analogue of the word-wrapping used
// by the Rust transcript (wrapping.rs word_wrap_line), reduced to the behavior
// the chat surface needs: break on spaces when possible, hard-break overlong
// words, and keep span styles intact.
//
// A width <= 0 or a line that already fits returns the line unchanged. Trailing
// spaces on wrapped rows are dropped so the rendered transcript matches the Rust
// output's trimmed wrap rows.
func WordWrapLine(l Line, width int) []Line {
	if width <= 0 || lineWidth(l) <= width {
		return []Line{l}
	}

	var cells []styledCell
	for _, s := range l.Spans {
		for _, r := range s.Text {
			cells = append(cells, styledCell{r: r, style: s.Style})
		}
	}

	var out []Line
	var cur []styledCell
	curWidth := 0
	lastSpace := -1 // index in cur of the last breakable space

	flush := func(upTo int) {
		end := upTo
		for end > 0 && cur[end-1].r == ' ' {
			end--
		}
		out = append(out, cellsToLine(cur[:end]))
	}
	restAfter := func(from int) []styledCell {
		for from < len(cur) && cur[from].r == ' ' {
			from++
		}
		return append([]styledCell(nil), cur[from:]...)
	}

	for _, c := range cells {
		w := runeWidth(c.r)
		// A trailing space that overflows the width does not force a wrap: it is
		// dropped by flush's trailing-space trim and recorded as a break point so
		// the next non-space char wraps cleanly. This keeps "the quick" (exactly
		// width) on one row instead of breaking after "the".
		if curWidth+w > width && len(cur) > 0 && c.r != ' ' {
			if lastSpace >= 0 {
				flush(lastSpace)
				cur = restAfter(lastSpace)
			} else {
				flush(len(cur))
				cur = cur[:0]
			}
			curWidth = 0
			for _, cc := range cur {
				curWidth += runeWidth(cc.r)
			}
			lastSpace = -1
		}
		if c.r == ' ' {
			lastSpace = len(cur)
		}
		cur = append(cur, c)
		curWidth += w
	}
	if len(cur) > 0 {
		flush(len(cur))
	}
	if len(out) == 0 {
		out = append(out, Line{})
	}
	return out
}

// cellsToLine coalesces adjacent runes sharing a style into spans.
func cellsToLine(cells []styledCell) Line {
	if len(cells) == 0 {
		return Line{}
	}
	var spans []Span
	var b strings.Builder
	cur := cells[0].style
	for _, c := range cells {
		if c.style != cur {
			spans = append(spans, Span{Text: b.String(), Style: cur})
			b.Reset()
			cur = c.style
		}
		b.WriteRune(c.r)
	}
	if b.Len() > 0 {
		spans = append(spans, Span{Text: b.String(), Style: cur})
	}
	return Line{Spans: spans}
}

// WordWrapLines wraps each line in turn and concatenates the results.
func WordWrapLines(lines []Line, width int) []Line {
	var out []Line
	for _, l := range lines {
		out = append(out, WordWrapLine(l, width)...)
	}
	return out
}
