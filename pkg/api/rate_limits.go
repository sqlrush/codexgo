package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// ParseDefaultRateLimit parses the default Codex rate-limit header family into a
// RateLimitSnapshot. It mirrors the Rust `parse_default_rate_limit`.
func ParseDefaultRateLimit(headers http.Header) *protocol.RateLimitSnapshot {
	return ParseRateLimitForLimit(headers, "")
}

// ParseAllRateLimits parses all known rate-limit header families into snapshots
// keyed by limit id. It mirrors the Rust `parse_all_rate_limits`.
func ParseAllRateLimits(headers http.Header) []protocol.RateLimitSnapshot {
	var snapshots []protocol.RateLimitSnapshot
	if snapshot := ParseDefaultRateLimit(headers); snapshot != nil {
		snapshots = append(snapshots, *snapshot)
	}

	limitIDs := map[string]struct{}{}
	for name := range headers {
		lower := strings.ToLower(name)
		if id, ok := headerNameToLimitID(lower); ok && id != "codex" {
			limitIDs[id] = struct{}{}
		}
	}
	sorted := make([]string, 0, len(limitIDs))
	for id := range limitIDs {
		sorted = append(sorted, id)
	}
	sort.Strings(sorted)

	for _, id := range sorted {
		snapshot := ParseRateLimitForLimit(headers, id)
		if snapshot != nil && hasRateLimitData(snapshot) {
			snapshots = append(snapshots, *snapshot)
		}
	}
	return snapshots
}

// ParseRateLimitForLimit parses rate-limit headers for the provided limit id. An
// empty limitID defaults to the legacy "codex" header family. It mirrors the
// Rust `parse_rate_limit_for_limit`.
func ParseRateLimitForLimit(headers http.Header, limitID string) *protocol.RateLimitSnapshot {
	normalized := strings.TrimSpace(limitID)
	if normalized == "" {
		normalized = "codex"
	}
	normalized = strings.ReplaceAll(strings.ToLower(normalized), "_", "-")
	prefix := "x-" + normalized

	primary := parseRateLimitWindow(headers,
		prefix+"-primary-used-percent",
		prefix+"-primary-window-minutes",
		prefix+"-primary-reset-at",
	)
	secondary := parseRateLimitWindow(headers,
		prefix+"-secondary-used-percent",
		prefix+"-secondary-window-minutes",
		prefix+"-secondary-reset-at",
	)

	normalizedLimitID := normalizeLimitID(normalized)
	credits := parseCreditsSnapshot(headers)
	limitName := strings.TrimSpace(headerStr(headers, prefix+"-limit-name"))
	var limitNamePtr *string
	if limitName != "" {
		limitNamePtr = &limitName
	}

	idCopy := normalizedLimitID
	return &protocol.RateLimitSnapshot{
		LimitID:   &idCopy,
		LimitName: limitNamePtr,
		Primary:   primary,
		Secondary: secondary,
		Credits:   credits,
	}
}

// rateLimitEventWindow mirrors the Rust deserialization struct for SSE rate
// limit events.
type rateLimitEventWindow struct {
	UsedPercent   float64 `json:"used_percent"`
	WindowMinutes *int64  `json:"window_minutes"`
	ResetAt       *int64  `json:"reset_at"`
}

type rateLimitEventDetails struct {
	Primary   *rateLimitEventWindow `json:"primary"`
	Secondary *rateLimitEventWindow `json:"secondary"`
}

type rateLimitEventCredits struct {
	HasCredits bool    `json:"has_credits"`
	Unlimited  bool    `json:"unlimited"`
	Balance    *string `json:"balance"`
}

type rateLimitEvent struct {
	Kind             string                 `json:"type"`
	PlanType         *protocol.PlanType     `json:"plan_type"`
	RateLimits       *rateLimitEventDetails `json:"rate_limits"`
	Credits          *rateLimitEventCredits `json:"credits"`
	MeteredLimitName *string                `json:"metered_limit_name"`
	LimitName        *string                `json:"limit_name"`
}

// ParseRateLimitEvent parses a "codex.rate_limits" SSE/WS event payload into a
// RateLimitSnapshot. It mirrors the Rust `parse_rate_limit_event`.
func ParseRateLimitEvent(payload string) *protocol.RateLimitSnapshot {
	var event rateLimitEvent
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return nil
	}
	if event.Kind != "codex.rate_limits" {
		return nil
	}
	var primary, secondary *protocol.RateLimitWindow
	if event.RateLimits != nil {
		primary = mapEventWindow(event.RateLimits.Primary)
		secondary = mapEventWindow(event.RateLimits.Secondary)
	}
	var credits *protocol.CreditsSnapshot
	if event.Credits != nil {
		credits = &protocol.CreditsSnapshot{
			HasCredits: event.Credits.HasCredits,
			Unlimited:  event.Credits.Unlimited,
			Balance:    event.Credits.Balance,
		}
	}
	limitID := "codex"
	if event.MeteredLimitName != nil {
		limitID = normalizeLimitID(*event.MeteredLimitName)
	} else if event.LimitName != nil {
		limitID = normalizeLimitID(*event.LimitName)
	}
	return &protocol.RateLimitSnapshot{
		LimitID:   &limitID,
		Primary:   primary,
		Secondary: secondary,
		Credits:   credits,
		PlanType:  event.PlanType,
	}
}

