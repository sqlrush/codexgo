package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"golang.org/x/term"

	"github.com/sqlrush/codexgo/internal/gitutils"
	"github.com/sqlrush/codexgo/internal/termdetect"
)

// proxyEnvVars are the proxy-related environment variables the network.env check
// reports, mirroring the Rust doctor's PROXY_ENV_VARS list.
var proxyEnvVars = []string{
	"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY",
	"http_proxy", "https_proxy", "all_proxy", "no_proxy",
}

// localeEnvVars are the locale-related environment variables the
// system.environment check surfaces, mirroring the Rust LOCALE_ENV_VARS list.
var localeEnvVars = []string{"LANG", "LC_ALL", "LC_CTYPE", "LANGUAGE"}

// colorEnvVars are the color-related environment variables the terminal.env check
// surfaces, mirroring the Rust COLOR_ENV_VARS list.
var colorEnvVars = []string{"COLORTERM", "NO_COLOR", "CLICOLOR", "CLICOLOR_FORCE", "FORCE_COLOR", "COLORFGBG"}

// terminalDimensionEnvVars are the dimension override env vars terminal.env
// surfaces, mirroring the Rust TERMINAL_DIMENSION_ENV_VARS list.
var terminalDimensionEnvVars = []string{"COLUMNS", "LINES"}

// defaultTerminalColumns and defaultTerminalRows are the fallback terminal
// dimensions used when no controlling terminal is attached, matching crossterm's
// (80, 24) fallback used by the Rust terminal check.
const (
	defaultTerminalColumns = 80
	defaultTerminalRows    = 24
)

// systemEnvironmentCheck reports OS/runtime metadata and the effective locale,
// mirroring system.environment in doctor.rs. It is always informational (ok).
func systemEnvironmentCheck() doctorCheck {
	b := newCheck("system.environment", "system")
	info := detectOSInfo()
	b.detail(fmt.Sprintf("os: %s", info.OS))
	b.detail(fmt.Sprintf("os type: %s", info.OSType))
	b.detail(fmt.Sprintf("os version: %s", info.OSVersion))

	language := osLanguage()
	if language != "" {
		b.detail(fmt.Sprintf("os language: %s", language))
	} else {
		b.detail("os language: unavailable")
	}
	for _, name := range localeEnvVars {
		if value, ok := os.LookupEnv(name); ok && value != "" {
			b.detail(fmt.Sprintf("%s: %s", name, value))
		}
	}

	if language != "" {
		b.ok(fmt.Sprintf("OS language %s", language))
	} else {
		b.ok("OS language unavailable")
	}
	return b.build()
}

// terminalEnvCheck reports detected terminal metadata, mirroring terminal.env in
// doctor.rs. A dumb terminal degrades to a warning because the interactive TUI
// cannot run there; all other cases are informational.
func terminalEnvCheck() doctorCheck {
	b := newCheck("terminal.env", "terminal")
	info := termdetect.Detect()
	stdinTTY := isTerminal(os.Stdin)
	stdoutTTY := isTerminal(os.Stdout)
	stderrTTY := isTerminal(os.Stderr)

	// Detail emission order mirrors terminal_check_from_inputs in doctor.rs.
	b.detail(fmt.Sprintf("terminal: %s", terminalNameDisplay(info)))
	if info.TermProgram != "" {
		b.detail(fmt.Sprintf("TERM_PROGRAM: %s", info.TermProgram))
	}
	if info.Version != "" {
		b.detail(fmt.Sprintf("terminal version: %s", info.Version))
	}
	if info.Term != "" {
		b.detail(fmt.Sprintf("TERM: %s", info.Term))
	}
	if info.Multiplexer.Present() {
		b.detail(fmt.Sprintf("multiplexer: %s", multiplexerNameDisplay(info.Multiplexer)))
	}
	b.detail(fmt.Sprintf("stdin is terminal: %t", stdinTTY))
	b.detail(fmt.Sprintf("stdout is terminal: %t", stdoutTTY))
	b.detail(fmt.Sprintf("stderr is terminal: %t", stderrTTY))

	columns, rows := detectTerminalSize()
	b.detail(fmt.Sprintf("terminal size: %dx%d", columns, rows))
	for _, name := range terminalDimensionEnvVars {
		if value, ok := os.LookupEnv(name); ok && value != "" {
			b.detail(fmt.Sprintf("%s: %s", name, value))
		}
	}

	b.detail(fmt.Sprintf("color output: %s", colorOutputSummary(stdoutTTY)))
	for _, name := range colorEnvVars {
		if value, ok := os.LookupEnv(name); ok && value != "" {
			b.detail(fmt.Sprintf("%s: %s", name, value))
		}
	}

	b.detail(fmt.Sprintf("effective locale: %s", orNone(effectiveLocale())))

	if info.Name == termdetect.TerminalDumb {
		b.warn("TERM is set to \"dumb\"; the interactive TUI may not work").
			remedy("Run in a supported terminal or unset TERM")
		return b.build()
	}
	b.ok("terminal metadata was detected")
	return b.build()
}

