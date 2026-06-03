package backendclient

import (
	"github.com/sqlrush/codexgo/internal/protocol"
)

// RateLimitStatusPayload mirrors the backend `/usage` JSON body. It is the
// body-based counterpart to the header-based parser in internal/api.
type RateLimitStatusPayload struct {
	PlanType             protocol.PlanType            `json:"plan_type"`
	RateLimit            *RateLimitStatusDetails      `json:"rate_limit"`
	AdditionalRateLimits []AdditionalRateLimitDetails `json:"additional_rate_limits"`
	Credits              *CreditStatusDetails         `json:"credits"`
	RateLimitReachedType *RateLimitReachedTypePayload `json:"rate_limit_reached_type"`
}

// RateLimitStatusDetails mirrors the backend rate-limit details object.
type RateLimitStatusDetails struct {
	PrimaryWindow   *RateLimitWindowSnapshot `json:"primary_window"`
	SecondaryWindow *RateLimitWindowSnapshot `json:"secondary_window"`
}

// RateLimitWindowSnapshot mirrors the backend rate-limit window object.
type RateLimitWindowSnapshot struct {
	UsedPercent        int32 `json:"used_percent"`
	LimitWindowSeconds int32 `json:"limit_window_seconds"`
	ResetAfterSeconds  int32 `json:"reset_after_seconds"`
	ResetAt            int32 `json:"reset_at"`
}

// AdditionalRateLimitDetails mirrors the backend additional-rate-limit object.
type AdditionalRateLimitDetails struct {
	LimitName      string                  `json:"limit_name"`
	MeteredFeature string                  `json:"metered_feature"`
	RateLimit      *RateLimitStatusDetails `json:"rate_limit"`
}

// CreditStatusDetails mirrors the backend credit-status object.
type CreditStatusDetails struct {
	HasCredits bool    `json:"has_credits"`
	Unlimited  bool    `json:"unlimited"`
	Balance    *string `json:"balance"`
}

// RateLimitReachedTypePayload mirrors the backend rate-limit-reached object.
type RateLimitReachedTypePayload struct {
	Kind RateLimitReachedKind `json:"kind"`
}

// RateLimitReachedKind enumerates the backend reasons a rate limit was reached.
type RateLimitReachedKind string

const (
	// RateLimitReachedKindRateLimitReached indicates a generic rate-limit hit.
	RateLimitReachedKindRateLimitReached RateLimitReachedKind = "rate_limit_reached"
	// RateLimitReachedKindWorkspaceOwnerCreditsDepleted indicates owner credits ran out.
	RateLimitReachedKindWorkspaceOwnerCreditsDepleted RateLimitReachedKind = "workspace_owner_credits_depleted"
	// RateLimitReachedKindWorkspaceMemberCreditsDepleted indicates member credits ran out.
	RateLimitReachedKindWorkspaceMemberCreditsDepleted RateLimitReachedKind = "workspace_member_credits_depleted"
	// RateLimitReachedKindWorkspaceOwnerUsageLimitReached indicates owner usage limit reached.
	RateLimitReachedKindWorkspaceOwnerUsageLimitReached RateLimitReachedKind = "workspace_owner_usage_limit_reached"
	// RateLimitReachedKindWorkspaceMemberUsageLimitReached indicates member usage limit reached.
	RateLimitReachedKindWorkspaceMemberUsageLimitReached RateLimitReachedKind = "workspace_member_usage_limit_reached"
	// RateLimitReachedKindUnknown indicates an unrecognized reason.
	RateLimitReachedKindUnknown RateLimitReachedKind = "unknown"
)

// rateLimitSnapshotsFromPayload converts a payload into snapshots, mirroring the
// Rust `Client::rate_limit_snapshots_from_payload`.
func rateLimitSnapshotsFromPayload(payload RateLimitStatusPayload) []protocol.RateLimitSnapshot {
	planType := payload.PlanType
	var reachedType *protocol.RateLimitReachedType
	if payload.RateLimitReachedType != nil {
		reachedType = mapRateLimitReachedType(payload.RateLimitReachedType.Kind)
	}

	codexID := "codex"
	snapshots := []protocol.RateLimitSnapshot{
		makeRateLimitSnapshot(&codexID, nil, payload.RateLimit, payload.Credits, &planType, reachedType),
	}

	for _, additional := range payload.AdditionalRateLimits {
		metered := additional.MeteredFeature
		limitName := additional.LimitName
		snapshots = append(snapshots, makeRateLimitSnapshot(
			&metered, &limitName, additional.RateLimit, nil, &planType, nil,
		))
	}
	return snapshots
}

func makeRateLimitSnapshot(
	limitID *string,
	limitName *string,
	rateLimit *RateLimitStatusDetails,
	credits *CreditStatusDetails,
	planType *protocol.PlanType,
	reachedType *protocol.RateLimitReachedType,
) protocol.RateLimitSnapshot {
	var primary, secondary *protocol.RateLimitWindow
	if rateLimit != nil {
		primary = mapRateLimitWindow(rateLimit.PrimaryWindow)
		secondary = mapRateLimitWindow(rateLimit.SecondaryWindow)
	}
	return protocol.RateLimitSnapshot{
		LimitID:              limitID,
		LimitName:            limitName,
		Primary:              primary,
		Secondary:            secondary,
		Credits:              mapCredits(credits),
		PlanType:             planType,
		RateLimitReachedType: reachedType,
	}
}

func mapRateLimitWindow(window *RateLimitWindowSnapshot) *protocol.RateLimitWindow {
	if window == nil {
		return nil
	}
	usedPercent := float64(window.UsedPercent)
	windowMinutes := windowMinutesFromSeconds(window.LimitWindowSeconds)
	resetsAt := int64(window.ResetAt)
	return &protocol.RateLimitWindow{
		UsedPercent:   usedPercent,
		WindowMinutes: windowMinutes,
		ResetsAt:      &resetsAt,
	}
}

func mapCredits(credits *CreditStatusDetails) *protocol.CreditsSnapshot {
	if credits == nil {
		return nil
	}
	return &protocol.CreditsSnapshot{
		HasCredits: credits.HasCredits,
		Unlimited:  credits.Unlimited,
		Balance:    credits.Balance,
	}
}

func mapRateLimitReachedType(kind RateLimitReachedKind) *protocol.RateLimitReachedType {
	var t protocol.RateLimitReachedType
	switch kind {
	case RateLimitReachedKindRateLimitReached:
		t = protocol.RateLimitReachedTypeRateLimitReached
	case RateLimitReachedKindWorkspaceOwnerCreditsDepleted:
		t = protocol.RateLimitReachedTypeWorkspaceOwnerCreditsDepleted
	case RateLimitReachedKindWorkspaceMemberCreditsDepleted:
		t = protocol.RateLimitReachedTypeWorkspaceMemberCreditsDepleted
	case RateLimitReachedKindWorkspaceOwnerUsageLimitReached:
		t = protocol.RateLimitReachedTypeWorkspaceOwnerUsageLimitReached
	case RateLimitReachedKindWorkspaceMemberUsageLimitReached:
		t = protocol.RateLimitReachedTypeWorkspaceMemberUsageLimitReached
	default:
		return nil
	}
	return &t
}

func windowMinutesFromSeconds(seconds int32) *int64 {
	if seconds <= 0 {
		return nil
	}
	v := (int64(seconds) + 59) / 60
	return &v
}