func mapEventWindow(window *rateLimitEventWindow) *protocol.RateLimitWindow {
	if window == nil {
		return nil
	}
	return &protocol.RateLimitWindow{
		UsedPercent:   window.UsedPercent,
		WindowMinutes: window.WindowMinutes,
		ResetsAt:      window.ResetAt,
	}
}

// ParsePromoMessage parses the Codex promo-message header. It mirrors the Rust
// `parse_promo_message`.
func ParsePromoMessage(headers http.Header) *string {
	value := strings.TrimSpace(headerStr(headers, "x-codex-promo-message"))
	if value == "" {
		return nil
	}
	return &value
}

// ParseRateLimitReachedType parses the rate-limit-reached-type header. It mirrors
// the Rust `parse_rate_limit_reached_type`.
func ParseRateLimitReachedType(headers http.Header) *protocol.RateLimitReachedType {
	raw := strings.TrimSpace(headerStr(headers, "x-codex-rate-limit-reached-type"))
	if raw == "" {
		return nil
	}
	t := protocol.RateLimitReachedType(raw)
	return &t
}

func parseRateLimitWindow(headers http.Header, usedPercentHeader, windowMinutesHeader, resetsAtHeader string) *protocol.RateLimitWindow {
	usedPercent, ok := headerF64(headers, usedPercentHeader)
	if !ok {
		return nil
	}
	windowMinutes := headerI64Ptr(headers, windowMinutesHeader)
	resetsAt := headerI64Ptr(headers, resetsAtHeader)

	hasData := usedPercent != 0.0 ||
		(windowMinutes != nil && *windowMinutes != 0) ||
		resetsAt != nil
	if !hasData {
		return nil
	}
	return &protocol.RateLimitWindow{
		UsedPercent:   usedPercent,
		WindowMinutes: windowMinutes,
		ResetsAt:      resetsAt,
	}
}

func parseCreditsSnapshot(headers http.Header) *protocol.CreditsSnapshot {
	hasCredits, ok := headerBool(headers, "x-codex-credits-has-credits")
	if !ok {
		return nil
	}
	unlimited, ok := headerBool(headers, "x-codex-credits-unlimited")
	if !ok {
		return nil
	}
	balance := strings.TrimSpace(headerStr(headers, "x-codex-credits-balance"))
	var balancePtr *string
	if balance != "" {
		balancePtr = &balance
	}
	return &protocol.CreditsSnapshot{
		HasCredits: hasCredits,
		Unlimited:  unlimited,
		Balance:    balancePtr,
	}
}

func headerF64(headers http.Header, name string) (float64, bool) {
	raw := headerStr(headers, name)
	if raw == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || isNonFinite(v) {
		return 0, false
	}
	return v, true
}

func isNonFinite(v float64) bool {
	return v != v || v > 1e308 || v < -1e308
}

func headerI64Ptr(headers http.Header, name string) *int64 {
	raw := headerStr(headers, name)
	if raw == "" {
		return nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil
	}
	return &v
}

func headerBool(headers http.Header, name string) (bool, bool) {
	raw := headerStr(headers, name)
	if raw == "" {
		return false, false
	}
	if strings.EqualFold(raw, "true") || raw == "1" {
		return true, true
	}
	if strings.EqualFold(raw, "false") || raw == "0" {
		return false, true
	}
	return false, false
}

// headerStr returns the first value of the named header (case-insensitive), or
// "" when absent.
func headerStr(headers http.Header, name string) string {
	if headers == nil {
		return ""
	}
	return headers.Get(name)
}

func hasRateLimitData(snapshot *protocol.RateLimitSnapshot) bool {
	return snapshot.Primary != nil || snapshot.Secondary != nil || snapshot.Credits != nil
}

func headerNameToLimitID(headerName string) (string, bool) {
	const suffix = "-primary-used-percent"
	prefix, ok := strings.CutSuffix(headerName, suffix)
	if !ok {
		return "", false
	}
	limit, ok := strings.CutPrefix(prefix, "x-")
	if !ok {
		return "", false
	}
	return normalizeLimitID(limit), true
}

func normalizeLimitID(name string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), "-", "_")
}
