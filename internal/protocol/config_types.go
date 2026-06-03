package protocol

// This file ports `config_types.rs`: configuration value enums and structs.
// Each enum is modeled as a string-typed Go alias with named constants; the
// underlying string values match the Rust serde rename_all output exactly.

// AutoCompactTokenLimitScope selects which part of the active context is charged
// against `model_auto_compact_token_limit`. Rust: snake_case.
type AutoCompactTokenLimitScope string

const (
	AutoCompactTokenLimitScopeTotal           AutoCompactTokenLimitScope = "total"
	AutoCompactTokenLimitScopeBodyAfterPrefix AutoCompactTokenLimitScope = "body_after_prefix"
)

// ReasoningSummary controls reasoning summary detail. Rust: lowercase.
type ReasoningSummary string

const (
	ReasoningSummaryAuto     ReasoningSummary = "auto"
	ReasoningSummaryConcise  ReasoningSummary = "concise"
	ReasoningSummaryDetailed ReasoningSummary = "detailed"
	ReasoningSummaryNone     ReasoningSummary = "none"
)

// Verbosity controls output length/detail on GPT-5 models. Rust: lowercase.
type Verbosity string

const (
	VerbosityLow    Verbosity = "low"
	VerbosityMedium Verbosity = "medium"
	VerbosityHigh   Verbosity = "high"
)

// SandboxMode is the high-level sandbox setting. Rust: kebab-case with explicit
// renames matching the kebab-case form.
type SandboxMode string

const (
	SandboxModeReadOnly         SandboxMode = "read-only"
	SandboxModeWorkspaceWrite   SandboxMode = "workspace-write"
	SandboxModeDangerFullAccess SandboxMode = "danger-full-access"
)

// ApprovalsReviewer configures who approval requests are routed to. Rust uses
// explicit renames: "user" and "guardian_subagent" (with "auto_review" accepted
// as a deserialize alias). The canonical serialized form for AutoReview is
// "guardian_subagent".
type ApprovalsReviewer string

const (
	ApprovalsReviewerUser       ApprovalsReviewer = "user"
	ApprovalsReviewerAutoReview ApprovalsReviewer = "guardian_subagent"
)

// NormalizeApprovalsReviewer maps any accepted on-the-wire alias to the
// canonical value, mirroring Rust's `alias = "auto_review"`.
func NormalizeApprovalsReviewer(v string) ApprovalsReviewer {
	switch v {
	case "user":
		return ApprovalsReviewerUser
	case "auto_review", "guardian_subagent":
		return ApprovalsReviewerAutoReview
	default:
		return ApprovalsReviewer(v)
	}
}

// ShellEnvironmentPolicyInherit selects the starting point for the shell
// environment. Rust: kebab-case (single-word variants are unchanged).
type ShellEnvironmentPolicyInherit string

const (
	ShellEnvironmentPolicyInheritCore ShellEnvironmentPolicyInherit = "core"
	ShellEnvironmentPolicyInheritAll  ShellEnvironmentPolicyInherit = "all"
	ShellEnvironmentPolicyInheritNone ShellEnvironmentPolicyInherit = "none"
)

// WindowsSandboxLevel selects the Windows sandbox enforcement level. Rust:
// kebab-case.
type WindowsSandboxLevel string

const (
	WindowsSandboxLevelDisabled        WindowsSandboxLevel = "disabled"
	WindowsSandboxLevelRestrictedToken WindowsSandboxLevel = "restricted-token"
	WindowsSandboxLevelElevated        WindowsSandboxLevel = "elevated"
)

// Personality selects the assistant personality. Rust: lowercase.
type Personality string

const (
	PersonalityNone      Personality = "none"
	PersonalityFriendly  Personality = "friendly"
	PersonalityPragmatic Personality = "pragmatic"
)

// WebSearchMode selects the web search behavior. Rust: lowercase.
type WebSearchMode string

const (
	WebSearchModeDisabled WebSearchMode = "disabled"
	WebSearchModeCached   WebSearchMode = "cached"
	WebSearchModeLive     WebSearchMode = "live"
)

// WebSearchContextSize selects the web search context size. Rust: lowercase.
type WebSearchContextSize string

const (
	WebSearchContextSizeLow    WebSearchContextSize = "low"
	WebSearchContextSizeMedium WebSearchContextSize = "medium"
	WebSearchContextSizeHigh   WebSearchContextSize = "high"
)

// WebSearchUserLocationType is the type tag for an approximate user location.
// Rust: lowercase (only "approximate").
type WebSearchUserLocationType string

const (
	WebSearchUserLocationTypeApproximate WebSearchUserLocationType = "approximate"
)

