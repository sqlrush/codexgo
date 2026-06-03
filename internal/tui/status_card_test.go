package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/protocol"
)

func TestFormatTokensCompact(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{0, "0"},
		{-5, "0"},
		{42, "42"},
		{999, "999"},
		{1000, "1K"},
		{1500, "1.5K"},
		{12_345, "12.3K"},
		{123_456, "123K"},
		{1_000_000, "1M"},
		{1_500_000, "1.5M"},
		{2_300_000_000, "2.3B"},
		{1_000_000_000_000, "1T"},
	}
	for _, tc := range tests {
		if got := FormatTokensCompact(tc.in); got != tc.want {
			t.Errorf("FormatTokensCompact(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPlanTypeDisplayName(t *testing.T) {
	tests := []struct {
		in   protocol.PlanType
		want string
	}{
		{protocol.PlanTypeFree, "Free"},
		{protocol.PlanTypeGo, "Go"},
		{protocol.PlanTypePlus, "Plus"},
		{protocol.PlanTypePro, "Pro"},
		{protocol.PlanTypeProLite, "Pro Lite"},
		{protocol.PlanTypeTeam, "Business"},
		{protocol.PlanTypeBusiness, "Enterprise"},
		{protocol.PlanTypeEnterprise, "Enterprise"},
		{protocol.PlanTypeEdu, "Edu"},
		{protocol.PlanTypeUnknown, "Unknown"},
	}
	for _, tc := range tests {
		if got := PlanTypeDisplayName(tc.in); got != tc.want {
			t.Errorf("PlanTypeDisplayName(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRenderStatusLimitProgressBar(t *testing.T) {
	tests := []struct {
		remaining float64
		want      string
	}{
		{100, "[████████████████████]"},
		{0, "[░░░░░░░░░░░░░░░░░░░░]"},
		{50, "[██████████░░░░░░░░░░]"},
		{150, "[████████████████████]"}, // clamps high
		{-10, "[░░░░░░░░░░░░░░░░░░░░]"}, // clamps low
	}
	for _, tc := range tests {
		if got := RenderStatusLimitProgressBar(tc.remaining); got != tc.want {
			t.Errorf("RenderStatusLimitProgressBar(%v) = %q, want %q", tc.remaining, got, tc.want)
		}
	}
}

func TestComposeRateLimitDataMissingAndStale(t *testing.T) {
	now := time.Now()
	if got := ComposeRateLimitData(nil, now); got.Availability != RateLimitMissing {
		t.Fatalf("nil snapshots should be Missing, got %v", got.Availability)
	}

	mins := int64(5 * 60)
	fresh := RateLimitSnapshotDisplay{
		LimitName:  "codex",
		CapturedAt: now,
		Primary:    &RateLimitWindowDisplay{UsedPercent: 10, WindowMins: &mins},
	}
	if got := ComposeRateLimitData([]RateLimitSnapshotDisplay{fresh}, now); got.Availability != RateLimitAvailable {
		t.Fatalf("fresh snapshot should be Available, got %v", got.Availability)
	}

	stale := fresh
	stale.CapturedAt = now.Add(-30 * time.Minute)
	if got := ComposeRateLimitData([]RateLimitSnapshotDisplay{stale}, now); got.Availability != RateLimitStale {
		t.Fatalf("old snapshot should be Stale, got %v", got.Availability)
	}
}

func TestComposeRateLimitNonCodexSingleLimitCombines(t *testing.T) {
	now := time.Now()
	mins := int64(5 * 60)
	codex := RateLimitSnapshotDisplay{
		LimitName:  "codex",
		CapturedAt: now,
		Primary:    &RateLimitWindowDisplay{UsedPercent: 10, WindowMins: &mins},
		Credits:    &CreditsSnapshotDisplay{HasCredits: true, Balance: "25"},
	}
	other := RateLimitSnapshotDisplay{
		LimitName:  "codex-other",
		CapturedAt: now,
		Primary:    &RateLimitWindowDisplay{UsedPercent: 20, WindowMins: &mins},
		Credits:    &CreditsSnapshotDisplay{HasCredits: true, Balance: "99"},
	}
	data := ComposeRateLimitData([]RateLimitSnapshotDisplay{codex, other}, now)
	var labels []string
	for _, r := range data.Rows {
		labels = append(labels, r.Label)
	}
	want := []string{"5h limit", "Credits", "codex-other 5h limit", "Credits"}
	if len(labels) != len(want) {
		t.Fatalf("labels = %v, want %v", labels, want)
	}
	for i := range want {
		if labels[i] != want[i] {
			t.Fatalf("labels[%d] = %q, want %q (all=%v)", i, labels[i], want[i], labels)
		}
	}
}

func TestCreditsRowUnlimited(t *testing.T) {
	now := time.Now()
	snap := RateLimitSnapshotDisplay{
		LimitName:  "codex",
		CapturedAt: now,
		Credits:    &CreditsSnapshotDisplay{HasCredits: true, Unlimited: true},
	}
	data := ComposeRateLimitData([]RateLimitSnapshotDisplay{snap}, now)
	if len(data.Rows) != 1 || data.Rows[0].Label != "Credits" || data.Rows[0].Text != "Unlimited" {
		t.Fatalf("expected Unlimited credits row, got %+v", data.Rows)
	}
}

func TestFmtElapsedCompact(t *testing.T) {
	tests := []struct {
		secs int64
		want string
	}{
		{0, "0s"},
		{5, "5s"},
		{59, "59s"},
		{60, "1m 00s"},
		{125, "2m 05s"},
		{3600, "1h 00m 00s"},
		{3725, "1h 02m 05s"},
	}
	for _, tc := range tests {
		if got := FmtElapsedCompact(tc.secs); got != tc.want {
			t.Errorf("FmtElapsedCompact(%d) = %q, want %q", tc.secs, got, tc.want)
		}
	}
}

func TestRunningSpinnerLineContainsHeaderAndElapsed(t *testing.T) {
	theme := DefaultTheme(Capabilities{})
	sp := NewRunningSpinner(false).WithElapsed(65 * time.Second)
	line := sp.Line(theme)
	if !tuiTextContains(line, "Working") {
		t.Fatalf("spinner line should contain header, got %q", line)
	}
	if !tuiTextContains(line, "1m 05s") {
		t.Fatalf("spinner line should contain elapsed, got %q", line)
	}
	if !tuiTextContains(line, "esc") || !tuiTextContains(line, "interrupt") {
		t.Fatalf("spinner line should contain interrupt hint, got %q", line)
	}
}

func TestRunningSpinnerHeaderDefaultsNonEmpty(t *testing.T) {
	sp := NewRunningSpinner(true).WithHeader("")
	if sp.Header != "Working" {
		t.Fatalf("empty header should default to Working, got %q", sp.Header)
	}
}

func TestStatusAccountLabel(t *testing.T) {
	chat := StatusAccountDisplay{Kind: AccountChatGPT, Email: "a@b.com", Plan: "Pro"}
	if got := chat.Label(); got != "ChatGPT (Pro) a@b.com" {
		t.Fatalf("chatgpt label = %q", got)
	}
	api := StatusAccountDisplay{Kind: AccountAPIKey}
	if got := api.Label(); got != "API key" {
		t.Fatalf("api label = %q", got)
	}
}

func TestStatusTokenUsageFromInfo(t *testing.T) {
	window := int64(1000)
	info := &protocol.TokenUsageInfo{
		TotalTokenUsage: protocol.TokenUsage{
			InputTokens:  300,
			OutputTokens: 200,
			TotalTokens:  500,
		},
		ModelContextWindow: &window,
	}
	tu := StatusTokenUsageFromInfo(info)
	if tu.Total != 500 || tu.Input != 300 || tu.Output != 200 {
		t.Fatalf("unexpected totals: %+v", tu)
	}
	if tu.ContextWindow == nil {
		t.Fatalf("expected context window")
	}
	if tu.ContextWindow.PercentRemaining != 50 {
		t.Fatalf("expected 50%% remaining, got %d", tu.ContextWindow.PercentRemaining)
	}
}

func TestFormatResetTimestampSameDay(t *testing.T) {
	captured := time.Date(2026, 6, 3, 10, 0, 0, 0, time.Local)
	reset := time.Date(2026, 6, 3, 14, 30, 0, 0, time.Local)
	if got := FormatResetTimestamp(reset, captured); got != "14:30" {
		t.Fatalf("same-day reset = %q, want 14:30", got)
	}
	nextDay := time.Date(2026, 6, 5, 9, 5, 0, 0, time.Local)
	if got := FormatResetTimestamp(nextDay, captured); got != "09:05 on 5 Jun" {
		t.Fatalf("cross-day reset = %q, want '09:05 on 5 Jun'", got)
	}
}

// tuiTextContains reports whether rendered text contains a substring. It is used
// by the onboarding/status/resume-picker render assertions (named distinctly to
// avoid colliding with sibling test helpers in the shared package).
func tuiTextContains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