// terminalNameDisplay maps a detected terminal name to its display label,
// mirroring terminal_name in doctor.rs.
func terminalNameDisplay(info termdetect.TerminalInfo) string {
	switch info.Name {
	case termdetect.TerminalAppleTerminal:
		return "Apple Terminal"
	case termdetect.TerminalGhostty:
		return "Ghostty"
	case termdetect.TerminalIterm2:
		return "iTerm2"
	case termdetect.TerminalWarpTerminal:
		return "Warp"
	case termdetect.TerminalVsCode:
		return "VS Code"
	case termdetect.TerminalWezTerm:
		return "WezTerm"
	case termdetect.TerminalKitty:
		return "kitty"
	case termdetect.TerminalAlacritty:
		return "Alacritty"
	case termdetect.TerminalKonsole:
		return "Konsole"
	case termdetect.TerminalGnomeTerminal:
		return "GNOME Terminal"
	case termdetect.TerminalVte:
		return "VTE"
	case termdetect.TerminalWindowsTerminal:
		return "Windows Terminal"
	case termdetect.TerminalDumb:
		return "dumb"
	default:
		return "unknown"
	}
}

// multiplexerNameDisplay renders the detected multiplexer with its version,
// mirroring multiplexer_name in doctor.rs.
func multiplexerNameDisplay(mux termdetect.Multiplexer) string {
	name := "tmux"
	if mux.Kind == termdetect.MultiplexerZellij {
		name = "zellij"
	}
	if mux.Version != "" {
		return fmt.Sprintf("%s %s", name, mux.Version)
	}
	return name
}

// detectTerminalSize returns the controlling terminal's column/row count, falling
// back to 80x24 when no terminal is attached, matching crossterm::terminal::size.
func detectTerminalSize() (columns, rows int) {
	for _, f := range []*os.File{os.Stderr, os.Stdout, os.Stdin} {
		if cols, lines, err := term.GetSize(int(f.Fd())); err == nil && cols > 0 && lines > 0 {
			return cols, lines
		}
	}
	return defaultTerminalColumns, defaultTerminalRows
}

// colorOutputSummary reports whether colorized output is enabled and, when
// disabled, the dominant reason, mirroring color_output_summary in doctor.rs. The
// doctor JSON renderer always disables color (output is captured), so the common
// reason on a non-tty stdout is "stdout is not a terminal".
func colorOutputSummary(stdoutTTY bool) string {
	if envVarPresent("NO_COLOR") {
		return "disabled (NO_COLOR)"
	}
	if value, ok := os.LookupEnv("TERM"); ok && value == "dumb" {
		return "disabled (TERM=dumb)"
	}
	if !stdoutTTY {
		return "disabled (stdout is not a terminal)"
	}
	return "enabled"
}

// terminalTitleCheck reports the configured terminal-title behavior, mirroring
// terminal.title in doctor.rs. With no override it reports the default item set.
func terminalTitleCheck(dctx doctorContext) doctorCheck {
	b := newCheck("terminal.title", "title")
	source := "default"
	items := []string{"activity", "project-name"}
	if dctx.Loaded && dctx.Cfg.Tui != nil && dctx.Cfg.Tui.TerminalTitle != nil {
		configured := *dctx.Cfg.Tui.TerminalTitle
		if len(configured) == 0 {
			source = "disabled"
			items = nil
		} else {
			source = "configured"
			items = configured
		}
	}
	b.detail(fmt.Sprintf("terminal title source: %s", source))
	if len(items) == 0 {
		b.detail("terminal title items: none")
	} else {
		b.detail(fmt.Sprintf("terminal title items: %s", joinComma(items)))
	}
	b.detail(fmt.Sprintf("terminal title activity: %t", containsString(items, "activity")))

	// When the project-name item is selected, codex emits the project source and
	// value rows. The project root is the git repo root when present, otherwise
	// the cwd; the value is the basename, truncated to the TUI title width.
	if projectTitleSelected(items) {
		projectSource, projectValue := projectTitleCandidate(resolveCwd())
		b.detail(fmt.Sprintf("terminal title project source: %s", projectSource))
		if projectValue != "" {
			b.detail(fmt.Sprintf("terminal title project value: %s", projectValue))
		}
	}

	b.ok(fmt.Sprintf("terminal title %s", source))
	return b.build()
}

