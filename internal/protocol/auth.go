package protocol

import (
	"encoding/json"
	"fmt"
	"strings"
)

// This file ports `auth.rs`: the model-provider plan type (`PlanType`), the
// canonical `KnownPlan` enum, and the (non-wire) refresh-token failure types.

// KnownPlan enumerates the plan tiers that Codex recognizes by name.
//
// Rust: `#[serde(rename_all = "lowercase")]` with explicit renames for the two
// usage-based variants, plus deserialize aliases "hc" (Enterprise) and
// "education" (Edu). It is encoded as a bare JSON string.
type KnownPlan string

const (
	KnownPlanFree                        KnownPlan = "free"
	KnownPlanGo                          KnownPlan = "go"
	KnownPlanPlus                        KnownPlan = "plus"
	KnownPlanPro                         KnownPlan = "pro"
	KnownPlanProLite                     KnownPlan = "prolite"
	KnownPlanTeam                        KnownPlan = "team"
	KnownPlanSelfServeBusinessUsageBased KnownPlan = "self_serve_business_usage_based"
	KnownPlanBusiness                    KnownPlan = "business"
	KnownPlanEnterpriseCbpUsageBased     KnownPlan = "enterprise_cbp_usage_based"
	KnownPlanEnterprise                  KnownPlan = "enterprise"
	KnownPlanEdu                         KnownPlan = "edu"
)

// parseKnownPlan maps a wire string (including the serde aliases) to a KnownPlan.
// It reports false when the value is not a recognized plan.
func parseKnownPlan(s string) (KnownPlan, bool) {
	switch s {
	case string(KnownPlanFree):
		return KnownPlanFree, true
	case string(KnownPlanGo):
		return KnownPlanGo, true
	case string(KnownPlanPlus):
		return KnownPlanPlus, true
	case string(KnownPlanPro):
		return KnownPlanPro, true
	case string(KnownPlanProLite):
		return KnownPlanProLite, true
	case string(KnownPlanTeam):
		return KnownPlanTeam, true
	case string(KnownPlanSelfServeBusinessUsageBased):
		return KnownPlanSelfServeBusinessUsageBased, true
	case string(KnownPlanBusiness):
		return KnownPlanBusiness, true
	case string(KnownPlanEnterpriseCbpUsageBased):
		return KnownPlanEnterpriseCbpUsageBased, true
	case string(KnownPlanEnterprise), "hc": // alias("hc")
		return KnownPlanEnterprise, true
	case string(KnownPlanEdu), "education": // alias("education")
		return KnownPlanEdu, true
	default:
		return "", false
	}
}

// DisplayName returns the human-readable label for a KnownPlan, mirroring the
// Rust `KnownPlan::display_name`.
func (p KnownPlan) DisplayName() string {
	switch p {
	case KnownPlanFree:
		return "Free"
	case KnownPlanGo:
		return "Go"
	case KnownPlanPlus:
		return "Plus"
	case KnownPlanPro:
		return "Pro"
	case KnownPlanProLite:
		return "Pro Lite"
	case KnownPlanTeam:
		return "Team"
	case KnownPlanSelfServeBusinessUsageBased:
		return "Self Serve Business Usage Based"
	case KnownPlanBusiness:
		return "Business"
	case KnownPlanEnterpriseCbpUsageBased:
		return "Enterprise CBP Usage Based"
	case KnownPlanEnterprise:
		return "Enterprise"
	case KnownPlanEdu:
		return "Edu"
	default:
		return ""
	}
}

// RawValue returns the canonical wire string for a KnownPlan, mirroring the Rust
// `KnownPlan::raw_value`.
func (p KnownPlan) RawValue() string {
	return string(p)
}

// IsWorkspaceAccount reports whether this plan is one of the workspace
// (team/business/enterprise/edu) tiers, mirroring Rust
// `KnownPlan::is_workspace_account`.
func (p KnownPlan) IsWorkspaceAccount() bool {
	switch p {
	case KnownPlanTeam,
		KnownPlanSelfServeBusinessUsageBased,
		KnownPlanBusiness,
		KnownPlanEnterpriseCbpUsageBased,
		KnownPlanEnterprise,
		KnownPlanEdu:
		return true
	default:
		return false
	}
}

// MarshalJSON encodes the KnownPlan as its canonical bare string.
func (p KnownPlan) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(p))
}

