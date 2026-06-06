package paritytest

// A small, dependency-free ANSI/VT100 terminal emulator used by the TUI
// frame-differential harness (tui_frame_test.go). It interprets just enough of
// the control sequences that codex's ratatui backend and codexgo's bubbletea
// backend emit to reconstruct the visible cell grid (a [Grid]) from a raw byte
// stream captured off a PTY.
//
// Each cell records BOTH its rune AND its final SGR attributes (foreground /
// background color — including 16-color, 256-indexed, and 24-bit RGB — plus
// bold / dim / italic / underline / reverse). Two layers of comparison are
// therefore possible against codex's output:
//
//   - characters only (the wave-1/2 bar), via [Grid.Row] / [Grid.String]; and
//   - per-cell attributes (wave 3), via [Grid.CellAt] / [diffGridAttrs].
//
// Because the comparison is done on the reconstructed grid, escape ORDERING is
// irrelevant: lipgloss (codexgo) and ratatui (codex) emit different SGR byte
// sequences, but as long as each cell ends up with the same final attributes the
// grids compare equal at the cell level.
//
// Scope of the control-sequence interpretation:
//
//   - CUP/HVP cursor positioning (CSI H / CSI f), CUU/CUD/CUF/CUB relative moves,
//     CR/LF/BS, RI (reverse index, ESC M), NEL (ESC E).
//   - ED (CSI J) and EL (CSI K) erase, in their 0/1/2 variants. Erased cells are
//     reset to a blank cell carrying the CURRENT background (matching how both
//     backends fill the gap when a styled background is active).
//   - DECSTBM scroll region (CSI r) plus scrolling on LF/RI within the region.
//   - Alternate screen enter/leave (CSI ? 1049 h/l) — switches to a cleared grid
//     so an alt-screen UI is read on its own surface.
//   - SGR (CSI m) is now FULLY parsed into the live pen and stamped onto every
//     written/erased cell; cursor show/hide, bracketed paste, synchronized-update
//     (?2026) and other private modes are consumed without visible effect.
//   - OSC sequences (ESC ] ... BEL | ST) and other ESC-prefixed shorts are
//     consumed so they do not corrupt the cell stream.
//
// Anything unrecognized is skipped defensively rather than rendered, so the
// emulator never emits stray control bytes into the grid. UTF-8 text is decoded
// to runes and placed using display width (so wide runes occupy two cells and a
// trailing filler cell is reserved), matching how both backends lay out text.

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

// vtWidthCond is a fixed display-width condition matching the Rust `unicode-width`
// crate codex's ratatui backend uses: ambiguous-width runes (the "…" ellipsis,
// box-drawing, etc.) are NARROW (width 1), independent of the process locale.
// runewidth's DefaultCondition auto-detects East Asian Width from the
// environment, which would mis-place such runes when reconstructing the grid.
var vtWidthCond = func() *runewidth.Condition {
	c := runewidth.NewCondition()
	c.EastAsianWidth = false
	return c
}()

// wideContinuation is the sentinel stored in the second cell of a wide (width-2)
// rune. It renders to nothing: Row reconstruction skips it so a width-2 rune
// contributes exactly its glyph (not glyph + filler space).
const wideContinuation = rune(0)

// ColorKind tags how a [Color] is encoded, mirroring ratatui's `Color` enum
// shape (default, a named 16-color, an indexed palette entry, or 24-bit RGB).
type ColorKind uint8

const (
	// ColorDefault is the terminal's default color (no SGR fg/bg override).
	ColorDefault ColorKind = iota
	// ColorIndexed is a 0..255 palette index. Values 0..15 carry the 16 named
	// ANSI colors (so e.g. ratatui's Color::Cyan and an SGR-36 both normalize to
	// index 6); 16..255 are the xterm fixed palette.
	ColorIndexed
	// ColorRGB is a 24-bit true-color value.
	ColorRGB
)

