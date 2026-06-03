package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// This file ports codex-rs/tui/src/status/: the `/status` card display data and
// the running spinner. It turns protocol snapshots (token usage, rate limits,
// account) into stable display structures and renders them with the foundation
// [Theme].
//
// Ports:
//   - status/helpers.rs: format_tokens_compact, plan_type_display_name,
//     format_directory_display, format_reset_timestamp.
//   - status/rate_limits.rs: snapshot -> display rows, stale detection, progress
//     bar, credits row.
//   - status/account.rs: StatusAccountDisplay.
//   - status/card.rs: the assembled card (rendered as labeled field rows).
//   - frames.rs / ascii_animation.rs (running spinner): the streaming spinner.
//
// Cosmetic deviation: the Rust card draws a bordered history cell with adaptive
// wrapping; here rows are rendered as aligned "label: value" lines, which keeps
// the same content and ordering without ratatui's buffer machinery.

// lipFg returns a lipgloss style with the given foreground color. It is a small
// shared helper used across the onboarding/status renderers.
func lipFg(c lipgloss.TerminalColor) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(c)
}

// --- account display (port of status/account.rs StatusAccountDisplay) --------

// StatusAccountKind distinguishes the account display variants.
type StatusAccountKind int

const (
	// AccountChatGPT is a ChatGPT-authenticated account.
	AccountChatGPT StatusAccountKind = iota
	// AccountAPIKey is an API-key-authenticated account.
	AccountAPIKey
)

// StatusAccountDisplay describes the signed-in account for the status card.
type StatusAccountDisplay struct {
	Kind StatusAccountKind
	// Email is the ChatGPT account email, if known (ChatGPT only).
	Email string
	// Plan is the display plan name, if known (ChatGPT only).
	Plan string
}

// Label returns the account value text for the status card.
func (a StatusAccountDisplay) Label() string {
	if a.Kind == AccountAPIKey {
		return "API key"
	}
	parts := []string{"ChatGPT"}
	if a.Plan != "" {
		parts = append(parts, fmt.Sprintf("(%s)", a.Plan))
	}
	if a.Email != "" {
		parts = append(parts, a.Email)
	}
	return strings.Join(parts, " ")
}

// --- helpers (port of status/helpers.rs) -------------------------------------

// FormatTokensCompact formats a token count compactly (e.g. 1.5K, 2.3M),
// mirroring helpers.rs format_tokens_compact exactly.
func FormatTokensCompact(value int64) string {
	if value < 0 {
		value = 0
	}
	if value == 0 {
		return "0"
	}
	if value < 1000 {
		return fmt.Sprintf("%d", value)
	}
	v := float64(value)
	var scaled float64
	var suffix string
	switch {
	case value >= 1_000_000_000_000:
		scaled, suffix = v/1_000_000_000_000, "T"
	case value >= 1_000_000_000:
		scaled, suffix = v/1_000_000_000, "B"
	case value >= 1_000_000:
		scaled, suffix = v/1_000_000, "M"
	default:
		scaled, suffix = v/1_000, "K"
	}
	var decimals int
	switch {
	case scaled < 10:
		decimals = 2
	case scaled < 100:
		decimals = 1
	default:
		decimals = 0
	}
	formatted := fmt.Sprintf("%.*f", decimals, scaled)
	if strings.Contains(formatted, ".") {
		formatted = strings.TrimRight(formatted, "0")
		formatted = strings.TrimRight(formatted, ".")
	}
	return formatted + suffix
}

// PlanTypeDisplayName maps a plan type to its user-facing label, mirroring
// helpers.rs plan_type_display_name.
func PlanTypeDisplayName(plan protocol.PlanType) string {
	switch {
	case plan.IsTeamLike():
		return "Business"
	case plan.IsBusinessLike():
		return "Enterprise"
	case plan == protocol.PlanTypeProLite:
		return "Pro Lite"
	default:
		return titleCase(planRawName(plan))
	}
}

// planRawName returns the Rust Debug-style base name used before title-casing.
func planRawName(plan protocol.PlanType) string {
	switch plan {
	case protocol.PlanTypeFree:
		return "free"
	case protocol.PlanTypeGo:
		return "go"
	case protocol.PlanTypePlus:
		return "plus"
	case protocol.PlanTypePro:
		return "pro"
	case protocol.PlanTypeEdu:
		return "edu"
	case protocol.PlanTypeUnknown:
		return "unknown"
	default:
		return string(plan)
	}
}

// titleCase upper-cases the first rune and lower-cases the rest (port of
// helpers.rs title_case).
func titleCase(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	return strings.ToUpper(string(r[0])) + strings.ToLower(string(r[1:]))
}