// UnmarshalJSON decodes a KnownPlan from a bare string, accepting the serde
// aliases. Unknown values are rejected (use AuthPlanType for tolerant parsing).
func (p *KnownPlan) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	plan, ok := parseKnownPlan(s)
	if !ok {
		return fmt.Errorf("unknown KnownPlan value: %q", s)
	}
	*p = plan
	return nil
}

// AuthPlanTypeKind discriminates the untagged AuthPlanType representation.
type AuthPlanTypeKind int

const (
	// AuthPlanTypeKnown wraps a recognized KnownPlan.
	AuthPlanTypeKnown AuthPlanTypeKind = iota
	// AuthPlanTypeUnknown wraps an unrecognized raw string.
	AuthPlanTypeUnknown
)

// AuthPlanType is the model-provider plan type from `auth.rs`.
//
// Rust: `#[serde(untagged)]` enum over `Known(KnownPlan)` and `Unknown(String)`.
// Both arms encode as a bare JSON string, so deserialization tries KnownPlan
// first and falls back to a raw string. Named `AuthPlanType` in Go to avoid a
// collision with the account-level `PlanType`.
type AuthPlanType struct {
	Kind AuthPlanTypeKind
	// Known holds the value when Kind == AuthPlanTypeKnown.
	Known KnownPlan
	// Unknown holds the raw string when Kind == AuthPlanTypeUnknown.
	Unknown string
}

// KnownAuthPlanType constructs an AuthPlanType for a recognized plan.
func KnownAuthPlanType(plan KnownPlan) AuthPlanType {
	return AuthPlanType{Kind: AuthPlanTypeKnown, Known: plan}
}

// UnknownAuthPlanType constructs an AuthPlanType for an unrecognized raw value.
func UnknownAuthPlanType(raw string) AuthPlanType {
	return AuthPlanType{Kind: AuthPlanTypeUnknown, Unknown: raw}
}

// AuthPlanTypeFromRawValue maps a raw value to an AuthPlanType, mirroring the
// Rust `PlanType::from_raw_value`. Matching is case-insensitive and includes the
// "hc"/"education" aliases.
func AuthPlanTypeFromRawValue(raw string) AuthPlanType {
	if plan, ok := parseKnownPlan(strings.ToLower(raw)); ok {
		return KnownAuthPlanType(plan)
	}
	return UnknownAuthPlanType(raw)
}

// MarshalJSON encodes the AuthPlanType as a bare string for either arm.
func (p AuthPlanType) MarshalJSON() ([]byte, error) {
	switch p.Kind {
	case AuthPlanTypeKnown:
		return json.Marshal(string(p.Known))
	case AuthPlanTypeUnknown:
		return json.Marshal(p.Unknown)
	default:
		return nil, fmt.Errorf("unknown AuthPlanType kind: %d", p.Kind)
	}
}

// UnmarshalJSON decodes the untagged AuthPlanType. A recognized plan string maps
// to Known; anything else (including the original casing) maps to Unknown.
func (p *AuthPlanType) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	if plan, ok := parseKnownPlan(s); ok {
		*p = KnownAuthPlanType(plan)
		return nil
	}
	*p = UnknownAuthPlanType(s)
	return nil
}

// RefreshTokenFailedReason classifies why a token refresh failed. Rust: a plain
// enum with no serde derive, so it has no wire representation.
type RefreshTokenFailedReason int

const (
	// RefreshTokenFailedExpired indicates the refresh token has expired.
	RefreshTokenFailedExpired RefreshTokenFailedReason = iota
	// RefreshTokenFailedExhausted indicates the refresh token was already used.
	RefreshTokenFailedExhausted
	// RefreshTokenFailedRevoked indicates the refresh token was revoked.
	RefreshTokenFailedRevoked
	// RefreshTokenFailedOther is a catch-all reason.
	RefreshTokenFailedOther
)

// RefreshTokenFailedError reports a failed token refresh. Rust: a `thiserror`
// error type with no serde derive (not a wire type).
type RefreshTokenFailedError struct {
	Reason  RefreshTokenFailedReason
	Message string
}

// NewRefreshTokenFailedError builds a RefreshTokenFailedError.
func NewRefreshTokenFailedError(reason RefreshTokenFailedReason, message string) RefreshTokenFailedError {
	return RefreshTokenFailedError{Reason: reason, Message: message}
}

// Error implements the error interface, mirroring the Rust `#[error("{message}")]`.
func (e RefreshTokenFailedError) Error() string {
	return e.Message
}