// Color is a backend-neutral cell color: a default, a palette index, or 24-bit
// RGB. Named ANSI colors are normalized to their 0..15 index so a comparison is
// agnostic to whether a backend emitted "SGR 36" (named cyan) or "SGR 38;5;6"
// (indexed cyan) — both map to ColorIndexed{6}.
type Color struct {
	Kind ColorKind
	// Idx is the palette index when Kind == ColorIndexed.
	Idx uint8
	// R, G, B are the channels when Kind == ColorRGB.
	R, G, B uint8
}

// defaultColor is the zero value: the terminal default.
var defaultColor = Color{Kind: ColorDefault}

// String renders a color compactly for diff output.
func (c Color) String() string {
	switch c.Kind {
	case ColorIndexed:
		return fmt.Sprintf("idx%d", c.Idx)
	case ColorRGB:
		return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
	default:
		return "default"
	}
}

// CellAttr is the final SGR state stamped on a cell.
type CellAttr struct {
	Fg, Bg                                Color
	Bold, Dim, Italic, Underline, Reverse bool
}

// defaultAttr is the reset pen: default colors, no modifiers.
var defaultAttr = CellAttr{Fg: defaultColor, Bg: defaultColor}

// String renders the attribute set compactly for diff output (only the
// non-default fields, so equal cells read as "·").
func (a CellAttr) String() string {
	var parts []string
	if a.Fg.Kind != ColorDefault {
		parts = append(parts, "fg="+a.Fg.String())
	}
	if a.Bg.Kind != ColorDefault {
		parts = append(parts, "bg="+a.Bg.String())
	}
	if a.Bold {
		parts = append(parts, "bold")
	}
	if a.Dim {
		parts = append(parts, "dim")
	}
	if a.Italic {
		parts = append(parts, "italic")
	}
	if a.Underline {
		parts = append(parts, "underline")
	}
	if a.Reverse {
		parts = append(parts, "reverse")
	}
	if len(parts) == 0 {
		return "·"
	}
	return strings.Join(parts, ",")
}

// Cell is one display column: a rune plus its final SGR attributes.
type Cell struct {
	R    rune
	Attr CellAttr
}

// Grid is a fixed-size character grid: rows of cells, each cell one display
// column. It is the cell-level rendering target both binaries are compared on.
type Grid struct {
	Rows  int
	Cols  int
	cells [][]Cell
}

// newGrid builds a blank rows×cols grid filled with default-attr spaces.
func newGrid(rows, cols int) *Grid {
	cells := make([][]Cell, rows)
	for r := range cells {
		row := make([]Cell, cols)
		for c := range row {
			row[c] = Cell{R: ' ', Attr: defaultAttr}
		}
		cells[r] = row
	}
	return &Grid{Rows: rows, Cols: cols, cells: cells}
}

// CellAt returns the cell at (r,c), or a blank default cell when out of range.
func (g *Grid) CellAt(r, c int) Cell {
	if r < 0 || r >= g.Rows || c < 0 || c >= g.Cols {
		return Cell{R: ' ', Attr: defaultAttr}
	}
	return g.cells[r][c]
}

// Row returns row r as a string with trailing spaces stripped (the per-row
// normalization both backends' renderers make irrelevant). Wide-rune
// continuation cells contribute nothing. Out-of-range rows return "".
func (g *Grid) Row(r int) string {
	if r < 0 || r >= g.Rows {
		return ""
	}
	var b strings.Builder
	for _, c := range g.cells[r] {
		if c.R == wideContinuation {
			continue
		}
		b.WriteRune(c.R)
	}
	return strings.TrimRight(b.String(), " ")
}

// AllRows returns every row as a trailing-space-stripped string.
func (g *Grid) AllRows() []string {
	out := make([]string, g.Rows)
	for r := 0; r < g.Rows; r++ {
		out[r] = g.Row(r)
	}
	return out
}

// String renders the grid as newline-joined, trailing-space-stripped rows.
func (g *Grid) String() string {
	return strings.Join(g.AllRows(), "\n")
}

// vtEmulator is the parser/state machine that turns a byte stream into a [Grid].
type vtEmulator struct {
	rows, cols int

	// primary is the normal screen; alt is the alternate screen. cur points at
	// the active one.
	primary *Grid
	alt     *Grid
	cur     *Grid

	// cursor position (0-based).
	row, col int

	// pen is the live SGR state stamped on every written/erased cell.
	pen CellAttr

	// scroll region (0-based, inclusive). Defaults to the full grid.
	top, bot int

	// saved cursor position (DECSC/DECRC, ESC 7 / ESC 8).
	savedRow, savedCol int
}

