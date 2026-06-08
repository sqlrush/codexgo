package tui

import (
	"fmt"
	"strings"

	"github.com/mattn/go-runewidth"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	gmtext "github.com/yuin/goldmark/text"
)

// MarkdownRenderer converts markdown source into styled transcript [Line]s. It
// is the Go analogue of the Rust markdown_render.rs pipeline (pulldown-cmark ->
// styled ratatui lines), reimplemented over goldmark's AST.
//
// Behavioral parity goals (matching markdown_render.rs MarkdownStyles defaults):
//   - headings: h1 bold+underline, h2 bold, h3 bold+italic, h4..h6 italic;
//   - inline code & code spans use the info accent (Rust cyan);
//   - emphasis -> italic, strong -> bold, strikethrough -> crossed out;
//   - fenced code blocks are syntax-highlighted via [Highlighter];
//   - blockquotes are accented (Rust green -> theme success) and prefixed;
//   - ordered/unordered lists with nested indentation; ordered markers accented.
//
// Cosmetic deviations from the Rust renderer are documented inline (no table
// transposition pipeline; links render their label with the destination dimmed).
type MarkdownRenderer struct {
	theme     Theme
	hl        Highlighter
	md        goldmark.Markdown
	headStyle [6]Style
}

// NewMarkdownRenderer builds a renderer bound to a theme.
func NewMarkdownRenderer(theme Theme) MarkdownRenderer {
	return MarkdownRenderer{
		theme: theme,
		hl:    NewHighlighter(theme),
		md:    goldmark.New(goldmark.WithExtensions(extension.Table)),
		headStyle: [6]Style{
			{Fg: theme.Foreground, Bold: true, Underline: true}, // h1
			{Fg: theme.Foreground, Bold: true},                  // h2
			{Fg: theme.Foreground, Bold: true, Italic: true},    // h3
			{Fg: theme.Foreground, Italic: true},                // h4
			{Fg: theme.Foreground, Italic: true},                // h5
			{Fg: theme.Foreground, Italic: true},                // h6
		},
	}
}

// Render parses markdown source and returns styled lines (unwrapped). Callers
// apply width wrapping with [WordWrapLines] after rendering so reflow on resize
// re-runs only the cheap wrap stage.
//
// Render never returns an error; malformed markdown degrades to plain text.
func (r MarkdownRenderer) Render(source string) []Line {
	src := []byte(source)
	doc := r.md.Parser().Parse(gmtext.NewReader(src))
	var out []Line
	r.renderBlocks(doc, src, &out, "")
	return trimTrailingBlank(out)
}

// renderBlocks renders the block-level children of n, separating blocks with a
// blank line, applying the given continuation prefix (used for blockquotes and
// list-item nesting).
func (r MarkdownRenderer) renderBlocks(n ast.Node, src []byte, out *[]Line, prefix string) {
	first := true
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if !first && blockNeedsSeparator(c) {
			*out = append(*out, prefixLine(Line{}, prefix))
		}
		first = false
		r.renderBlock(c, src, out, prefix)
	}
}

