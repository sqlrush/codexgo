package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// This file assembles the `/status` card content (port of status/card.rs) into
// labeled field rows, and implements the running spinner / status-indicator row
// (port of status_indicator_widget.rs + motion.rs).
//
// Cosmetic deviation: card.rs renders a bordered, adaptively-wrapped history
// cell; here the card is a flat list of "label:   value" rows aligned to the
// widest label, which preserves the same fields and order.

// StatusTokenUsage is the display-ready token usage for the card (port of
// StatusTokenUsageData).
type StatusTokenUsage struct {
	Total  int64
	Input  int64
	Output int64
	// ContextWindow is the optional context-window breakdown.
	ContextWindow *StatusContextWindow
}

// StatusContextWindow holds the context-window breakdown for the card.
type StatusContextWindow struct {
	PercentRemaining int64
	TokensInContext  int64
	Window           int64
}

// StatusTokenUsageFromInfo derives display token usage from protocol token info,
// mirroring the data path in card.rs (total/input/output plus optional context
// window from the model context window).
func StatusTokenUsageFromInfo(info *protocol.TokenUsageInfo) StatusTokenUsage {
	if info == nil {
		return StatusTokenUsage{}
	}
	total := info.TotalTokenUsage
	out := StatusTokenUsage{
		Total:  total.TotalTokens,
		Input:  total.InputTokens,
		Output: total.OutputTokens,
	}
	if info.ModelContextWindow != nil && *info.ModelContextWindow > 0 {
		window := *info.ModelContextWindow
		inCtx := total.TotalTokens
		percentRemaining := int64(0)
		if window > 0 {
			used := float64(inCtx) / float64(window)
			if used < 0 {
				used = 0
			}
			if used > 1 {
				used = 1
			}
			percentRemaining = int64((1 - used) * 100)
		}
		out.ContextWindow = &StatusContextWindow{
			PercentRemaining: percentRemaining,
			TokensInContext:  inCtx,
			Window:           window,
		}
	}
	return out
}

// StatusCard is the display model for `/status`, an ordered set of fields plus
// the rate-limit section. It is rendered into aligned rows.
type StatusCard struct {
	ModelName     string
	ModelDetails  []string
	Directory     string
	Permissions   string
	AgentsSummary string
	Account       *StatusAccountDisplay
	ThreadName    string
	SessionID     string
	ForkedFrom    string
	TokenUsage    StatusTokenUsage
	RateLimits    RateLimitData
	CLIVersion    string
}

// statusField is one label/value pair in the card.
type statusField struct {
	label string
	value string
}

// fields returns the ordered card fields (port of the card.rs field ordering).
func (c StatusCard) fields() []statusField {
	var fields []statusField
	model := c.ModelName
	if len(c.ModelDetails) > 0 {
		model = fmt.Sprintf("%s (%s)", c.ModelName, strings.Join(c.ModelDetails, ", "))
	}
	fields = append(fields, statusField{"Model", model})
	if c.Directory != "" {
		fields = append(fields, statusField{"Directory", c.Directory})
	}
	if c.Permissions != "" {
		fields = append(fields, statusField{"Approval", c.Permissions})
	}
	if c.AgentsSummary != "" {
		fields = append(fields, statusField{"Agents", c.AgentsSummary})
	}
	if c.Account != nil {
		fields = append(fields, statusField{"Account", c.Account.Label()})
	}
	if c.ThreadName != "" {
		fields = append(fields, statusField{"Session", c.ThreadName})
	}
	if c.SessionID != "" {
		fields = append(fields, statusField{"Session ID", c.SessionID})
	}
	if c.ForkedFrom != "" {
		fields = append(fields, statusField{"Forked from", c.ForkedFrom})
	}
	tu := c.TokenUsage
	fields = append(fields, statusField{
		"Tokens",
		fmt.Sprintf("%s total (%s input, %s output)",
			FormatTokensCompact(tu.Total),
			FormatTokensCompact(tu.Input),
			FormatTokensCompact(tu.Output)),
	})
	if cw := tu.ContextWindow; cw != nil {
		fields = append(fields, statusField{
			"Context",
			fmt.Sprintf("%d%% left (%s of %s)",
				cw.PercentRemaining,
				FormatTokensCompact(cw.TokensInContext),
				FormatTokensCompact(cw.Window)),
		})
	}
	if c.CLIVersion != "" {
		fields = append(fields, statusField{"Version", c.CLIVersion})
	}
	return fields
}

// Render renders the status card to styled rows joined by newlines.
func (c StatusCard) Render(theme Theme) string {
	fields := c.fields()
	labelWidth := 0
	for _, f := range fields {
		if len([]rune(f.label)) > labelWidth {
			labelWidth = len([]rune(f.label))
		}
	}
	dim := theme.StatusLine
	var lines []string
	for _, f := range fields {
		pad := strings.Repeat(" ", labelWidth-len([]rune(f.label)))
		label := dim.Render(" " + f.label + ":" + pad + "   ")
		lines = append(lines, label+f.value)
	}
	lines = append(lines, renderRateLimitSection(c.RateLimits, labelWidth, theme)...)
	return strings.Join(lines, "\n")
}