// newVTEmulator builds an emulator for a rows×cols terminal.
func newVTEmulator(rows, cols int) *vtEmulator {
	primary := newGrid(rows, cols)
	alt := newGrid(rows, cols)
	return &vtEmulator{
		rows:    rows,
		cols:    cols,
		primary: primary,
		alt:     alt,
		cur:     primary,
		pen:     defaultAttr,
		top:     0,
		bot:     rows - 1,
	}
}

// RenderGrid interprets raw (a captured PTY byte stream) on a fresh rows×cols
// emulator and returns the resulting visible [Grid].
func RenderGrid(raw []byte, rows, cols int) *Grid {
	e := newVTEmulator(rows, cols)
	e.feed(raw)
	return e.cur
}

// feed runs the whole byte stream through the state machine.
func (e *vtEmulator) feed(raw []byte) {
	i := 0
	for i < len(raw) {
		b := raw[i]
		switch {
		case b == 0x1b: // ESC
			i = e.handleEscape(raw, i)
		case b == '\r':
			e.col = 0
			i++
		case b == '\n':
			e.lineFeed()
			i++
		case b == '\b':
			if e.col > 0 {
				e.col--
			}
			i++
		case b == '\t':
			e.col = ((e.col / 8) + 1) * 8
			if e.col >= e.cols {
				e.col = e.cols - 1
			}
			i++
		case b == 0x07: // BEL
			i++
		case b < 0x20:
			// Other C0 controls: ignore.
			i++
		default:
			i = e.putText(raw, i)
		}
	}
}

// putText decodes one UTF-8 rune at raw[i], writes it at the cursor honoring
// display width, advances the cursor, and returns the new index.
func (e *vtEmulator) putText(raw []byte, i int) int {
	r, size := utf8.DecodeRune(raw[i:])
	if r == utf8.RuneError && size <= 1 {
		// Invalid byte: skip it so it never lands in the grid.
		return i + 1
	}
	w := vtWidthCond.RuneWidth(r)
	if w == 0 {
		// Combining / zero-width: drop (we compare base cells only).
		return i + size
	}
	e.writeRune(r, w)
	return i + size
}

// blankCell returns a space carrying the CURRENT pen background (with all
// foreground/modifier attributes reset). This matches how a styled fill paints
// erased / padding cells: the glyph is a space but the background color is the
// active one, so e.g. a reverse-video or colored-background run reads correctly.
func (e *vtEmulator) blankCell() Cell {
	return Cell{R: ' ', Attr: CellAttr{Fg: defaultColor, Bg: e.pen.Bg}}
}

// writeRune places r at the cursor (stamped with the live pen) and advances.
// Wide runes (w==2) occupy the current cell and reserve the next as a filler.
func (e *vtEmulator) writeRune(r rune, w int) {
	if e.col >= e.cols {
		// Stay clamped at the last column; backends generally avoid this by
		// wrapping explicitly, so we do not auto-wrap.
		e.col = e.cols - 1
	}
	if e.inBounds(e.row, e.col) {
		e.cur.cells[e.row][e.col] = Cell{R: r, Attr: e.pen}
	}
	if w == 2 && e.col+1 < e.cols && e.inBounds(e.row, e.col+1) {
		e.cur.cells[e.row][e.col+1] = Cell{R: wideContinuation, Attr: e.pen}
	}
	e.col += w
	if e.col > e.cols {
		e.col = e.cols
	}
}

// inBounds reports whether (r,c) is inside the active grid.
func (e *vtEmulator) inBounds(r, c int) bool {
	return r >= 0 && r < e.rows && c >= 0 && c < e.cols
}

// lineFeed moves down one line, scrolling the region when at the bottom margin.
func (e *vtEmulator) lineFeed() {
	if e.row == e.bot {
		e.scrollUp()
		return
	}
	if e.row < e.rows-1 {
		e.row++
	}
}