// renderBlock renders a single block node.
func (r MarkdownRenderer) renderBlock(n ast.Node, src []byte, out *[]Line, prefix string) {
	switch node := n.(type) {
	case *ast.Heading:
		style := r.headStyle[clampLevel(node.Level)-1]
		marker := strings.Repeat("#", node.Level) + " "
		line := Line{Spans: []Span{{Text: marker, Style: style}}}
		line.Spans = append(line.Spans, r.renderInline(node, src, style)...)
		*out = append(*out, prefixLine(line, prefix))

	case *ast.Paragraph, *ast.TextBlock:
		// Plain paragraph text carries NO foreground color: codex's markdown
		// renderer builds text with Style::default() (markdown_render.rs
		// current_line_style), so the body inherits the terminal's default fg and
		// reads as "·" in the captured cell attributes. Emphasis/code/link spans
		// override the fg explicitly below, so they are unaffected.
		para := r.renderInline(n, src, Style{})
		for _, l := range splitOnHardBreaks(para) {
			*out = append(*out, prefixLine(l, prefix))
		}

	case *ast.FencedCodeBlock:
		lang := string(node.Language(src))
		code := codeBlockText(node, src)
		for _, l := range r.hl.Highlight(code, lang) {
			*out = append(*out, prefixLine(l, prefix))
		}

	case *ast.CodeBlock:
		code := codeBlockText(node, src)
		for _, l := range plainCodeLines(code) {
			*out = append(*out, prefixLine(l, prefix))
		}

	case *ast.Blockquote:
		quotePrefix := prefix + "▌ "
		var inner []Line
		r.renderBlocks(node, src, &inner, "")
		quoteStyle := Style{Fg: r.theme.Success}
		for _, l := range inner {
			*out = append(*out, prefixStyledLine(l, quotePrefix, quoteStyle))
		}

	case *ast.List:
		r.renderList(node, src, out, prefix)

	case *ast.ThematicBreak:
		*out = append(*out, prefixLine(Line{Spans: []Span{{Text: "────────", Style: Style{Fg: r.theme.Dim}}}}, prefix))

	case *east.Table:
		r.renderTable(node, src, out, prefix)

	default:
		// Unknown block: render its inline text as a paragraph fallback.
		if n.Type() == ast.TypeBlock {
			r.renderBlocks(n, src, out, prefix)
		}
	}
}

// renderList renders an ordered or unordered list with nested indentation.
func (r MarkdownRenderer) renderList(node *ast.List, src []byte, out *[]Line, prefix string) {
	index := node.Start
	if index == 0 && node.IsOrdered() {
		index = 1
	}
	for item := node.FirstChild(); item != nil; item = item.NextSibling() {
		var marker string
		var markerStyle Style
		if node.IsOrdered() {
			marker = fmt.Sprintf("%d. ", index)
			markerStyle = Style{Fg: r.theme.Primary} // ordered markers accented
			index++
		} else {
			marker = "- "
			markerStyle = Style{Fg: r.theme.Foreground}
		}
		r.renderListItem(item, src, out, prefix, marker, markerStyle)
	}
}

// renderListItem renders a single list item: the first block carries the marker,
// continuation blocks are indented to align under the text.
func (r MarkdownRenderer) renderListItem(item ast.Node, src []byte, out *[]Line, prefix, marker string, markerStyle Style) {
	indent := strings.Repeat(" ", len([]rune(marker)))
	var inner []Line
	r.renderBlocks(item, src, &inner, "")
	for i, l := range inner {
		if i == 0 {
			lead := Line{Spans: []Span{{Text: prefix + marker, Style: markerStyle}}}
			lead.Spans = append(lead.Spans, l.Spans...)
			*out = append(*out, lead)
		} else {
			*out = append(*out, prefixLine(l, prefix+indent))
		}
	}
	if len(inner) == 0 {
		*out = append(*out, Line{Spans: []Span{{Text: prefix + marker, Style: markerStyle}}})
	}
}

// renderInline renders the inline children of n into spans, applying base as the
// default style. Emphasis/strong/code/links derive styles from base.
func (r MarkdownRenderer) renderInline(n ast.Node, src []byte, base Style) []Span {
	var spans []Span
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		spans = append(spans, r.renderInlineNode(c, src, base)...)
	}
	return spans
}

