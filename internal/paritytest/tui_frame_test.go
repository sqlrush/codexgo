package paritytest

// TUI frame-differential parity harness — WAVE 1.
//
// This is the failing/target test for the TUI pixel-fidelity effort. codexgo's
// TUI is a behavioral bubbletea port that is not yet cell-grid identical to
// codex's ratatui TUI (DEVIATIONS.md rows 36-40). This harness drives BOTH the
// real codex 0.136.0 binary and the built codexgo binary inside a pseudo-terminal
// at fixed window sizes, captures each first screen offline (hermetic CODEX_HOME,
// parity provider, no auth, pre-trusted cwd), reconstructs the visible cell grid
// with the in-repo VT100 emulator (vtgrid.go), masks volatile cells (version,
// paths, the random composer placeholder, promotional tips), and compares the
// grids row-by-row.
//
// It is env-gated on CODEX_PARITY_BIN (reusing referenceBin) and skips when the
// binary is unset or no usable first-screen is reachable (CI safety). Run it:
//
//	CODEX_PARITY_BIN=/path/to/codex \
//	  go test ./internal/paritytest/ -run TestParityTUIFrame -v
//
// Strict cell equality is gated behind CODEX_TUI_FRAME_STRICT=1. By default the
// test captures, renders, and *logs* the side-by-side diff without failing, so
// the standard env-gated parity suite stays green while the cell-grid gap is
// being closed wave by wave. With CODEX_TUI_FRAME_STRICT=1 the unmasked rows must
// match or the test fails — that is the bar future waves drive to zero.
//
// Wave-2 status (see DEVIATIONS.md / docs/STATUS.md TUI rows):
//
//   - The session-header welcome card renders byte-identical (modulo the masked
//     version + path); TestSessionHeaderCardLayout80 (internal/tui) pins the bytes.
//   - codexgo now runs in the SAME inline-scrollback model as codex: finalized
//     history cells are printed into native terminal scrollback while the live
//     viewport renders only a one-row top inset + the composer block. The idle
//     composer (blank pad / "› <placeholder>" / blank pad) and the default
//     status-line footer ("<model> <reasoning> · <dir>") are cell-identical.
//   - The startup tooltip is disabled for BOTH binaries via the hermetic config
//     (tui.show_tooltips = false): codex's tip is a network-fetched, release- and
//     time-volatile random announcement that cannot be reproduced byte-for-byte.
//   - Only four genuinely per-run rows remain masked: the version, the header
//     directory row, the random composer placeholder, and the footer cwd.

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// strictFrameEnv gates how hard the frame comparison fails. It is a small ladder
// so each fidelity layer can be turned on independently as it lands:
//
//	unset / "0"     : log-only (the suite stays green during the multi-wave effort)
//	"1"/"true"/"yes": CHARACTER strict — non-masked rows' glyphs must match
//	"2"             : CHARACTER + ATTRIBUTE strict — non-masked rows' glyphs AND
//	                  per-cell SGR attributes (fg/bg incl. 256/24-bit, bold, dim,
//	                  italic, underline, reverse) must match
//
// Level 2 is a strict SUPERSET of level 1: it always runs the character check
// first (chars must match before attributes are meaningful), then layers the
// attribute assertion on top. The cell-grid comparison makes escape ORDERING
// irrelevant — lipgloss and ratatui emit different SGR byte streams, but only the
// final per-cell attributes are compared.
const strictFrameEnv = "CODEX_TUI_FRAME_STRICT"

// strictFrameLevel returns the configured strictness ladder value (0, 1, or 2).
func strictFrameLevel() int {
	switch os.Getenv(strictFrameEnv) {
	case "2":
		return 2
	case "1", "true", "yes":
		return 1
	default:
		return 0
	}
}

// strictFrame reports whether the character-level comparison must fail hard
// (level >= 1).
func strictFrame() bool {
	return strictFrameLevel() >= 1
}

// strictFrameAttrs reports whether the attribute-level comparison must fail hard
// (level >= 2).
func strictFrameAttrs() bool {
	return strictFrameLevel() >= 2
}

