package tools

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

// ASCII rendering primitives for the deterministic reports.
//
// CRITICAL — "code matching" with codexgo's TUI: all width math uses
// mattn/go-runewidth at the SAME version codexgo's renderer uses, so a table the
// plugin aligns is rendered pixel-aligned by codexgo (CJK double-width included).
// Two invariants keep plugin and host from diverging:
//   - EastAsianWidth is pinned to false (codexgo's default), so ambiguous-width
//     glyphs measure identically on both sides.
//   - Frames use ASCII "+ - |" (a fixed 1 cell on every locale/terminal); the
//     prettier box-drawing glyphs are East-Asian-Ambiguous (2 cells under a CJK
//     locale) and would misalign frame vs content. Bars use block █░ (same width
//     per row, not mixed with frames). Emoji width varies by terminal and MUST
//     NOT appear inside an aligned cell — only in free-form heading lines.

func init() {
	// Match codexgo: treat ambiguous-width characters as narrow.
	runewidth.DefaultCondition.EastAsianWidth = false
}

// dispWidth is the terminal display width of s (CJK counts as 2).
func dispWidth(s string) int { return runewidth.StringWidth(s) }

// truncDisp truncates s to display width w, appending … when cut.
func truncDisp(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if dispWidth(s) <= w {
		return s
	}
	return runewidth.Truncate(s, w, "…")
}

// padRight left-aligns s within display width w (truncating if wider).
func padRight(s string, w int) string {
	s = truncDisp(s, w)
	if pad := w - dispWidth(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

// padLeft right-aligns s within display width w (for numeric columns).
func padLeft(s string, w int) string {
	s = truncDisp(s, w)
	if pad := w - dispWidth(s); pad > 0 {
		return strings.Repeat(" ", pad) + s
	}
	return s
}

// tableColumn describes one column of an asciiTable.
type tableColumn struct {
	Header string
	Right  bool // right-align cell content (numbers)
	Max    int  // cap display width (0 = fit content)
}

// asciiTable renders a box-drawing table aligned by DISPLAY width. Every emitted
// line has identical display width, so codexgo renders it perfectly aligned.
func asciiTable(cols []tableColumn, rows [][]string) string {
	n := len(cols)
	if n == 0 {
		return ""
	}
	w := make([]int, n)
	for i, c := range cols {
		w[i] = dispWidth(c.Header)
	}
	for _, row := range rows {
		for i := 0; i < n && i < len(row); i++ {
			cw := dispWidth(row[i])
			if cols[i].Max > 0 && cw > cols[i].Max {
				cw = cols[i].Max
			}
			if cw > w[i] {
				w[i] = cw
			}
		}
	}
	for i, c := range cols {
		if c.Max > 0 && w[i] > c.Max {
			w[i] = c.Max
		}
	}
	// Keep the whole table within a width budget so a wide table is not wrapped
	// by the terminal (which destroys alignment). Only columns that declared a
	// Max (cappable text columns) are shrunk; numeric/short columns are left
	// intact. The plugin can't know the real terminal width, so this targets the
	// common codexgo TUI width.
	shrinkToBudget(cols, w)

	var b strings.Builder
	border := func(l, m, r string) {
		b.WriteString(l)
		for i := range cols {
			b.WriteString(strings.Repeat("-", w[i]+2))
			if i < n-1 {
				b.WriteString(m)
			}
		}
		b.WriteString(r + "\n")
	}
	writeCell := func(s string, i int) {
		if cols[i].Right {
			s = padLeft(s, w[i])
		} else {
			s = padRight(s, w[i])
		}
		b.WriteString(" " + s + " |")
	}

	border("+", "+", "+")
	b.WriteString("|")
	for i, c := range cols {
		writeCell(c.Header, i)
	}
	b.WriteString("\n")
	border("+", "+", "+")
	for _, row := range rows {
		b.WriteString("|")
		for i := range cols {
			v := ""
			if i < len(row) {
				v = row[i]
			}
			writeCell(v, i)
		}
		b.WriteString("\n")
	}
	border("+", "+", "+")
	return b.String()
}

// maxTableWidth is the target maximum rendered table width (incl. borders).
// Picked to fit the common codexgo TUI without the terminal wrapping the row.
const maxTableWidth = 112

// minCappableCol is the floor a cappable (Max>0) column is shrunk to.
const minCappableCol = 12

// tableTotalWidth is the rendered width of a table with the given column widths:
// a leading "|" plus, per column, " <cell> |" = width+3.
func tableTotalWidth(w []int) int {
	t := 1
	for _, x := range w {
		t += x + 3
	}
	return t
}

// shrinkToBudget reduces the widest cappable (Max>0) column by one, repeatedly,
// until the table fits maxTableWidth or no cappable column exceeds the floor.
func shrinkToBudget(cols []tableColumn, w []int) {
	for tableTotalWidth(w) > maxTableWidth {
		wi, ww := -1, minCappableCol
		for i := range cols {
			if cols[i].Max > 0 && w[i] > ww {
				ww = w[i]
				wi = i
			}
		}
		if wi < 0 {
			return // nothing left to shrink
		}
		w[wi]--
	}
}

// barLine renders "label  value  ██████░░░░  suffix" — a proportional bar.
// frac is clamped to [0,1]; width = number of cells. label/value are padded to
// the given display widths so a column of barLines aligns.
func barLine(label, value string, frac float64, barWidth, labelW, valueW int, suffix string) string {
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	filled := int(frac*float64(barWidth) + 0.5)
	if filled > barWidth {
		filled = barWidth
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	out := padRight(label, labelW) + "  " + padRight(value, valueW) + "  " + bar
	if suffix != "" {
		out += "  " + suffix
	}
	return out
}