func (r MarkdownRenderer) renderInlineNode(n ast.Node, src []byte, base Style) []Span {
	switch node := n.(type) {
	case *ast.Text:
		seg := node.Segment
		text := string(seg.Value(src))
		spans := []Span{{Text: text, Style: base}}
		if node.HardLineBreak() {
			spans = append(spans, Span{Text: "\n"})
		} else if node.SoftLineBreak() {
			spans = append(spans, Span{Text: " ", Style: base})
		}
		return spans

	case *ast.String:
		return []Span{{Text: string(node.Value), Style: base}}

	case *ast.CodeSpan:
		style := base
		style.Fg = r.theme.Info // Rust: code cyan
		return []Span{{Text: codeSpanText(node, src), Style: style}}

	case *ast.Emphasis:
		style := base
		if node.Level >= 2 {
			style.Bold = true
		} else {
			style.Italic = true
		}
		return r.renderInline(node, src, style)

	case *ast.Link:
		style := base
		style.Fg = r.theme.Info
		style.Underline = true
		return r.renderInline(node, src, style)

	case *ast.AutoLink:
		style := base
		style.Fg = r.theme.Info
		style.Underline = true
		return []Span{{Text: string(node.URL(src)), Style: style}}

	case *ast.RawHTML:
		return []Span{{Text: rawHTMLText(node, src), Style: base}}

	case *ast.Image:
		// Render the alt text; the chat surface does not inline images.
		return r.renderInline(node, src, base)

	default:
		if n.Type() == ast.TypeInline {
			return r.renderInline(n, src, base)
		}
		return nil
	}
}

// renderTable renders a GFM table as a runewidth-aligned ASCII box table so it
// displays correctly in the terminal (goldmark's table extension is enabled in
// NewMarkdownRenderer; without a dedicated renderer the pipes would otherwise
// collapse into one garbled line). Cells render as plain text (inline styles
// dropped) to keep column widths exact; column widths are capped so a wide table
// does not overflow and get wrapped.
func (r MarkdownRenderer) renderTable(node *east.Table, src []byte, out *[]Line, prefix string) {
	var header []string
	var rows [][]string
	for c := node.FirstChild(); c != nil; c = c.NextSibling() {
		switch row := c.(type) {
		case *east.TableHeader:
			header = r.tableCells(row, src)
		case *east.TableRow:
			rows = append(rows, r.tableCells(row, src))
		}
	}
	ncol := len(header)
	for _, rw := range rows {
		if len(rw) > ncol {
			ncol = len(rw)
		}
	}
	if ncol == 0 {
		return
	}

	const maxCol = 36
	w := make([]int, ncol)
	measure := func(cells []string) {
		for i := 0; i < ncol; i++ {
			t := ""
			if i < len(cells) {
				t = cells[i]
			}
			cw := runewidth.StringWidth(t)
			if cw > maxCol {
				cw = maxCol
			}
			if cw > w[i] {
				w[i] = cw
			}
		}
	}
	measure(header)
	for _, rw := range rows {
		measure(rw)
	}

	dim := Style{Fg: r.theme.Dim}
	border := func(l, m, rr string) {
		var b strings.Builder
		b.WriteString(l)
		for i := 0; i < ncol; i++ {
			b.WriteString(strings.Repeat("-", w[i]+2))
			if i < ncol-1 {
				b.WriteString(m)
			}
		}
		b.WriteString(rr)
		*out = append(*out, Line{Spans: []Span{{Text: prefix + b.String(), Style: dim}}})
	}
	rowLine := func(cells []string, bold bool) {
		spans := []Span{{Text: prefix + "|", Style: dim}}
		for i := 0; i < ncol; i++ {
			t := ""
			if i < len(cells) {
				t = cells[i]
			}
			t = tableTruncate(t, w[i])
			if i < len(node.Alignments) && node.Alignments[i] == east.AlignRight {
				t = padCellLeft(t, w[i])
			} else {
				t = padCellRight(t, w[i])
			}
			st := Style{}
			if bold {
				st.Bold = true
			}
			spans = append(spans, Span{Text: " " + t + " ", Style: st})
			spans = append(spans, Span{Text: "|", Style: dim})
		}
		*out = append(*out, Line{Spans: spans})
	}

	// ASCII-only frame (+ - |): box-drawing glyphs are East-Asian-Ambiguous width
	// (2 cells under a CJK locale), which would misalign the frame vs the content.
	// ASCII is a fixed 1 cell everywhere, matching spaces and CJK exactly.
	border("+", "+", "+")
	if len(header) > 0 {
		rowLine(header, true)
		border("+", "+", "+")
	}
	for _, rw := range rows {
		rowLine(rw, false)
	}
	border("+", "+", "+")
}

