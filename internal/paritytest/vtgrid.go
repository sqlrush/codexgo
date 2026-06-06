package paritytest

// A small, dependency-free ANSI/VT100 terminal emulator used by the TUI
// frame-differential harness (tui_frame_test.go). It interprets just enough of
// the control sequences that codex's ratatui backend and codexgo's bubbletea
// backend emit to reconstruct the visible character grid (a [Grid]) from a raw
// byte stream captured off a PTY.
//
// Scope (wave 1 — characters only):
//
//   - CUP/HVP cursor positioning (CSI H / CSI f), CUU/CUD/CUF/CUB relative moves,
//     CR/LF/BS, RI (reverse index, ESC M), NEL (ESC E).
//   - ED (CSI J) and EL (CSI K) erase, in their 0/1/2 variants.
//   - DECSTBM scroll region (CSI r) plus scrolling on LF/RI within the region.
//   - Alternate screen enter/leave (CSI ? 1049 h/l) — switches to a cleared grid
//     so an alt-screen UI is read on its own surface.
//   - SGR (CSI m) is parsed and dropped (colors are a later wave); cursor
//     show/hide, bracketed paste, synchronized-update (?2026) and other private
//     modes are consumed without visible effect.
//   - OSC sequences (ESC ] ... BEL | ST) and other ESC-prefixed shorts are
//     consumed so they do not corrupt the cell stream.
//
// Anything unrecognized is skipped defensively rather than rendered, so the
// emulator never emits stray control bytes into the grid. UTF-8 text is decoded
// to runes and placed using display width (so wide runes occupy two cells and a
// trailing filler cell is reserved), matching how both backends lay out text.

import (
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

// Grid is a fixed-size character grid: rows of cells, each cell one display
// column. It is the cell-level rendering target both binaries are compared on.
type Grid struct {
	Rows  int
	Cols  int
	cells [][]rune
}

// newGrid builds a blank rows×cols grid filled with spaces.
func newGrid(rows, cols int) *Grid {
	cells := make([][]rune, rows)
	for r := range cells {
		row := make([]rune, cols)
		for c := range row {
			row[c] = ' '
		}
		cells[r] = row
	}
	return &Grid{Rows: rows, Cols: cols, cells: cells}
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
		if c == wideContinuation {
			continue
		}
		b.WriteRune(c)
	}
	return strings.TrimRight(b.String(), " ")
}

// Rows returns every row as a trailing-space-stripped string.
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
		// Combining / zero-width: drop (wave 1 compares base cells only).
		return i + size
	}
	e.writeRune(r, w)
	return i + size
}

// writeRune places r at the cursor and advances. Wide runes (w==2) occupy the
// current cell and reserve the next as a filler space.
func (e *vtEmulator) writeRune(r rune, w int) {
	if e.col >= e.cols {
		// Stay clamped at the last column; backends generally avoid this by
		// wrapping explicitly, so we do not auto-wrap (wave-1 simplicity).
		e.col = e.cols - 1
	}
	if e.inBounds(e.row, e.col) {
		e.cur.cells[e.row][e.col] = r
	}
	if w == 2 && e.col+1 < e.cols && e.inBounds(e.row, e.col+1) {
		e.cur.cells[e.row][e.col+1] = wideContinuation
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

// clearRow fills row r with spaces.
func (e *vtEmulator) clearRow(r int) {
	if r < 0 || r >= e.rows {
		return
	}
	for c := range e.cur.cells[r] {
		e.cur.cells[r][c] = ' '
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
// default).
func parseParams(s string) []int {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ";")
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
	case 'm', 's', 'u', 'h', 'l', 'n', 'c', 't', 'q', 'p':
		// SGR (m), save/restore cursor (s/u — approximated as no-op here since
		// codex restores to known positions explicitly), mode set/reset,
		// device-status/attribute queries, window ops, cursor style: no visible
		// cell effect for wave-1 character comparison.
	default:
		// Unknown CSI: ignore.
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
func (e *vtEmulator) eraseLine(mode int) {
	if e.row < 0 || e.row >= e.rows {
		return
	}
	row := e.cur.cells[e.row]
	switch mode {
	case 0:
		for c := e.col; c < e.cols; c++ {
			row[c] = ' '
		}
	case 1:
		for c := 0; c <= e.col && c < e.cols; c++ {
			row[c] = ' '
		}
	case 2:
		for c := 0; c < e.cols; c++ {
			row[c] = ' '
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
	for c := e.col; c < e.cols; c++ {
		if c+n < e.cols {
			row[c] = row[c+n]
		} else {
			row[c] = ' '
		}
	}
}

// insertChars implements ICH at the cursor.
func (e *vtEmulator) insertChars(n int) {
	if e.row < 0 || e.row >= e.rows {
		return
	}
	row := e.cur.cells[e.row]
	for c := e.cols - 1; c >= e.col; c-- {
		if c-n >= e.col {
			row[c] = row[c-n]
		} else {
			row[c] = ' '
		}
	}
}

// eraseChars implements ECH at the cursor.
func (e *vtEmulator) eraseChars(n int) {
	if e.row < 0 || e.row >= e.rows {
		return
	}
	row := e.cur.cells[e.row]
	for c := e.col; c < e.col+n && c < e.cols; c++ {
		row[c] = ' '
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