// reverseIndex (ESC M) moves up one line, scrolling down when at the top margin.
func (e *vtEmulator) reverseIndex() {
	if e.row == e.top {
		e.scrollDown()
		return
	}
	if e.row > 0 {
		e.row--
	}
}

// scrollUp shifts rows [top,bot] up by one, clearing the bottom row.
func (e *vtEmulator) scrollUp() {
	for r := e.top; r < e.bot; r++ {
		copy(e.cur.cells[r], e.cur.cells[r+1])
	}
	e.clearRow(e.bot)
}

// scrollDown shifts rows [top,bot] down by one, clearing the top row.
func (e *vtEmulator) scrollDown() {
	for r := e.bot; r > e.top; r-- {
		copy(e.cur.cells[r], e.cur.cells[r-1])
	}
	e.clearRow(e.top)
}

// clearRow fills row r with blank cells carrying the current pen background.
func (e *vtEmulator) clearRow(r int) {
	if r < 0 || r >= e.rows {
		return
	}
	blank := e.blankCell()
	for c := range e.cur.cells[r] {
		e.cur.cells[r][c] = blank
	}
}

// handleEscape dispatches an ESC-introduced sequence starting at raw[i] (raw[i]
// == 0x1b) and returns the index just past it.
func (e *vtEmulator) handleEscape(raw []byte, i int) int {
	if i+1 >= len(raw) {
		return i + 1
	}
	switch raw[i+1] {
	case '[':
		return e.handleCSI(raw, i+2)
	case ']':
		return e.handleOSC(raw, i+2)
	case 'M': // RI — reverse index
		e.reverseIndex()
		return i + 2
	case 'E': // NEL — next line
		e.col = 0
		e.lineFeed()
		return i + 2
	case 'D': // IND — index (line feed)
		e.lineFeed()
		return i + 2
	case '7': // DECSC — save cursor position.
		e.savedRow, e.savedCol = e.row, e.col
		return i + 2
	case '8': // DECRC — restore cursor position.
		e.row, e.col = e.savedRow, e.savedCol
		return i + 2
	case 'P', 'X', '^', '_': // DCS/SOS/PM/APC strings — consume to ST.
		return e.consumeToST(raw, i+2)
	case '(', ')', '*', '+': // charset designations — skip the next byte.
		return i + 3
	case '=', '>': // keypad mode — no visible effect.
		return i + 2
	case '\\': // lone ST.
		return i + 2
	default:
		return i + 2
	}
}

// handleCSI parses a CSI sequence whose parameter bytes start at raw[start]
// (after the "ESC[") and applies it. Returns the index just past the final byte.
func (e *vtEmulator) handleCSI(raw []byte, start int) int {
	j := start
	// Optional private-marker prefix (e.g. '?', '>', '=').
	private := byte(0)
	if j < len(raw) && (raw[j] == '?' || raw[j] == '>' || raw[j] == '=' || raw[j] == '<') {
		private = raw[j]
		j++
	}
	paramStart := j
	// Parameter and intermediate bytes: 0x30–0x3f (params) and 0x20–0x2f (interm).
	for j < len(raw) {
		c := raw[j]
		if (c >= 0x30 && c <= 0x3f) || (c >= 0x20 && c <= 0x2f) {
			j++
			continue
		}
		break
	}
	if j >= len(raw) {
		return j // truncated; nothing more to do.
	}
	final := raw[j]
	paramsRaw := string(raw[paramStart:j])
	// Strip any intermediate bytes for parameter parsing.
	paramsRaw = stripIntermediates(paramsRaw)
	params := parseParams(paramsRaw)
	e.applyCSI(private, params, final)
	return j + 1
}

