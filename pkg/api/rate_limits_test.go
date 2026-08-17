package api

import (
	"net/http"
	"testing"
)

func TestParseRateLimitForLimitDefaultsToCodex(t *testing.T) {
	h := http.Header{}
	h.Set("x-codex-primary-used-percent", "12.5")
	h.Set("x-codex-primary-window-minutes", "60")
	h.Set("x-codex-primary-reset-at", "1704069000")

	snapshot := ParseRateLimitForLimit(h, "")
	if snapshot == nil {
		t.Fatalf("expected snapshot")
	}
	if snapshot.LimitID == nil || *snapshot.LimitID != "codex" {
		t.Fatalf("unexpected limit id: %v", snapshot.LimitID)
	}
	if snapshot.Primary == nil || snapshot.Primary.UsedPercent != 12.5 {
		t.Fatalf("unexpected primary: %+v", snapshot.Primary)
	}
	if snapshot.Primary.WindowMinutes == nil || *snapshot.Primary.WindowMinutes != 60 {
		t.Fatalf("unexpected window minutes")
	}
	if snapshot.Primary.ResetsAt == nil || *snapshot.Primary.ResetsAt != 1704069000 {
		t.Fatalf("unexpected resets at")
	}
}

func TestParseRateLimitForLimitPrefersLimitName(t *testing.T) {
	h := http.Header{}
	h.Set("x-codex-bengalfox-primary-used-percent", "80")
	h.Set("x-codex-bengalfox-limit-name", "gpt-5.2-codex-sonic")

	snapshot := ParseRateLimitForLimit(h, "codex_bengalfox")
	if snapshot.LimitID == nil || *snapshot.LimitID != "codex_bengalfox" {
		t.Fatalf("unexpected limit id: %v", snapshot.LimitID)
	}
	if snapshot.LimitName == nil || *snapshot.LimitName != "gpt-5.2-codex-sonic" {
		t.Fatalf("unexpected limit name: %v", snapshot.LimitName)
	}
}

func TestParseAllRateLimitsReadsAllFamilies(t *testing.T) {
	h := http.Header{}
	h.Set("x-codex-primary-used-percent", "12.5")
	h.Set("x-codex-secondary-primary-used-percent", "80")

	updates := ParseAllRateLimits(h)
	if len(updates) != 2 {
		t.Fatalf("expected 2 updates, got %d", len(updates))
	}
	if *updates[0].LimitID != "codex" {
		t.Fatalf("update 0 limit id %v", updates[0].LimitID)
	}
	if *updates[1].LimitID != "codex_secondary" {
		t.Fatalf("update 1 limit id %v", updates[1].LimitID)
	}
}

func TestParseAllRateLimitsIncludesDefaultCodexSnapshot(t *testing.T) {
	updates := ParseAllRateLimits(http.Header{})
	if len(updates) != 1 {
		t.Fatalf("expected 1 update, got %d", len(updates))
	}
	if *updates[0].LimitID != "codex" {
		t.Fatalf("unexpected limit id %v", updates[0].LimitID)
	}
	if updates[0].Primary != nil || updates[0].Secondary != nil || updates[0].Credits != nil {
		t.Fatalf("expected empty default snapshot")
	}
}

func TestParseRateLimitEvent(t *testing.T) {
	payload := `{"type":"codex.rate_limits","plan_type":"pro","rate_limits":{"primary":{"used_percent":42.0,"window_minutes":60,"reset_at":123}},"credits":{"has_credits":true,"unlimited":false,"balance":"5.00"},"metered_limit_name":"codex"}`
	snapshot := ParseRateLimitEvent(payload)
	if snapshot == nil {
		t.Fatalf("expected snapshot")
	}
	if snapshot.Primary == nil || snapshot.Primary.UsedPercent != 42.0 {
		t.Fatalf("unexpected primary: %+v", snapshot.Primary)
	}
	if snapshot.Credits == nil || !snapshot.Credits.HasCredits || snapshot.Credits.Balance == nil || *snapshot.Credits.Balance != "5.00" {
		t.Fatalf("unexpected credits: %+v", snapshot.Credits)
	}
}

func TestParseRateLimitEventWrongType(t *testing.T) {
	if ParseRateLimitEvent(`{"type":"other"}`) != nil {
		t.Fatalf("expected nil for non rate-limit event")
	}
}

func TestParsePromoMessage(t *testing.T) {
	h := http.Header{}
	h.Set("x-codex-promo-message", "  upgrade now  ")
	msg := ParsePromoMessage(h)
	if msg == nil || *msg != "upgrade now" {
		t.Fatalf("unexpected promo: %v", msg)
	}
	if ParsePromoMessage(http.Header{}) != nil {
		t.Fatalf("expected nil for missing promo")
	}
}
