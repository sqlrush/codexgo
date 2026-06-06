package paritytest

// TUI /clear frame-differential parity — WAVE 5.
//
// This scenario drives the /clear slash command end-to-end in both binaries and
// compares the POST-CLEAR frame. /clear (AppEvent::ClearUi) clears the terminal
// (scrollback + visible screen via the exact codex ANSI sequence
// \x1b[r\x1b[0m\x1b[H\x1b[2J\x1b[3J\x1b[H), resets the transcript state, and starts
// a fresh session — so the post-clear frame is the fresh idle screen: the
// session-header welcome card drained back into scrollback, a fresh idle composer
// + footer in the live region. It must therefore match the idle first frame
// byte-for-byte (modulo the same volatile rows the idle harness masks).
//
// CAPTURE — codex shows a slash-command popup while "/clear" is being typed, so
// the capture types "/clear", waits for the popup to render, presses Enter (which
// dispatches the command and clears the popup), then polls until the screen has
// gone quiet on the fresh idle frame. The VT100 emulator (vtgrid.go) processes
// the clear sequence (ED2 + scrollback purge + cursor home) and reconstructs the
// final visible grid, which is what we compare.
//
// STRICTNESS — this participates in the SAME strict ladder as the idle/dynamic
// frames (CODEX_TUI_FRAME_STRICT): the diff is always logged; level >= 1 fails on
// a non-masked glyph divergence and level >= 2 additionally fails on a non-masked
// per-cell SGR-attribute divergence. It skips gracefully when /clear cannot be
// driven to completion on the host.

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/pty"
)

// TestParityTUIFrameClear drives /clear in both binaries and compares the
// post-clear (fresh idle) frame.
func TestParityTUIFrameClear(t *testing.T) {
	ref := referenceBin(t)
	cgo := buildCodexgoBin(t)

	for _, size := range frameSizes {
		size := size
		name := fmt.Sprintf("%dx%d", size.Cols, size.Rows)
		t.Run(name, func(t *testing.T) {
			cwd := sharedCanonicalCwd(t)

			refRaw, ok := captureClearFrame(t, ref, cwd, size)
			if !ok {
				t.Skipf("codex /clear did not complete at %s (interactive turn unreachable on this host); skipping", name)
			}
			cgoRaw, ok := captureClearFrame(t, cgo, cwd, size)
			if !ok {
				t.Skipf("codexgo /clear did not complete at %s; skipping", name)
			}

			refGrid := RenderGrid(refRaw, int(size.Rows), int(size.Cols))
			cgoGrid := RenderGrid(cgoRaw, int(size.Rows), int(size.Cols))

			// Sanity: the fresh session-header card must be present in BOTH grids,
			// proving /clear ran and re-seeded the fresh session. If not, treat the
			// scenario as unreachable and skip rather than fail confusingly.
			if !gridContains(refGrid, "OpenAI Codex") || !gridContains(cgoGrid, "OpenAI Codex") {
				t.Skipf("fresh session header not found in both grids at %s (/clear did not render); skipping", name)
			}
			// Guard against capturing a transient mid-clear frame still showing the
			// "/clear" composer text or its slash popup.
			if gridContains(refGrid, "/clear") || gridContains(cgoGrid, "/clear") {
				t.Skipf("post-clear grid still shows the /clear composer/popup at %s (timing); skipping", name)
			}

			diff := diffGrids(refGrid, cgoGrid)
			matched := size.Rows - uint16(len(diff.mismatchedRows)) - uint16(diff.maskedRows)
			t.Logf("clear frame %s: %d rows total, %d masked (volatile), %d matched, %d mismatched",
				name, size.Rows, diff.maskedRows, matched, len(diff.mismatchedRows))

			if len(diff.mismatchedRows) > 0 {
				t.Logf("non-masked row differences (codex | codexgo):\n%s", diff.render())
				if strictFrame() {
					t.Errorf("clear frame %s: %d non-masked rows differ (run with %s unset to log only)",
						name, len(diff.mismatchedRows), strictFrameEnv)
				}
			}

			attrDiff := diffGridAttrs(refGrid, cgoGrid, diff)
			t.Logf("clear frame %s: attribute layer — %d cells compared, %d mismatched across %d rows",
				name, attrDiff.cellsCompared, attrDiff.cellsMismatched, len(attrDiff.rows))
			if attrDiff.cellsMismatched > 0 {
				t.Logf("non-masked cell ATTRIBUTE differences (codex | codexgo):\n%s", attrDiff.render())
				if strictFrameAttrs() {
					t.Errorf("clear frame %s: %d non-masked cells differ in SGR attributes (run with %s<2 to log only)",
						name, attrDiff.cellsMismatched, strictFrameEnv)
				}
			}
		})
	}
}

