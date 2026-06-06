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

// frameSizes are the window sizes the harness exercises. 80x24 is the wave-1
// primary target (codex renders its first screen via a synchronized full repaint
// there); 120x40 is captured too but is documented as a later-wave target (its
// inline-scrollback insertion path needs a fuller emulator).
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
		"PATH":           os.Getenv("PATH"),
		"HOME":           os.Getenv("HOME"),
		"CODEX_HOME":     home,
		fakeEnvKey:       fakeAPIKey,
		"TERM":           "xterm-256color",
		"LANG":           "en_US.UTF-8",
		"COLUMNS":        strconv.Itoa(int(size.Cols)),
		"LINES":          strconv.Itoa(int(size.Rows)),
		"OPENAI_API_KEY": "",
		"CODEX_API_KEY":  "",
		"NO_COLOR":       "", // let both backends emit their normal SGR stream
		"CI":             "", // avoid CI-specific TUI degradation paths
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
// volatile (a per-run path, a version string, the random composer placeholder, a
// spinner glyph). Rows matching any mask are excluded from strict comparison and
// reported separately.
//
// These are the documented volatile cells for wave 1 (see DEVIATIONS.md TUI
// rows). Characters/timestamps that change every run cannot be byte-identical.
var frameMask = []string{
	"directory:",     // absolute per-run cwd path (center-truncated differently)
	"(v",             // CLI version string in the header title
	"/private/var",   // macOS temp-dir paths anywhere on a row
	"/var/folders",   // macOS temp-dir paths (canonical form)
	"/tmp",           // linux temp-dir paths
	"context left",   // composer footer context percentage
	"Send a message", // codexgo composer placeholder (volatile vs codex)
	"Ask Codex",      // codex composer placeholder (random per run)
	"Write tests",    // codex random placeholder
	"Explain this",   // codex random placeholder
	"Summarize",      // codex random placeholder
	"Implement",      // codex random placeholder
	"Find and fix",   // codex random placeholder
	"Improve doc",    // codex random placeholder
	"Run /review",    // codex random placeholder
	"Use /skills",    // codex random placeholder
	"Tip:",           // promotional startup tip (changes across releases)
	"Learn more",     // promotional tip continuation
	"GPT-5.5",        // promotional tip body
	"introducing",    // promotional tip URL
	// Promotional tip body continuation lines (wrapped, no "Tip:" prefix). The
	// whole startup tip is release-volatile and codexgo does not reproduce it
	// this wave; these phrases cover the 0.136.0 GPT-5.5 tip wrap at 80 and 120.
	"reason through",
	"keep going until",
	"check assumptions",
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