// ServiceTier selects the request service tier. Rust: lowercase.
type ServiceTier string

const (
	ServiceTierFast ServiceTier = "fast"
	ServiceTierFlex ServiceTier = "flex"
)

// ServiceTierDefaultRequestValue is the sentinel meaning explicit standard
// routing (no catalog service tier).
const ServiceTierDefaultRequestValue = "default"

// ForcedLoginMethod selects a forced login method. Rust: lowercase.
type ForcedLoginMethod string

const (
	ForcedLoginMethodChatgpt ForcedLoginMethod = "chatgpt"
	ForcedLoginMethodApi     ForcedLoginMethod = "api"
)

// TrustLevel represents the trust level for a project directory. Rust:
// lowercase.
type TrustLevel string

const (
	TrustLevelTrusted   TrustLevel = "trusted"
	TrustLevelUntrusted TrustLevel = "untrusted"
)

// AltScreenMode controls whether the TUI uses the alternate screen buffer.
// Rust: lowercase.
type AltScreenMode string

const (
	AltScreenModeAuto   AltScreenMode = "auto"
	AltScreenModeAlways AltScreenMode = "always"
	AltScreenModeNever  AltScreenMode = "never"
)

// ModeKind is the collaboration mode kind. Rust: snake_case. Several aliases
// ("code", "pair_programming", "execute", "custom") deserialize to Default.
// The hidden PairProgramming / Execute variants are skip-serialized in Rust and
// are intentionally omitted here.
type ModeKind string

const (
	ModeKindPlan    ModeKind = "plan"
	ModeKindDefault ModeKind = "default"
)

// NormalizeModeKind maps accepted aliases to the canonical value, mirroring the
// Rust serde aliases on the Default variant.
func NormalizeModeKind(v string) ModeKind {
	switch v {
	case "plan":
		return ModeKindPlan
	case "default", "code", "pair_programming", "execute", "custom":
		return ModeKindDefault
	default:
		return ModeKind(v)
	}
}

// WebSearchLocation is an optional, sparse location used to bias web search.
// All fields use omitempty because the Rust struct uses Option fields with no
// skip annotation; serde emits `null` for None, but in JSON-schema-friendly
// usage these are nullable strings. We use *string + omitempty so absent fields
// (rather than null) round-trip cleanly, matching how clients build payloads.
type WebSearchLocation struct {
	Country  *string `json:"country"`
	Region   *string `json:"region"`
	City     *string `json:"city"`
	Timezone *string `json:"timezone"`
}

// WebSearchFilters constrains web search domains.
type WebSearchFilters struct {
	AllowedDomains *[]string `json:"allowed_domains"`
}

// WebSearchUserLocation describes an approximate user location for web search.
type WebSearchUserLocation struct {
	Type     WebSearchUserLocationType `json:"type"`
	Country  *string                   `json:"country"`
	Region   *string                   `json:"region"`
	City     *string                   `json:"city"`
	Timezone *string                   `json:"timezone"`
}

// WebSearchConfig is the resolved web search configuration sent to the API.
type WebSearchConfig struct {
	Filters           *WebSearchFilters      `json:"filters"`
	UserLocation      *WebSearchUserLocation `json:"user_location"`
	SearchContextSize *WebSearchContextSize  `json:"search_context_size"`
}

// WebSearchToolConfig is the user-facing web search tool configuration.
type WebSearchToolConfig struct {
	ContextSize    *WebSearchContextSize `json:"context_size"`
	AllowedDomains *[]string             `json:"allowed_domains"`
	Location       *WebSearchLocation    `json:"location"`
}

// Settings holds per-collaboration-mode settings.
type Settings struct {
	Model                 string           `json:"model"`
	ReasoningEffort       *ReasoningEffort `json:"reasoning_effort"`
	DeveloperInstructions *string          `json:"developer_instructions"`
}

// CollaborationMode is the collaboration mode for a Codex session.
type CollaborationMode struct {
	Mode     ModeKind `json:"mode"`
	Settings Settings `json:"settings"`
}

// CollaborationModeMask is a partial-update mask for collaboration mode
// settings. The outer pointer marks "field present in mask"; the inner pointer
// (for the doubly-optional Rust fields) marks "set vs. clear". To preserve that
// three-state semantics in JSON, ReasoningEffort and DeveloperInstructions are
// modeled as **double pointers**.
type CollaborationModeMask struct {
	Name                  string            `json:"name"`
	Mode                  *ModeKind         `json:"mode"`
	Model                 *string           `json:"model"`
	ReasoningEffort       **ReasoningEffort `json:"reasoning_effort"`
	DeveloperInstructions **string          `json:"developer_instructions"`
}