// stripIntermediates removes 0x20–0x2f intermediate bytes from a CSI param run.
func stripIntermediates(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0x20 && r <= 0x2f {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// parseParams splits a ';'-separated CSI parameter string into integers, mapping
// empty fields to 0 (the VT default sentinel; callers substitute the real
// default). A ':' sub-parameter separator (used by some SGR color forms) is
// treated like ';' so e.g. "38:5:6" parses the same as "38;5;6".
func parseParams(s string) []int {
	if s == "" {
		return nil
	}
	parts := splitParams(s)
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n := 0
		for _, r := range p {
			if r < '0' || r > '9' {
				n = 0
				break
			}
			n = n*10 + int(r-'0')
		}
		out = append(out, n)
	}
	return out
}

// splitParams splits an SGR/CSI parameter string on ';' and ':' while PRESERVING
// empty fields (VT semantics: an empty field is the default/0). This keeps
// positional parameters (e.g. the channels of "38;2;r;g;b") aligned.
func splitParams(s string) []string {
	var out []string
	cur := strings.Builder{}
	for _, r := range s {
		if r == ';' || r == ':' {
			out = append(out, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteRune(r)
	}
	out = append(out, cur.String())
	return out
}

// param returns the idx-th parameter or def when absent or zero-as-empty.
func param(params []int, idx, def int) int {
	if idx >= len(params) || params[idx] == 0 {
		return def
	}
	return params[idx]
}

// applyCSI executes a parsed CSI command.
func (e *vtEmulator) applyCSI(private byte, params []int, final byte) {
	if private == '?' {
		e.applyPrivateMode(params, final)
		return
	}
	if private != 0 {
		// '>' / '=' / '<' introducers (keyboard/device queries): no visible effect.
		return
	}
	switch final {
	case 'H', 'f': // CUP / HVP — 1-based row;col.
		e.row = clamp(param(params, 0, 1)-1, 0, e.rows-1)
		e.col = clamp(param(params, 1, 1)-1, 0, e.cols-1)
	case 'A': // CUU
		e.row = clamp(e.row-param(params, 0, 1), 0, e.rows-1)
	case 'B': // CUD
		e.row = clamp(e.row+param(params, 0, 1), 0, e.rows-1)
	case 'C': // CUF
		e.col = clamp(e.col+param(params, 0, 1), 0, e.cols-1)
	case 'D': // CUB
		e.col = clamp(e.col-param(params, 0, 1), 0, e.cols-1)
	case 'd': // VPA — vertical position absolute.
		e.row = clamp(param(params, 0, 1)-1, 0, e.rows-1)
	case 'G': // CHA — column absolute.
		e.col = clamp(param(params, 0, 1)-1, 0, e.cols-1)
	case 'J': // ED — erase in display.
		e.eraseDisplay(param(params, 0, 0))
	case 'K': // EL — erase in line.
		e.eraseLine(param(params, 0, 0))
	case 'L': // IL — insert lines.
		e.insertLines(param(params, 0, 1))
	case 'M': // DL — delete lines.
		e.deleteLines(param(params, 0, 1))
	case 'P': // DCH — delete chars.
		e.deleteChars(param(params, 0, 1))
	case '@': // ICH — insert blanks.
		e.insertChars(param(params, 0, 1))
	case 'X': // ECH — erase chars.
		e.eraseChars(param(params, 0, 1))
	case 'r': // DECSTBM — set scroll region.
		e.setScrollRegion(params)
	case 'm': // SGR — set graphics rendition (now applied to the live pen).
		e.applySGR(params)
	case 's', 'u', 'h', 'l', 'n', 'c', 't', 'q', 'p':
		// save/restore cursor (s/u — approximated as no-op since codex restores to
		// known positions explicitly), mode set/reset, device-status/attribute
		// queries, window ops, cursor style: no visible cell effect.
	default:
		// Unknown CSI: ignore.
	}
}

// applySGR mutates the live pen from an SGR (CSI m) parameter list. An empty
// list means SGR 0 (reset). Recognized: reset, bold/dim/italic/underline/reverse
// and their resets, the 16 named fg/bg colors (30-37/90-97, 40-47/100-107),
// default fg/bg (39/49), and the extended 256-indexed (38;5;n) / 24-bit RGB
// (38;2;r;g;b) forms for both fg and bg.
func (e *vtEmulator) applySGR(params []int) {
	if len(params) == 0 {
		e.pen = defaultAttr
		return
	}
	for i := 0; i < len(params); i++ {
		p := params[i]
		switch {
		case p == 0:
			e.pen = defaultAttr
		case p == 1:
			e.pen.Bold = true
		case p == 2:
			e.pen.Dim = true
		case p == 3:
			e.pen.Italic = true
		case p == 4:
			e.pen.Underline = true
		case p == 7:
			e.pen.Reverse = true
		case p == 22:
			e.pen.Bold = false
			e.pen.Dim = false
		case p == 23:
			e.pen.Italic = false
		case p == 24:
			e.pen.Underline = false
		case p == 27:
			e.pen.Reverse = false
		case p >= 30 && p <= 37:
			e.pen.Fg = Color{Kind: ColorIndexed, Idx: uint8(p - 30)}
		case p == 39:
			e.pen.Fg = defaultColor
		case p >= 40 && p <= 47:
			e.pen.Bg = Color{Kind: ColorIndexed, Idx: uint8(p - 40)}
		case p == 49:
			e.pen.Bg = defaultColor
		case p >= 90 && p <= 97:
			e.pen.Fg = Color{Kind: ColorIndexed, Idx: uint8(p - 90 + 8)}
		case p >= 100 && p <= 107:
			e.pen.Bg = Color{Kind: ColorIndexed, Idx: uint8(p - 100 + 8)}
		case p == 38:
			col, adv := parseExtColor(params, i)
			e.pen.Fg = col
			i += adv
		case p == 48:
			col, adv := parseExtColor(params, i)
			e.pen.Bg = col
			i += adv
		}
		// Other SGR codes (e.g. 5 blink, 8 hidden, 9 strike, 21/25/28/29) are not
		// relevant to the surfaces being compared and are ignored.
	}
}

// parseExtColor parses an extended-color SGR run starting at params[i] (which is
// 38 or 48). It returns the resolved [Color] and the number of EXTRA params it
// consumed past index i (so the caller advances correctly). Supports the
// 5;<idx> (indexed) and 2;<r>;<g>;<b> (RGB) forms. Malformed runs fall back to
// the default color and consume nothing extra.
func parseExtColor(params []int, i int) (Color, int) {
	if i+1 >= len(params) {
		return defaultColor, 0
	}
	switch params[i+1] {
	case 5:
		if i+2 >= len(params) {
			return defaultColor, 1
		}
		return Color{Kind: ColorIndexed, Idx: uint8(params[i+2])}, 2
	case 2:
		if i+4 >= len(params) {
			return defaultColor, len(params) - 1 - i
		}
		return Color{
			Kind: ColorRGB,
			R:    uint8(params[i+2]),
			G:    uint8(params[i+3]),
			B:    uint8(params[i+4]),
		}, 4
	default:
		return defaultColor, 1
	}
}

// applyPrivateMode handles CSI ? Pm h/l (DEC private modes).
func (e *vtEmulator) applyPrivateMode(params []int, final byte) {
	set := final == 'h'
	for _, p := range params {
		switch p {
		case 1049, 47, 1047: // alternate screen buffer.
			if set {
				e.enterAlt()
			} else {
				e.leaveAlt()
			}
		}
		// All other private modes (cursor visibility 25, bracketed paste 2004,
		// synchronized update 2026, mouse 1000/1004, focus, etc.) have no
		// character-grid effect.
	}
}

// enterAlt switches to a freshly cleared alternate screen.
func (e *vtEmulator) enterAlt() {
	if e.cur == e.alt {
		return
	}
	e.alt = newGrid(e.rows, e.cols)
	e.cur = e.alt
	e.top, e.bot = 0, e.rows-1
	e.row, e.col = 0, 0
}

// leaveAlt switches back to the primary screen.
func (e *vtEmulator) leaveAlt() {
	if e.cur == e.primary {
		return
	}
	e.cur = e.primary
	e.top, e.bot = 0, e.rows-1
}

// eraseDisplay implements ED (mode 0: cursor→end, 1: start→cursor, 2/3: all).
func (e *vtEmulator) eraseDisplay(mode int) {
	switch mode {
	case 0:
		e.eraseLine(0)
		for r := e.row + 1; r < e.rows; r++ {
			e.clearRow(r)
		}
	case 1:
		e.eraseLine(1)
		for r := 0; r < e.row; r++ {
			e.clearRow(r)
		}
	case 2, 3:
		for r := 0; r < e.rows; r++ {
			e.clearRow(r)
		}
	}
}

// eraseLine implements EL (mode 0: cursor→eol, 1: bol→cursor, 2: whole line).
// Erased cells carry the current pen background.
func (e *vtEmulator) eraseLine(mode int) {
	if e.row < 0 || e.row >= e.rows {
		return
	}
	row := e.cur.cells[e.row]
	blank := e.blankCell()
	switch mode {
	case 0:
		for c := e.col; c < e.cols; c++ {
			row[c] = blank
		}
	case 1:
		for c := 0; c <= e.col && c < e.cols; c++ {
			row[c] = blank
		}
	case 2:
		for c := 0; c < e.cols; c++ {
			row[c] = blank
		}
	}
}

// insertLines implements IL within the scroll region at the cursor row.
func (e *vtEmulator) insertLines(n int) {
	if e.row < e.top || e.row > e.bot {
		return
	}
	for k := 0; k < n; k++ {
		for r := e.bot; r > e.row; r-- {
			copy(e.cur.cells[r], e.cur.cells[r-1])
		}
		e.clearRow(e.row)
	}
}

// deleteLines implements DL within the scroll region at the cursor row.
func (e *vtEmulator) deleteLines(n int) {
	if e.row < e.top || e.row > e.bot {
		return
	}
	for k := 0; k < n; k++ {
		for r := e.row; r < e.bot; r++ {
			copy(e.cur.cells[r], e.cur.cells[r+1])
		}
		e.clearRow(e.bot)
	}
}

// deleteChars implements DCH at the cursor.
func (e *vtEmulator) deleteChars(n int) {
	if e.row < 0 || e.row >= e.rows {
		return
	}
	row := e.cur.cells[e.row]
	blank := e.blankCell()
	for c := e.col; c < e.cols; c++ {
		if c+n < e.cols {
			row[c] = row[c+n]
		} else {
			row[c] = blank
		}
	}
}

// insertChars implements ICH at the cursor.
func (e *vtEmulator) insertChars(n int) {
	if e.row < 0 || e.row >= e.rows {
		return
	}
	row := e.cur.cells[e.row]
	blank := e.blankCell()
	for c := e.cols - 1; c >= e.col; c-- {
		if c-n >= e.col {
			row[c] = row[c-n]
		} else {
			row[c] = blank
		}
	}
}

// eraseChars implements ECH at the cursor.
func (e *vtEmulator) eraseChars(n int) {
	if e.row < 0 || e.row >= e.rows {
		return
	}
	row := e.cur.cells[e.row]
	blank := e.blankCell()
	for c := e.col; c < e.col+n && c < e.cols; c++ {
		row[c] = blank
	}
}

// setScrollRegion implements DECSTBM (CSI top;bot r). With no params it resets
// to the full screen.
func (e *vtEmulator) setScrollRegion(params []int) {
	top := param(params, 0, 1) - 1
	bot := param(params, 1, e.rows) - 1
	if top < 0 {
		top = 0
	}
	if bot >= e.rows {
		bot = e.rows - 1
	}
	if top >= bot {
		top, bot = 0, e.rows-1
	}
	e.top, e.bot = top, bot
	e.row, e.col = e.top, 0 // DECSTBM homes the cursor.
}

// handleOSC consumes an OSC sequence (terminated by BEL or ST) starting at
// raw[start] (after "ESC]") and returns the index just past the terminator.
func (e *vtEmulator) handleOSC(raw []byte, start int) int {
	return e.consumeToST(raw, start)
}

// consumeToST scans forward from i until a string terminator (BEL or ESC \) and
// returns the index just past it.
func (e *vtEmulator) consumeToST(raw []byte, i int) int {
	for i < len(raw) {
		if raw[i] == 0x07 { // BEL
			return i + 1
		}
		if raw[i] == 0x1b && i+1 < len(raw) && raw[i+1] == '\\' { // ST
			return i + 2
		}
		i++
	}
	return i
}

// clamp bounds v to [lo,hi].
func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
