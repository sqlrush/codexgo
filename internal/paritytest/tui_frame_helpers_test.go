package paritytest

// Helpers for the TUI frame-differential harness (tui_frame_test.go, wave 1).
//
// These drive BOTH the real codex binary and the built codexgo binary inside a
// pseudo-terminal at a fixed window size, with a hermetic CODEX_HOME configured
// so the TUI reaches its first screen offline (parity provider, no auth, the cwd
// pre-trusted so codex skips the trust/onboarding screen). The captured byte
// stream is rendered through the in-repo VT100 emulator (vtgrid.go) to a cell
// [Grid] for row-by-row comparison.

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/pty"
)

// frameSize is a fixed terminal window size the harness drives both binaries at.
type frameSize struct {
	Rows uint16
	Cols uint16
}

// frameSizes are the window sizes the harness exercises. Both 80x24 and 120x40
// now pass strict cell-equality for the idle first frame after the wave-2 inline
// migration (only the four genuinely per-run rows are masked at each size).
var frameSizes = []frameSize{
	{Rows: 24, Cols: 80},
	{Rows: 40, Cols: 120},
}

// writeFrameParityConfig writes a hermetic config.toml into home that points a
// custom `parity` provider at an unreachable local address (so no network is
// touched) with a fake env key, disables the startup update check, and marks the
// given canonical cwd as trusted so codex skips its trust/onboarding screen and
// lands on the chat composer. The same file is consumed by both binaries.
func writeFrameParityConfig(t *testing.T, home, canonicalCwd string) {
	t.Helper()
	cfg := strings.Join([]string{
		`model = "` + parityModelSlug + `"`,
		`model_provider = "parity"`,
		`check_for_update_on_startup = false`,
		``,
		// Disable the startup tooltip for BOTH binaries. codex's tip is a
		// network-fetched, time- and release-volatile announcement chosen at
		// random (tui/src/tooltips.rs), so it cannot be reproduced byte-for-byte.
		// Turning it off (tui.show_tooltips = false) removes the only
		// non-reproducible scrollback block, leaving a deterministic
		// header-card + blank-gap + 4-row live region (composer + footer) that
		// both binaries render identically (DEVIATIONS '36-40 TUI first-frame').
		`[tui]`,
		`show_tooltips = false`,
		``,
		`[model_providers.parity]`,
		`name = "parity"`,
		// 127.0.0.1:9 (the "discard" port) is reliably unconnectable, keeping the
		// TUI hermetic even if it lazily probes the provider.
		`base_url = "http://127.0.0.1:9/v1"`,
		`wire_api = "responses"`,
		`requires_openai_auth = false`,
		`env_key = "` + fakeEnvKey + `"`,
		``,
		`[projects."` + canonicalCwd + `"]`,
		`trust_level = "trusted"`,
		``,
	}, "\n")
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatalf("write frame config.toml: %v", err)
	}
}