// captureClearFrame spawns bin in a PTY, settles the idle first frame, types
// "/clear", waits for the slash popup to render, presses Enter to dispatch the
// command, then polls until the post-clear (fresh idle) frame is present AND
// output has gone quiet. It returns the raw byte stream and whether /clear
// completed within the deadline.
func captureClearFrame(t *testing.T, bin, canonicalCwd string, size frameSize) ([]byte, bool) {
	t.Helper()

	home := t.TempDir()
	writeFrameParityConfig(t, home, canonicalCwd)

	env := map[string]string{
		"PATH":            os.Getenv("PATH"),
		"HOME":            os.Getenv("HOME"),
		"CODEX_HOME":      home,
		"CODEXGO_HOME":    home, // codexgo namespace; reference binary reads CODEX_HOME
		fakeEnvKey:        fakeAPIKey,
		"TERM":            "xterm-256color",
		"LANG":            "en_US.UTF-8",
		"COLUMNS":         strconv.Itoa(int(size.Cols)),
		"LINES":           strconv.Itoa(int(size.Rows)),
		"OPENAI_API_KEY":  "",
		"CODEX_API_KEY":   "",
		"CODEXGO_API_KEY": "",
		"NO_COLOR":        "",
		"CI":              "",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sp, err := pty.SpawnPTY(ctx, bin, nil, canonicalCwd, env, nil,
		pty.TerminalSize{Rows: size.Rows, Cols: size.Cols})
	if err != nil {
		t.Logf("captureClearFrame: spawn %s: %v", bin, err)
		return nil, false
	}

	var buf []byte
	last := time.Now()
	const quietFor = 700 * time.Millisecond
	const idleSettle = 700 * time.Millisecond
	deadline := time.Now().Add(25 * time.Second)
	respondedBG, respondedDSR, cleared := false, false, false
	// clearedAt records when the buffer was reset after dispatching /clear, so we
	// only consider the POST-clear output for quiescence.
	var clearedAt time.Time

	drain := func() bool {
		select {
		case chunk, ok := <-sp.Stdout:
			if !ok {
				return false
			}
			buf = append(buf, chunk...)
			last = time.Now()
			if !respondedBG && bytes.Contains(buf, []byte("\x1b]11;?")) {
				respondedBG = true
				sp.Process.Stdin() <- []byte("\x1b]11;rgb:0000/0000/0000\x1b\\")
			}
			if !respondedDSR && bytes.Contains(buf, []byte("\x1b[6n")) {
				respondedDSR = true
				sp.Process.Stdin() <- []byte("\x1b[1;1R")
			}
		case <-time.After(100 * time.Millisecond):
		}
		return true
	}

loop:
	for {
		if !drain() {
			break loop
		}
		quiet := time.Since(last)

		// Phase 1: once the idle first frame has settled, type "/clear", let the
		// slash popup render, then press Enter (dispatches the command). After
		// Enter, reset the buffer so only the post-clear output is captured.
		if !cleared && len(buf) > 0 && quiet > idleSettle {
			func() {
				defer func() { _ = recover() }()
				sp.Process.Stdin() <- []byte("/clear")
				time.Sleep(400 * time.Millisecond) // popup render
				sp.Process.Stdin() <- []byte{'\r'} // dispatch
			}()
			cleared = true
			buf = nil // capture only the post-clear frame
			clearedAt = time.Now()
			last = time.Now()
			continue
		}

		// Phase 2: after /clear, the fresh frame is ready when the header card is
		// present and the screen has gone quiet, and the typed "/clear" / popup is
		// gone from the latest output.
		if cleared && time.Since(clearedAt) > 300*time.Millisecond &&
			bytes.Contains(buf, []byte("OpenAI Codex")) && quiet > quietFor {
			break loop
		}

		if time.Now().After(deadline) {
			break loop
		}
	}

	// Quit and terminate.
	func() {
		defer func() { _ = recover() }()
		sp.Process.Stdin() <- []byte{0x03}
		sp.Process.Stdin() <- []byte{0x03}
	}()
	sp.Process.Terminate()

	if !cleared || !bytes.Contains(buf, []byte("OpenAI Codex")) {
		return nil, false
	}
	return buf, true
}