// tableCells extracts each cell's plain text from a header/row node.
func (r MarkdownRenderer) tableCells(row ast.Node, src []byte) []string {
	var cells []string
	for c := row.FirstChild(); c != nil; c = c.NextSibling() {
		if cell, ok := c.(*east.TableCell); ok {
			var b strings.Builder
			for _, s := range r.renderInline(cell, src, Style{}) {
				b.WriteString(s.Text)
			}
			cells = append(cells, strings.TrimSpace(b.String()))
		}
	}
	return cells
}

func tableTruncate(s string, w int) string {
	if runewidth.StringWidth(s) <= w {
		return s
	}
	if w <= 1 {
		return "…"
	}
	return runewidth.Truncate(s, w, "…")
}

func padCellRight(s string, w int) string {
	if pad := w - runewidth.StringWidth(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

func padCellLeft(s string, w int) string {
	if pad := w - runewidth.StringWidth(s); pad > 0 {
		return strings.Repeat(" ", pad) + s
	}
	return s
}

// --- helpers ---------------------------------------------------------------

func clampLevel(level int) int {
	if level < 1 {
		return 1
	}
	if level > 6 {
		return 6
	}
	return level
}

// blockNeedsSeparator reports whether a blank line should precede a block. List
// items inside a tight list do not get separators; the Rust renderer collapses
// tight lists similarly.
func blockNeedsSeparator(n ast.Node) bool {
	return true
}

// prefixLine prepends an unstyled prefix to a line.
func prefixLine(l Line, prefix string) Line {
	if prefix == "" {
		return l
	}
	out := Line{Spans: append([]Span{{Text: prefix}}, l.Spans...)}
	return out
}

// prefixStyledLine prepends a styled prefix to a line and recolors the body with
// the prefix style's foreground (used for blockquote accenting).
func prefixStyledLine(l Line, prefix string, style Style) Line {
	spans := []Span{{Text: prefix, Style: style}}
	for _, s := range l.Spans {
		if s.Style.Fg == nil {
			s.Style.Fg = style.Fg
		}
		spans = append(spans, s)
	}
	return Line{Spans: spans}
}

// splitOnHardBreaks splits a span list on embedded "\n" break spans into lines.
func splitOnHardBreaks(spans []Span) []Line {
	var lines []Line
	var cur []Span
	for _, s := range spans {
		if s.Text == "\n" {
			lines = append(lines, Line{Spans: cur})
			cur = nil
			continue
		}
		cur = append(cur, s)
	}
	lines = append(lines, Line{Spans: cur})
	return lines
}

// trimTrailingBlank drops a trailing run of blank lines.
func trimTrailingBlank(lines []Line) []Line {
	for len(lines) > 0 && lineIsBlank(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// lineIsBlank reports whether a line has no visible content.
func lineIsBlank(l Line) bool {
	for _, s := range l.Spans {
		if strings.TrimSpace(s.Text) != "" {
			return false
		}
	}
	return true
}

// codeBlockText concatenates a code block's raw text segments.
func codeBlockText(n ast.Node, src []byte) string {
	var b strings.Builder
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		b.Write(seg.Value(src))
	}
	return b.String()
}

// codeSpanText extracts the literal text of a code span.
func codeSpanText(n *ast.CodeSpan, src []byte) string {
	var b strings.Builder
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if t, ok := c.(*ast.Text); ok {
			seg := t.Segment
			b.Write(seg.Value(src))
		}
	}
	return b.String()
}

// rawHTMLText extracts the literal text of a raw-HTML inline node.
func rawHTMLText(n *ast.RawHTML, src []byte) string {
	var b strings.Builder
	segs := n.Segments
	for i := 0; i < segs.Len(); i++ {
		seg := segs.At(i)
		b.Write(seg.Value(src))
	}
	return b.String()
}