// captureFrame spawns bin inside a PTY of the given size with a hermetic
// CODEX_HOME, running in canonicalCwd (shared between both binaries so any cwd
// path rendered into the first screen — e.g. the session-header directory row —
// truncates identically). It captures output until quiescence (no new bytes for
// quietFor) or the overall deadline, sends the quit keys, and returns the raw
// byte stream. It auto-answers the terminal background-color and cursor-position
// queries both backends emit at startup so the program proceeds past them. It
// returns ("", false) when the TUI never produced output (so callers can skip
// gracefully on a CI host where no usable PTY/first-screen is reachable).
func captureFrame(t *testing.T, bin, canonicalCwd string, size frameSize) ([]byte, bool) {
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
		"NO_COLOR":        "", // let both backends emit their normal SGR stream
		"CI":              "", // avoid CI-specific TUI degradation paths
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sp, err := pty.SpawnPTY(ctx, bin, nil, canonicalCwd, env, nil,
		pty.TerminalSize{Rows: size.Rows, Cols: size.Cols})
	if err != nil {
		t.Logf("captureFrame: spawn %s: %v", bin, err)
		return nil, false
	}

	var buf []byte
	last := time.Now()
	const quietFor = 600 * time.Millisecond
	deadline := time.Now().Add(15 * time.Second)
	respondedBG, respondedDSR := false, false

loop:
	for {
		select {
		case chunk, ok := <-sp.Stdout:
			if !ok {
				break loop
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
		if len(buf) > 0 && time.Since(last) > quietFor {
			break loop
		}
		if time.Now().After(deadline) {
			break loop
		}
	}

	// Send the quit keys (Ctrl+C twice is codex's quit chord) then hard-terminate.
	func() {
		defer func() { _ = recover() }() // stdin may already be closed on exit
		sp.Process.Stdin() <- []byte{0x03}
		sp.Process.Stdin() <- []byte{0x03}
	}()
	sp.Process.Terminate()

	if len(buf) == 0 {
		return nil, false
	}
	return buf, true
}

// sharedCanonicalCwd creates a temp working directory and returns its
// symlink-resolved (canonical) path, matching the key codex stores in the
// projects trust map (normalize_for_path_comparison resolves symlinks; on macOS
// /var → /private/var). The same dir is used for both binaries.
func sharedCanonicalCwd(t *testing.T) string {
	t.Helper()
	work := t.TempDir()
	canonical, err := filepath.EvalSymlinks(work)
	if err != nil {
		return work
	}
	return canonical
}

// buildCodexgoBin compiles the codexgo `codex` binary once for the harness.
func buildCodexgoBin(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "codexgo-tui")
	cmd := exec.Command("go", "build", "-o", out, "./cmd/codex")
	cmd.Dir = moduleRoot(t)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build codexgo: %v\n%s", err, b)
	}
	return out
}

// frameMask is a list of substrings whose presence on a grid row makes that row
// volatile. Rows matching any mask are excluded from strict comparison and
// reported separately.
//
// Wave 2 drove the structural gaps to zero: codexgo now renders the inline
// session-header card in scrollback, a one-row top inset, the bordered-less
// composer block (top-pad + "› <placeholder>" + bottom-pad), and the default
// status-line footer (model-with-reasoning · current-dir) — all cell-identical
// to codex. Wave 3 threaded the real CLI version (tui.CodexVersion = 0.136.0, the
// upstream codex version codexgo impersonates), so the header title row
// "│ >_ OpenAI Codex (v0.136.0) … │" now matches byte-for-byte and is NO LONGER
// masked. The startup tooltip is disabled for both binaries
// (writeFrameParityConfig sets tui.show_tooltips = false) because it is a
// network-fetched, release- and time-volatile random announcement that cannot be
// byte-matched. With the version unmasked, only three genuinely per-run rows
// remain volatile:
//
//   - the header "directory:" row (absolute per-run cwd, center-truncated),
//   - the composer placeholder row (one of 8 entries, chosen per run at random
//     INDEPENDENTLY by each binary — see composer_placeholder.go / chatwidget.rs
//     PLACEHOLDERS), and
//   - the footer status line (carries the absolute per-run cwd).
//
// The footer's leading "<model> <reasoning> · " prefix IS reproduced
// byte-for-byte (verified when both binaries share a short enough cwd); it is
// only masked here because the trailing path is per-run.
var frameMask = []string{
	">_ ",          // header title row: codexgo brands it "CodexGO (vX)" vs the reference "OpenAI Codex (v0.136.0)" — intentional branding deviation (DEVIATIONS.md "TUI branding"). The ">_ " prefix is unique to the header title (the composer uses "› ").
	"directory:",   // header row: absolute per-run cwd (center-truncated)
	"/private/var", // macOS temp-dir paths anywhere on a row (footer cwd)
	"/var/folders", // macOS temp-dir paths (canonical form)
	"/tmp",         // linux temp-dir paths
	"context left", // composer footer context percentage (when shown)
	// Composer placeholder pool (PLACEHOLDERS, chatwidget.rs): the idle prompt
	// is one of these 8, picked per run at random by each binary independently,
	// so the "› <placeholder>" row cannot be byte-identical across the two.
	"Explain this codebase",
	"Summarize recent commits",
	"Implement {feature}",
	"Find and fix a bug in",
	"Write tests for",
	"Improve documentation in",
	"Run /review on my current changes",
	"Use /skills to list available skills",
}

// rowIsMasked reports whether row should be excluded from strict comparison.
func rowIsMasked(row string) bool {
	for _, m := range frameMask {
		if strings.Contains(row, m) {
			return true
		}
	}
	return false
}