// networkEnvCheck reports the presence of proxy and custom-CA environment
// variables, mirroring network.env in doctor.rs. An unreadable custom CA file
// degrades to a warning; otherwise the check is informational.
func networkEnvCheck() doctorCheck {
	b := newCheck("network.env", "network")

	var present []string
	for _, name := range proxyEnvVars {
		if envVarPresent(name) {
			present = append(present, name)
		}
	}
	if len(present) == 0 {
		b.detail("proxy env vars: none")
	} else {
		sort.Strings(present)
		b.detail(fmt.Sprintf("proxy env vars present: %s", joinComma(present)))
	}

	status := statusOK
	summary := "network-related environment looks readable"
	for _, name := range []string{"CODEXGO_CA_CERTIFICATE", "SSL_CERT_FILE"} {
		raw, ok := os.LookupEnv(name)
		if !ok || raw == "" {
			continue
		}
		info, err := os.Stat(raw)
		switch {
		case err == nil && info.Mode().IsRegular():
			if file, openErr := os.Open(raw); openErr != nil {
				status = statusWarning
				summary = "custom CA env var points at an unreadable file"
				b.detail(fmt.Sprintf("%s: %s (%v)", name, raw, openErr))
			} else {
				_ = file.Close()
				b.detail(fmt.Sprintf("%s: readable file %s", name, raw))
			}
		case err == nil:
			status = statusWarning
			summary = "custom CA env var does not point at a file"
			b.detail(fmt.Sprintf("%s: not a file %s", name, raw))
		default:
			status = statusWarning
			summary = "custom CA env var points at an unreadable path"
			b.detail(fmt.Sprintf("%s: %s (%v)", name, raw, err))
		}
	}

	if status == statusWarning {
		b.warn(summary)
	} else {
		b.ok(summary)
	}
	return b.build()
}

// effectiveLocale resolves the effective locale from LC_ALL, then LANG, then
// LC_CTYPE, mirroring the precedence the Rust doctor uses.
func effectiveLocale() string {
	for _, name := range []string{"LC_ALL", "LANG", "LC_CTYPE"} {
		if value, ok := os.LookupEnv(name); ok && value != "" {
			return value
		}
	}
	return ""
}

// osLanguage derives a best-effort OS language tag from the effective locale, for
// example "en_US.UTF-8" -> "en-US".
func osLanguage() string {
	locale := effectiveLocale()
	if locale == "" || locale == "C" || locale == "POSIX" {
		return ""
	}
	if idx := indexByteSafe(locale, '.'); idx >= 0 {
		locale = locale[:idx]
	}
	return replaceRune(locale, '_', '-')
}

// projectTitleMaxChars bounds the rendered project-title value, mirroring the
// PROJECT_TITLE_MAX_CHARS constant in title.rs.
const projectTitleMaxChars = 24

// projectTitleSelected reports whether the configured title items include the
// project-name item (or its "project" alias), mirroring project_title_selected.
func projectTitleSelected(items []string) bool {
	for _, item := range items {
		if item == "project-name" || item == "project" {
			return true
		}
	}
	return false
}

// projectTitleCandidate resolves the project-title source label and value,
// mirroring terminal_title_project_root + project_title_candidate in title.rs:
// the git repo root ("git repo root") takes precedence over the cwd ("cwd"). The
// value is the directory basename truncated to projectTitleMaxChars graphemes.
func projectTitleCandidate(cwd string) (source, value string) {
	if root, ok := gitutils.GetGitRepoRoot(cwd); ok {
		return "git repo root", truncateTitlePart(filepath.Base(root))
	}
	return "cwd", truncateTitlePart(filepath.Base(cwd))
}

// truncateTitlePart truncates value to projectTitleMaxChars runes, appending an
// ellipsis when truncation occurred, mirroring truncate_title_part in title.rs.
func truncateTitlePart(value string) string {
	runes := []rune(value)
	if len(runes) <= projectTitleMaxChars || projectTitleMaxChars <= 3 {
		if len(runes) > projectTitleMaxChars {
			return string(runes[:projectTitleMaxChars])
		}
		return value
	}
	return string(runes[:projectTitleMaxChars-3]) + "..."
}

// containsString reports whether items contains value.
func containsString(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

// indexByteSafe returns the index of b in s, or -1 when absent.
func indexByteSafe(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// replaceRune returns s with every old byte replaced by new.
func replaceRune(s string, old, new byte) string {
	out := []byte(s)
	for i := range out {
		if out[i] == old {
			out[i] = new
		}
	}
	return string(out)
}