// TestParityTUIFrame is the wave-1 frame-differential harness. For each fixed
// window size it captures both binaries' first screens, renders cell grids, masks
// volatile rows, and compares the remainder. The diff is always logged; it only
// fails the test under CODEX_TUI_FRAME_STRICT.
func TestParityTUIFrame(t *testing.T) {
	ref := referenceBin(t)
	cgo := buildCodexgoBin(t)

	for _, size := range frameSizes {
		size := size
		name := fmt.Sprintf("%dx%d", size.Cols, size.Rows)
		t.Run(name, func(t *testing.T) {
			// Both binaries run in the SAME canonical cwd so any cwd path rendered
			// into the first screen truncates identically between them.
			cwd := sharedCanonicalCwd(t)

			refRaw, ok := captureFrame(t, ref, cwd, size)
			if !ok {
				t.Skipf("codex produced no first-screen output at %s (PTY/first-screen unreachable); skipping", name)
			}
			cgoRaw, ok := captureFrame(t, cgo, cwd, size)
			if !ok {
				t.Skipf("codexgo produced no first-screen output at %s; skipping", name)
			}

			refGrid := RenderGrid(refRaw, int(size.Rows), int(size.Cols))
			cgoGrid := RenderGrid(cgoRaw, int(size.Rows), int(size.Cols))

			diff := diffGrids(refGrid, cgoGrid)
			matched := size.Rows - uint16(len(diff.mismatchedRows)) - uint16(diff.maskedRows)

			t.Logf("frame %s: %d rows total, %d masked (volatile), %d matched, %d mismatched",
				name, size.Rows, diff.maskedRows, matched, len(diff.mismatchedRows))

			if len(diff.mismatchedRows) > 0 {
				t.Logf("non-masked row differences (codex | codexgo):\n%s", diff.render())
				if strictFrame() {
					t.Errorf("frame %s: %d non-masked rows differ (run with %s unset to log only)",
						name, len(diff.mismatchedRows), strictFrameEnv)
				}
			}

			// Attribute layer (CODEX_TUI_FRAME_STRICT=2): compare per-cell SGR
			// attributes on rows that matched at the character level and are not
			// masked. Rows whose glyphs already differ are skipped here — their
			// character mismatch is the actionable signal — so the attribute diff
			// reports only genuine styling gaps.
			attrDiff := diffGridAttrs(refGrid, cgoGrid, diff)
			t.Logf("frame %s: attribute layer — %d cells compared, %d mismatched across %d rows",
				name, attrDiff.cellsCompared, attrDiff.cellsMismatched, len(attrDiff.rows))
			if attrDiff.cellsMismatched > 0 {
				t.Logf("non-masked cell ATTRIBUTE differences (codex | codexgo):\n%s", attrDiff.render())
				if strictFrameAttrs() {
					t.Errorf("frame %s: %d non-masked cells differ in SGR attributes (run with %s<2 to log only)",
						name, attrDiff.cellsMismatched, strictFrameEnv)
				}
			}
		})
	}
}

// gridDiff holds the row-level comparison result for one frame size.
type gridDiff struct {
	rows           int
	maskedRows     int
	mismatchedRows []int
	masked         []bool // per-row: true when the row was excluded as volatile
	refRows        []string
	cgoRows        []string
}

// diffGrids compares two grids row by row, treating any row that matches the
// volatile mask (in either grid) as excluded. It records the indices of
// non-masked rows whose trailing-space-stripped content differs.
func diffGrids(ref, cgo *Grid) gridDiff {
	rows := ref.Rows
	if cgo.Rows < rows {
		rows = cgo.Rows
	}
	d := gridDiff{
		rows:    rows,
		masked:  make([]bool, rows),
		refRows: make([]string, rows),
		cgoRows: make([]string, rows),
	}
	for r := 0; r < rows; r++ {
		rr := ref.Row(r)
		cr := cgo.Row(r)
		d.refRows[r] = rr
		d.cgoRows[r] = cr
		if rowIsMasked(rr) || rowIsMasked(cr) {
			d.masked[r] = true
			d.maskedRows++
			continue
		}
		if rr != cr {
			d.mismatchedRows = append(d.mismatchedRows, r)
		}
	}
	return d
}

// attrEligible reports whether row r should participate in the attribute layer:
// it must be within range, not masked, and not already differing at the
// character level (a glyph mismatch is the actionable signal there).
func (d gridDiff) attrEligible(r int) bool {
	if r < 0 || r >= len(d.masked) || d.masked[r] {
		return false
	}
	for _, m := range d.mismatchedRows {
		if m == r {
			return false
		}
	}
	return d.refRows[r] == d.cgoRows[r]
}

// cellAttrDiff holds the attribute-layer comparison result for one frame size.
type cellAttrDiff struct {
	cellsCompared   int
	cellsMismatched int
	rows            []int // rows carrying at least one attribute mismatch
	// details lists one human-readable line per mismatched cell.
	details []string
}

// diffGridAttrs compares per-cell SGR attributes on every attribute-eligible row
// (see [gridDiff.attrEligible]). Wide-rune continuation cells are skipped (their
// attribute is a copy of the lead cell and carries no independent signal).
func diffGridAttrs(ref, cgo *Grid, rowDiff gridDiff) cellAttrDiff {
	rows := ref.Rows
	if cgo.Rows < rows {
		rows = cgo.Rows
	}
	cols := ref.Cols
	if cgo.Cols < cols {
		cols = cgo.Cols
	}
	var d cellAttrDiff
	for r := 0; r < rows; r++ {
		if !rowDiff.attrEligible(r) {
			continue
		}
		rowHasMismatch := false
		for c := 0; c < cols; c++ {
			rc := ref.CellAt(r, c)
			cc := cgo.CellAt(r, c)
			if rc.R == wideContinuation || cc.R == wideContinuation {
				continue
			}
			d.cellsCompared++
			if rc.Attr != cc.Attr {
				d.cellsMismatched++
				rowHasMismatch = true
				if len(d.details) < 40 { // cap the report so it stays readable
					d.details = append(d.details, fmt.Sprintf(
						"row %2d col %2d glyph %q  codex[%s] | codexgo[%s]",
						r, c, string(rc.R), rc.Attr.String(), cc.Attr.String()))
				}
			}
		}
		if rowHasMismatch {
			d.rows = append(d.rows, r)
		}
	}
	return d
}

// render produces a compact listing of the mismatched cells.
func (d cellAttrDiff) render() string {
	return strings.Join(d.details, "\n") + "\n"
}

// render produces a compact side-by-side listing of the mismatched rows.
func (d gridDiff) render() string {
	var b strings.Builder
	for _, r := range d.mismatchedRows {
		fmt.Fprintf(&b, "row %2d  codex   |%s|\n", r, d.refRows[r])
		fmt.Fprintf(&b, "        codexgo |%s|\n", d.cgoRows[r])
	}
	return b.String()
}
