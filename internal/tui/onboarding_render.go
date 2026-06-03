package tui

import (
	"fmt"
	"strings"
)

// This file renders the onboarding steps to styled text using the foundation
// [Theme]. It is the Go analogue of the WidgetRef impls in welcome.rs,
// trust_directory.rs, and auth.rs render_pick_mode/render_api_key_entry.
//
// Cosmetic deviation: layout uses simple left-margin indentation and lipgloss
// span styling rather than ratatui's ColumnRenderable/Paragraph wrapping. Text
// content and prompt wording match the Rust source.

// welcomeBanner is the static welcome line (port of welcome.rs's final line; the
// ASCII animation frames are intentionally omitted).
const welcomeBanner = "  Welcome to Codex, OpenAI's command-line coding agent"

// Render returns the onboarding screen as styled rows joined by newlines.
func (s OnboardingScreen) Render(theme Theme) string {
	var lines []string
	if s.ShowWelcome {
		lines = append(lines, theme.UserMessage.Render(welcomeBanner), "")
	}
	if s.Auth != nil && (s.Auth.State() == StepInProgress || s.Trust == nil) {
		lines = append(lines, renderAuthStep(*s.Auth, theme)...)
		lines = append(lines, "")
	}
	if s.Trust != nil && s.Trust.State() == StepInProgress {
		lines = append(lines, renderTrustStep(*s.Trust, theme)...)
	}
	return strings.Join(lines, "\n")
}

// renderTrustStep renders the trust-directory prompt (port of
// TrustDirectoryWidget::render_ref).
func renderTrustStep(t TrustStep, theme Theme) []string {
	dim := theme.StatusLine
	bold := theme.UserMessage
	var lines []string

	lines = append(lines, "> "+bold.Render("You are in ")+t.Cwd, "")

	if t.Cwd != t.TrustTarget {
		warn := theme.Warning
		w := fmt.Sprintf(
			"  Note: You're in a subdirectory of a Git project. Trusting will apply to the repository root: %s",
			t.TrustTarget,
		)
		lines = append(lines, lipFg(warn).Render(w), "")
	}

	lines = append(lines,
		"  Do you trust the contents of this directory? Working with untrusted "+
			"contents comes with higher risk of prompt injection. Trusting the "+
			"directory allows project-local config, hooks, and exec policies to load.",
		"")

	lines = append(lines, trustOptionRow(0, "Yes, continue", t.Highlighted == TrustYes, theme))
	lines = append(lines, trustOptionRow(1, "No, quit", t.Highlighted == TrustQuit, theme))
	lines = append(lines, "")

	if t.Error != "" {
		lines = append(lines, lipFg(theme.Error).Render("  "+t.Error), "")
	}

	tail := " to continue"
	if t.ShowSandboxHint {
		tail = " to continue and create a sandbox..."
	}
	lines = append(lines, "  "+dim.Render("Press ")+"Enter"+dim.Render(tail))
	return lines
}

// trustOptionRow renders one trust option (port of selection_option_row usage).
func trustOptionRow(idx int, text string, selected bool, theme Theme) string {
	caret := "  "
	if selected {
		caret = "❯ "
	}
	label := fmt.Sprintf("%d. %s", idx+1, text)
	if selected {
		return caret + lipFg(theme.Primary).Bold(true).Render(label)
	}
	return caret + label
}

// renderAuthStep renders the auth login step in its current phase (port of the
// AuthModeWidget render paths).
func renderAuthStep(a AuthStep, theme Theme) []string {
	switch a.Phase {
	case PhaseAPIKeyEntry:
		return renderAPIKeyEntry(a, theme)
	case PhaseChatGPTContinueInBrowser:
		return []string{"  " + theme.SystemMessage.Render("Continue signing in via your browser...")}
	case PhaseChatGPTSuccessMessage:
		return []string{
			"  " + lipFg(theme.Success).Render("Signed in with ChatGPT."),
			"  " + theme.StatusLine.Render("Press Enter to continue"),
		}
	default:
		return renderPickMode(a, theme)
	}
}

// renderPickMode renders the login method menu (port of render_pick_mode).
func renderPickMode(a AuthStep, theme Theme) []string {
	lines := []string{
		"  Sign in with ChatGPT to use Codex as part of your paid plan",
		"  or connect an API key for usage-based billing",
		"",
	}
	for idx, opt := range a.displayedOptions() {
		selected := a.Highlighted == opt
		caret := " "
		if selected {
			caret = ">"
		}
		head := fmt.Sprintf("%s %d. ", caret, idx+1)
		label := signInOptionLabel(opt)
		if selected {
			lines = append(lines,
				lipFg(theme.Primary).Render(head)+lipFg(theme.Primary).Render(label),
				"     "+theme.StatusLine.Render(signInOptionDescription(opt)))
		} else {
			lines = append(lines, fmt.Sprintf("  %d. %s", idx+1, label))
		}
	}
	if a.Error != "" {
		lines = append(lines, "", lipFg(theme.Error).Render("  "+a.Error))
	}
	return lines
}

// renderAPIKeyEntry renders the API-key input field (port of
// render_api_key_entry).
func renderAPIKeyEntry(a AuthStep, theme Theme) []string {
	masked := strings.Repeat("•", len([]rune(a.APIKey)))
	hint := "Paste your API key and press Enter"
	if a.APIKeyFromEnv {
		hint = "Found an API key in your environment; press Enter to use it"
	}
	lines := []string{
		"  " + theme.UserMessage.Render("Connect an API key"),
		"",
		"  " + theme.StatusLine.Render(hint),
		"  " + lipFg(theme.Primary).Render("› ") + masked,
	}
	if a.Error != "" {
		lines = append(lines, "", lipFg(theme.Error).Render("  "+a.Error))
	}
	lines = append(lines, "", "  "+theme.StatusLine.Render("Press Esc to go back"))
	return lines
}