// renderRateLimitSection renders the rate-limit rows below the card fields.
func renderRateLimitSection(data RateLimitData, labelWidth int, theme Theme) []string {
	dim := theme.StatusLine
	indent := strings.Repeat(" ", labelWidth+5)
	switch data.Availability {
	case RateLimitMissing:
		return nil
	case RateLimitUnavailable:
		return []string{"", dim.Render(" Usage limits: unavailable")}
	}
	var lines []string
	lines = append(lines, "")
	header := " Usage limits:"
	if data.Availability == RateLimitStale {
		header = " Usage limits (stale):"
	}
	lines = append(lines, dim.Render(header))
	for _, row := range data.Rows {
		if row.IsWindow {
			remaining := 100 - row.PercentUsed
			bar := RenderStatusLimitProgressBar(remaining)
			summary := FormatStatusLimitSummary(remaining)
			value := fmt.Sprintf("%s %s", bar, summary)
			if row.ResetsAt != "" {
				value += dim.Render(fmt.Sprintf(" (resets %s)", row.ResetsAt))
			}
			lines = append(lines, indent+dim.Render(row.Label+": ")+value)
		} else if row.Text != "" {
			lines = append(lines, indent+dim.Render(row.Label+": ")+row.Text)
		} else {
			lines = append(lines, indent+dim.Render(row.Label))
		}
	}
	return lines
}

// --- running spinner (port of status_indicator_widget.rs + motion.rs) --------

// FmtElapsedCompact formats an elapsed-seconds count compactly, mirroring
// status_indicator_widget.rs fmt_elapsed_compact.
func FmtElapsedCompact(elapsedSecs int64) string {
	if elapsedSecs < 0 {
		elapsedSecs = 0
	}
	if elapsedSecs < 60 {
		return fmt.Sprintf("%ds", elapsedSecs)
	}
	if elapsedSecs < 3600 {
		return fmt.Sprintf("%dm %02ds", elapsedSecs/60, elapsedSecs%60)
	}
	return fmt.Sprintf("%dh %02dm %02ds", elapsedSecs/3600, (elapsedSecs%3600)/60, elapsedSecs%60)
}

// SpinnerTickInterval is the spinner animation cadence (port of the 32ms
// schedule_frame_in in status_indicator_widget.rs).
const SpinnerTickInterval = 32 * time.Millisecond

// RunningSpinner models the streaming "Working (…)" status row shown while a
// turn is in flight (port of StatusIndicatorWidget). It is a value type; Tick
// and the With* helpers return new copies.
type RunningSpinner struct {
	// Header is the animated label left of the brackets (defaults to "Working").
	Header string
	// Elapsed is the running duration shown in the brackets.
	Elapsed time.Duration
	// ShowInterruptHint controls the "esc to interrupt" affordance.
	ShowInterruptHint bool
	// InterruptKey is the hint key label (defaults to "esc").
	InterruptKey string
	// AnimationsEnabled selects the blinking bullet vs. a hidden indicator.
	AnimationsEnabled bool
	// InlineMessage is optional trailing context after the brackets.
	InlineMessage string
	// frame advances the blink animation.
	frame int
}

// NewRunningSpinner returns a spinner with upstream defaults (port of
// StatusIndicatorWidget::new).
func NewRunningSpinner(animationsEnabled bool) RunningSpinner {
	return RunningSpinner{
		Header:            "Working",
		ShowInterruptHint: true,
		InterruptKey:      "esc",
		AnimationsEnabled: animationsEnabled,
	}
}

// Tick advances the spinner animation by one frame, returning a new spinner.
func (s RunningSpinner) Tick() RunningSpinner {
	s.frame++
	return s
}

// WithElapsed returns a copy with the elapsed duration set.
func (s RunningSpinner) WithElapsed(d time.Duration) RunningSpinner {
	s.Elapsed = d
	return s
}

// WithHeader returns a copy with the header label set, defaulting empty to
// "Working" (mirroring update_header keeping a non-empty label).
func (s RunningSpinner) WithHeader(header string) RunningSpinner {
	if header == "" {
		header = "Working"
	}
	s.Header = header
	return s
}

// indicator returns the animated bullet for the current frame (port of
// animated_activity_indicator's non-truecolor blink path). When animations are
// disabled the indicator is hidden (ReducedMotionIndicator::Hidden).
func (s RunningSpinner) indicator() (string, bool) {
	if !s.AnimationsEnabled {
		return "", false
	}
	// 600ms blink at a 32ms cadence ≈ 19 frames per half-cycle.
	if (s.frame/19)%2 == 0 {
		return "•", true
	}
	return "◦", true
}

// Line renders the spinner status row as a styled string (port of the render_ref
// span assembly in status_indicator_widget.rs).
func (s RunningSpinner) Line(theme Theme) string {
	dim := theme.StatusLine
	var b strings.Builder
	if ind, ok := s.indicator(); ok {
		b.WriteString(lipFg(theme.Primary).Render(ind))
		b.WriteString(" ")
	}
	b.WriteString(lipFg(theme.Primary).Render(s.Header))
	b.WriteString(" ")

	pretty := FmtElapsedCompact(int64(s.Elapsed.Seconds()))
	if s.ShowInterruptHint && s.InterruptKey != "" {
		b.WriteString(dim.Render(fmt.Sprintf("(%s • ", pretty)))
		b.WriteString(s.InterruptKey)
		b.WriteString(dim.Render(" to interrupt)"))
	} else {
		b.WriteString(dim.Render(fmt.Sprintf("(%s)", pretty)))
	}
	if s.InlineMessage != "" {
		b.WriteString(dim.Render(" · "))
		b.WriteString(dim.Render(s.InlineMessage))
	}
	return b.String()
}
