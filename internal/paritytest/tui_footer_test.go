package paritytest

// TUI footer status-line parity — WAVE 5.
//
// The idle frame harness (tui_frame_test.go) masks the WHOLE footer status-line
// row because it carries the per-run cwd path. That mask hid the model-name span
// and its softened theme colors from comparison. This test tightens the mask to
// JUST the path span: it locates the footer row in both binaries' captured idle
// frames and compares the fixed PREFIX cells — the 2-col indent, the
// "<model> <reasoning>" span, and the " · " separator — at BOTH the glyph and the
// per-cell SGR-attribute layer, so the softened-RGB theme colors codex emits
// (model #f6e2b7, separator dim) are proven byte-identical. Only the trailing
// path cells (the genuinely per-run cwd) are excluded.
//
// codex renders the default status line with per-segment softened theme colors,
// captured byte-for-byte from codex 0.136.0 at TrueColor:
//
//	model span "<model> <reasoning>" : fg RGB(246,226,183) #f6e2b7, NOT dim
//	separator  " · "                 : dim, default fg
//	path span  "<current-dir>"       : fg RGB(171,223,167) #abdfa7, NOT dim
//
// This scenario participates in the same strict ladder as the other frames
// (CODEX_TUI_FRAME_STRICT): the diff is always logged; level >= 1 fails on a
// glyph divergence in the prefix and level >= 2 additionally fails on an
// attribute divergence. It skips gracefully when the footer row is not reachable.

import (
	"fmt"
	"strings"
	"testing"
)

// footerPrefix is the fixed leading content of the default status line for the
// hermetic frame config (model gpt-5.5, no reasoning override → "default"):
// the 2-col indent, the model-with-reasoning span, and the " · " separator. The
// per-run cwd path follows it and is excluded from comparison.
const footerPrefix = "  " + parityModelSlug + " default · "

// TestParityTUIFooterPrefix compares the footer status-line PREFIX (model span +
// separator, excluding the per-run path) glyph- and attribute-identically between
// both binaries.
func TestParityTUIFooterPrefix(t *testing.T) {
	ref := referenceBin(t)
	cgo := buildCodexgoBin(t)

	for _, size := range frameSizes {
		size := size
		name := fmt.Sprintf("%dx%d", size.Cols, size.Rows)
		t.Run(name, func(t *testing.T) {
			cwd := sharedCanonicalCwd(t)

			refRaw, ok := captureFrame(t, ref, cwd, size)
			if !ok {
				t.Skipf("codex produced no first-screen output at %s; skipping", name)
			}
			cgoRaw, ok := captureFrame(t, cgo, cwd, size)
			if !ok {
				t.Skipf("codexgo produced no first-screen output at %s; skipping", name)
			}

			refGrid := RenderGrid(refRaw, int(size.Rows), int(size.Cols))
			cgoGrid := RenderGrid(cgoRaw, int(size.Rows), int(size.Cols))

			refRow := findFooterRow(refGrid)
			cgoRow := findFooterRow(cgoGrid)
			if refRow < 0 || cgoRow < 0 {
				t.Skipf("footer prefix %q not found in both grids at %s (ref row %d, cgo row %d); skipping",
					footerPrefix, name, refRow, cgoRow)
			}

			// Compare the prefix cells: glyphs first, then per-cell SGR attributes.
			prefixLen := len([]rune(footerPrefix))
			var glyphMismatch, attrMismatch []string
			for c := 0; c < prefixLen; c++ {
				rc := refGrid.CellAt(refRow, c)
				cc := cgoGrid.CellAt(cgoRow, c)
				if rc.R != cc.R {
					glyphMismatch = append(glyphMismatch, fmt.Sprintf(
						"col %2d codex %q | codexgo %q", c, string(rc.R), string(cc.R)))
					continue
				}
				if rc.Attr != cc.Attr {
					attrMismatch = append(attrMismatch, fmt.Sprintf(
						"col %2d glyph %q codex[%s] | codexgo[%s]",
						c, string(rc.R), rc.Attr.String(), cc.Attr.String()))
				}
			}

			t.Logf("footer prefix %s: %d cells compared, %d glyph mismatches, %d attribute mismatches",
				name, prefixLen, len(glyphMismatch), len(attrMismatch))

			if len(glyphMismatch) > 0 {
				t.Logf("footer prefix glyph differences:\n%s", strings.Join(glyphMismatch, "\n"))
				if strictFrame() {
					t.Errorf("footer prefix %s: %d glyph cells differ (run with %s unset to log only)",
						name, len(glyphMismatch), strictFrameEnv)
				}
			}
			if len(attrMismatch) > 0 {
				t.Logf("footer prefix attribute differences:\n%s", strings.Join(attrMismatch, "\n"))
				if strictFrameAttrs() {
					t.Errorf("footer prefix %s: %d attribute cells differ (run with %s<2 to log only)",
						name, len(attrMismatch), strictFrameEnv)
				}
			}
		})
	}
}

// findFooterRow returns the index of the row whose content starts with the
// fixed footer prefix (after the leading 2-col indent), or -1 when not present.
// The search scans from the bottom up because the footer renders near the
// bottom of the live region.
func findFooterRow(g *Grid) int {
	for r := g.Rows - 1; r >= 0; r-- {
		if strings.HasPrefix(g.Row(r), footerPrefix) {
			return r
		}
	}
	return -1
}
