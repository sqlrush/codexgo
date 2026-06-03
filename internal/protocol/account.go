package protocol

import (
	"encoding/json"
)

// This file ports `account.rs`: the app-facing `PlanType` wire enum, the
// (non-wire) `ProviderAccount` enum, and the conversions from the auth-level
// plan types.

// PlanType is the app-facing account plan type.
//
// Rust: `#[serde(rename_all = "lowercase")]`, `#[derive(Default)]` with `Free`
// as the default, explicit renames for the two usage-based variants, and an
// `#[serde(other)]` `Unknown` fallback. It is encoded as a bare JSON string.
// Unknown deserializes from any unrecognized string and serializes to "unknown".
type PlanType string

const (
	// PlanTypeFree is the default plan.
	PlanTypeFree                        PlanType = "free"
	PlanTypeGo                          PlanType = "go"
	PlanTypePlus                        PlanType = "plus"
	PlanTypePro                         PlanType = "pro"
	PlanTypeProLite                     PlanType = "prolite"
	PlanTypeTeam                        PlanType = "team"
	PlanTypeSelfServeBusinessUsageBased PlanType = "self_serve_business_usage_based"
	PlanTypeBusiness                    PlanType = "business"
	PlanTypeEnterpriseCbpUsageBased     PlanType = "enterprise_cbp_usage_based"
	PlanTypeEnterprise                  PlanType = "enterprise"
	PlanTypeEdu                         PlanType = "edu"
	// PlanTypeUnknown is the catch-all for unrecognized plans (#[serde(other)]).
	PlanTypeUnknown PlanType = "unknown"
)

// DefaultPlanType returns the Rust `#[default]` value (Free).
func DefaultPlanType() PlanType { return PlanTypeFree }

// isKnownPlanType reports whether s is one of the explicitly named PlanType
// wire values (excluding the Unknown fallback).
func isKnownPlanType(s string) bool {
	switch PlanType(s) {
	case PlanTypeFree,
		PlanTypeGo,
		PlanTypePlus,
		PlanTypePro,
		PlanTypeProLite,
		PlanTypeTeam,
		PlanTypeSelfServeBusinessUsageBased,
		PlanTypeBusiness,
		PlanTypeEnterpriseCbpUsageBased,
		PlanTypeEnterprise,
		PlanTypeEdu:
		return true
	default:
		return false
	}
}

// IsTeamLike reports whether the plan groups with team-style billing, mirroring
// Rust `PlanType::is_team_like`.
func (p PlanType) IsTeamLike() bool {
	switch p {
	case PlanTypeTeam, PlanTypeSelfServeBusinessUsageBased:
		return true
	default:
		return false
	}
}

// IsBusinessLike reports whether the plan groups with business-style billing,
// mirroring Rust `PlanType::is_business_like`.
func (p PlanType) IsBusinessLike() bool {
	switch p {
	case PlanTypeBusiness, PlanTypeEnterpriseCbpUsageBased:
		return true
	default:
		return false
	}
}

// IsWorkspaceAccount reports whether the plan is a workspace account tier,
// mirroring Rust `PlanType::is_workspace_account`.
func (p PlanType) IsWorkspaceAccount() bool {
	switch p {
	case PlanTypeTeam,
		PlanTypeSelfServeBusinessUsageBased,
		PlanTypeBusiness,
		PlanTypeEnterpriseCbpUsageBased,
		PlanTypeEnterprise,
		PlanTypeEdu:
		return true
	default:
		return false
	}
}

// MarshalJSON encodes the PlanType as its bare string value.
func (p PlanType) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(p))
}

// UnmarshalJSON decodes a PlanType. Any unrecognized string maps to Unknown,
// reproducing the Rust `#[serde(other)]` behavior.
func (p *PlanType) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	if isKnownPlanType(s) {
		*p = PlanType(s)
		return nil
	}
	*p = PlanTypeUnknown
	return nil
}

// PlanTypeFromKnownPlan converts an auth-level KnownPlan to the account PlanType,
// mirroring the Rust `From<KnownPlan> for PlanType`.
func PlanTypeFromKnownPlan(plan KnownPlan) PlanType {
	switch plan {
	case KnownPlanFree:
		return PlanTypeFree
	case KnownPlanGo:
		return PlanTypeGo
	case KnownPlanPlus:
		return PlanTypePlus
	case KnownPlanPro:
		return PlanTypePro
	case KnownPlanProLite:
		return PlanTypeProLite
	case KnownPlanTeam:
		return PlanTypeTeam
	case KnownPlanSelfServeBusinessUsageBased:
		return PlanTypeSelfServeBusinessUsageBased
	case KnownPlanBusiness:
		return PlanTypeBusiness
	case KnownPlanEnterpriseCbpUsageBased:
		return PlanTypeEnterpriseCbpUsageBased
	case KnownPlanEnterprise:
		return PlanTypeEnterprise
	case KnownPlanEdu:
		return PlanTypeEdu
	default:
		return PlanTypeUnknown
	}
}

// PlanTypeFromAuthPlanType converts an auth-level AuthPlanType to the account
// PlanType, mirroring the Rust `From<auth::PlanType> for PlanType`.
func PlanTypeFromAuthPlanType(plan AuthPlanType) PlanType {
	switch plan.Kind {
	case AuthPlanTypeKnown:
		return PlanTypeFromKnownPlan(plan.Known)
	default:
		return PlanTypeUnknown
	}
}

// ProviderAccountKind discriminates the ProviderAccount representation.
type ProviderAccountKind int

const (
	// ProviderAccountAPIKey is an API-key-authenticated account.
	ProviderAccountAPIKey ProviderAccountKind = iota
	// ProviderAccountChatgpt is a ChatGPT-authenticated account.
	ProviderAccountChatgpt
	// ProviderAccountAmazonBedrock is an Amazon Bedrock account.
	ProviderAccountAmazonBedrock
)

// ProviderAccount is the account state returned by a model provider before it is
// adapted to an app-facing wire type. Rust: a plain enum with no serde derive,
// so it has no wire representation; modeled here as a tagged struct.
type ProviderAccount struct {
	Kind ProviderAccountKind

	// Email is populated only when Kind == ProviderAccountChatgpt.
	Email string
	// PlanType is populated only when Kind == ProviderAccountChatgpt.
	PlanType PlanType
}

// NewAPIKeyProviderAccount builds an API-key ProviderAccount.
func NewAPIKeyProviderAccount() ProviderAccount {
	return ProviderAccount{Kind: ProviderAccountAPIKey}
}

// NewChatgptProviderAccount builds a ChatGPT ProviderAccount.
func NewChatgptProviderAccount(email string, plan PlanType) ProviderAccount {
	return ProviderAccount{Kind: ProviderAccountChatgpt, Email: email, PlanType: plan}
}

// NewAmazonBedrockProviderAccount builds an Amazon Bedrock ProviderAccount.
func NewAmazonBedrockProviderAccount() ProviderAccount {
	return ProviderAccount{Kind: ProviderAccountAmazonBedrock}
}
