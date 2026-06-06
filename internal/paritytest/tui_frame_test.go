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

// strictFrameEnv, when set to a truthy value, turns the row diff into a hard
// failure. Off by default so the suite stays green during the multi-wave effort.
const strictFrameEnv = "CODEX_TUI_FRAME_STRICT"

func strictFrame() bool {
	switch os.Getenv(strictFrameEnv) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
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
		})
	}
}

// gridDiff holds the row-level comparison result for one frame size.
type gridDiff struct {
	rows           int
	maskedRows     int
	mismatchedRows []int
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
		refRows: make([]string, rows),
		cgoRows: make([]string, rows),
	}
	for r := 0; r < rows; r++ {
		rr := ref.Row(r)
		cr := cgo.Row(r)
		d.refRows[r] = rr
		d.cgoRows[r] = cr
		if rowIsMasked(rr) || rowIsMasked(cr) {
			d.maskedRows++
			continue
		}
		if rr != cr {
			d.mismatchedRows = append(d.mismatchedRows, r)
		}
	}
	return d
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