// FormatResetTimestamp formats a reset time relative to a capture time,
// mirroring helpers.rs format_reset_timestamp.
func FormatResetTimestamp(reset, capturedAt time.Time) string {
	t := reset.Format("15:04")
	if sameDay(reset, capturedAt) {
		return t
	}
	return fmt.Sprintf("%s on %s", t, formatDayMonth(reset))
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// formatDayMonth renders "%-d %b" (e.g. "3 Jun").
func formatDayMonth(t time.Time) string {
	return fmt.Sprintf("%d %s", t.Day(), t.Format("Jan"))
}

// --- rate limits (port of status/rate_limits.rs) -----------------------------

const (
	statusLimitBarSegments = 20
	statusLimitBarFilled   = "█"
	statusLimitBarEmpty    = "░"
	// RateLimitStaleThresholdMinutes is the age after which a snapshot is stale.
	RateLimitStaleThresholdMinutes = 15
)

// RateLimitWindowDisplay is a display-ready usage window.
type RateLimitWindowDisplay struct {
	UsedPercent float64
	ResetsAt    string
	WindowMins  *int64
}

// RateLimitSnapshotDisplay is a display-ready rate-limit snapshot.
type RateLimitSnapshotDisplay struct {
	LimitName  string
	CapturedAt time.Time
	Primary    *RateLimitWindowDisplay
	Secondary  *RateLimitWindowDisplay
	Credits    *CreditsSnapshotDisplay
}

// CreditsSnapshotDisplay is a display-ready credits state.
type CreditsSnapshotDisplay struct {
	HasCredits bool
	Unlimited  bool
	Balance    string
}

// RateLimitWindowDisplayFrom maps a protocol window to its display form,
// mirroring RateLimitWindowDisplay::from_window.
func RateLimitWindowDisplayFrom(w protocol.RateLimitWindow, capturedAt time.Time) RateLimitWindowDisplay {
	resets := ""
	if w.ResetsAt != nil {
		resets = FormatResetTimestamp(time.Unix(*w.ResetsAt, 0).Local(), capturedAt)
	}
	return RateLimitWindowDisplay{
		UsedPercent: w.UsedPercent,
		ResetsAt:    resets,
		WindowMins:  w.WindowMinutes,
	}
}

// RateLimitSnapshotDisplayFor maps a protocol snapshot to its display form,
// mirroring rate_limit_snapshot_display_for_limit.
func RateLimitSnapshotDisplayFor(s protocol.RateLimitSnapshot, limitName string, capturedAt time.Time) RateLimitSnapshotDisplay {
	out := RateLimitSnapshotDisplay{LimitName: limitName, CapturedAt: capturedAt}
	if s.Primary != nil {
		w := RateLimitWindowDisplayFrom(*s.Primary, capturedAt)
		out.Primary = &w
	}
	if s.Secondary != nil {
		w := RateLimitWindowDisplayFrom(*s.Secondary, capturedAt)
		out.Secondary = &w
	}
	if s.Credits != nil {
		out.Credits = &CreditsSnapshotDisplay{
			HasCredits: s.Credits.HasCredits,
			Unlimited:  s.Credits.Unlimited,
			Balance:    deref(s.Credits.Balance),
		}
	}
	return out
}

// StatusRateLimitRow is one display row in the rate-limits section.
type StatusRateLimitRow struct {
	Label string
	// PercentUsed is set for window rows.
	PercentUsed float64
	// ResetsAt is the localized reset string for window rows.
	ResetsAt string
	// Text is the value for non-window rows (e.g. credits).
	Text string
	// IsWindow distinguishes window rows from text rows.
	IsWindow bool
}

// RateLimitAvailability classifies the freshness of rate-limit data.
type RateLimitAvailability int

const (
	// RateLimitAvailable means data is fresh.
	RateLimitAvailable RateLimitAvailability = iota
	// RateLimitStale means data exists but is past the staleness threshold.
	RateLimitStale
	// RateLimitUnavailable means the refresh returned no displayable rows.
	RateLimitUnavailable
	// RateLimitMissing means no snapshot is available.
	RateLimitMissing
)

// RateLimitData is the composed rate-limit display state.
type RateLimitData struct {
	Availability RateLimitAvailability
	Rows         []StatusRateLimitRow
}

// ComposeRateLimitData builds display rows from snapshots and marks stale data,
// mirroring compose_rate_limit_data_many. Non-codex limits get a group/prefix
// row, matching the Rust labeling rules.
func ComposeRateLimitData(snapshots []RateLimitSnapshotDisplay, now time.Time) RateLimitData {
	if len(snapshots) == 0 {
		return RateLimitData{Availability: RateLimitMissing}
	}
	var rows []StatusRateLimitRow
	stale := false
	for _, snap := range snapshots {
		if now.Sub(snap.CapturedAt) > time.Duration(RateLimitStaleThresholdMinutes)*time.Minute {
			stale = true
		}
		bucket := snap.LimitName
		showPrefix := !strings.EqualFold(bucket, "codex")
		windowCount := 0
		if snap.Primary != nil {
			windowCount++
		}
		if snap.Secondary != nil {
			windowCount++
		}
		combineSingle := showPrefix && windowCount == 1

		if showPrefix && !combineSingle {
			rows = append(rows, StatusRateLimitRow{Label: bucket + " limit"})
		}
		if snap.Primary != nil {
			label := capitalizeFirst(limitLabelForWindow(snap.Primary.WindowMins, false))
			rows = append(rows, windowRow(bucket, label, combineSingle, *snap.Primary))
		}
		if snap.Secondary != nil {
			label := capitalizeFirst(limitLabelForWindow(snap.Secondary.WindowMins, true))
			rows = append(rows, windowRow(bucket, label, combineSingle, *snap.Secondary))
		}
		if snap.Credits != nil {
			if row, ok := creditStatusRow(*snap.Credits); ok {
				rows = append(rows, row)
			}
		}
	}
	if len(rows) == 0 {
		return RateLimitData{Availability: RateLimitUnavailable}
	}
	if stale {
		return RateLimitData{Availability: RateLimitStale, Rows: rows}
	}
	return RateLimitData{Availability: RateLimitAvailable, Rows: rows}
}

// windowRow builds a window row, combining the bucket prefix for single-window
// non-codex limits (port of the label-building branches in compose).
func windowRow(bucket, label string, combineSingle bool, w RateLimitWindowDisplay) StatusRateLimitRow {
	full := label + " limit"
	if combineSingle {
		full = bucket + " " + full
	}
	return StatusRateLimitRow{
		Label:       full,
		PercentUsed: w.UsedPercent,
		ResetsAt:    w.ResetsAt,
		IsWindow:    true,
	}
}

// limitLabelForWindow maps a window duration to a label, mirroring the chatwidget
// helper limit_label_for_window referenced by rate_limits.rs.
func limitLabelForWindow(windowMins *int64, isSecondary bool) string {
	if windowMins == nil {
		return fallbackLimitLabel(isSecondary)
	}
	switch *windowMins {
	case 5 * 60:
		return "5h"
	case 60:
		return "hourly"
	case 24 * 60:
		return "daily"
	case 7 * 24 * 60:
		return "weekly"
	case 30 * 24 * 60:
		return "monthly"
	default:
		return fallbackLimitLabel(isSecondary)
	}
}

// fallbackLimitLabel returns the default window label (port of
// fallback_limit_label).
func fallbackLimitLabel(isSecondary bool) string {
	if isSecondary {
		return "secondary usage"
	}
	return "usage"
}

// capitalizeFirst upper-cases the first rune (port of text_formatting
// capitalize_first).
func capitalizeFirst(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	return strings.ToUpper(string(r[0])) + string(r[1:])
}

// creditStatusRow builds the credits row (port of credit_status_row).
func creditStatusRow(c CreditsSnapshotDisplay) (StatusRateLimitRow, bool) {
	if !c.HasCredits {
		return StatusRateLimitRow{}, false
	}
	if c.Unlimited {
		return StatusRateLimitRow{Label: "Credits", Text: "Unlimited"}, true
	}
	bal, ok := formatCreditBalance(c.Balance)
	if !ok {
		return StatusRateLimitRow{}, false
	}
	return StatusRateLimitRow{Label: "Credits", Text: bal + " credits"}, true
}

// formatCreditBalance rounds a raw balance string (port of format_credit_balance).
func formatCreditBalance(raw string) (string, bool) {
	t := strings.TrimSpace(raw)
	if t == "" {
		return "", false
	}
	var f float64
	if _, err := fmt.Sscanf(t, "%g", &f); err == nil && f > 0 {
		return fmt.Sprintf("%d", int64(math.Round(f))), true
	}
	return "", false
}

// RenderStatusLimitProgressBar renders a fixed-width bar from the remaining
// percentage (port of render_status_limit_progress_bar). Input is clamped to
// 0..=100.
func RenderStatusLimitProgressBar(percentRemaining float64) string {
	ratio := percentRemaining / 100.0
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	filled := int(math.Round(ratio * float64(statusLimitBarSegments)))
	if filled > statusLimitBarSegments {
		filled = statusLimitBarSegments
	}
	empty := statusLimitBarSegments - filled
	return "[" + strings.Repeat(statusLimitBarFilled, filled) + strings.Repeat(statusLimitBarEmpty, empty) + "]"
}

// FormatStatusLimitSummary formats a compact remaining-percentage summary (port
// of format_status_limit_summary).
func FormatStatusLimitSummary(percentRemaining float64) string {
	return fmt.Sprintf("%.0f%% left", percentRemaining)
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
