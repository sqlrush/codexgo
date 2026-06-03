package appserverproto

// DefaultEnabled returns the default for `#[serde(default = "default_enabled")]`
// fields in the v2 API (always true). Mirrors v2::shared::default_enabled.
func DefaultEnabled() bool { return true }

// NonSteerableTurnKind names a turn that cannot accept same-turn steering.
//
// Rust: v2::shared::NonSteerableTurnKind, serde rename_all = "camelCase".
type NonSteerableTurnKind string

const (
	// NonSteerableTurnKindReview is a `/review` turn.
	NonSteerableTurnKindReview NonSteerableTurnKind = "review"
	// NonSteerableTurnKindCompact is a manual `/compact` turn.
	NonSteerableTurnKindCompact NonSteerableTurnKind = "compact"
)

// SandboxModeV2 is the v2 API sandbox mode. Distinct from the internal protocol
// SandboxMode: this one is the camelCase/kebab-case API-v2 mirror.
//
// Rust: v2::shared::SandboxMode, serde rename_all = "kebab-case".
type SandboxModeV2 string

const (
	// SandboxModeV2ReadOnly grants read-only filesystem access.
	SandboxModeV2ReadOnly SandboxModeV2 = "read-only"
	// SandboxModeV2WorkspaceWrite grants writes within the workspace.
	SandboxModeV2WorkspaceWrite SandboxModeV2 = "workspace-write"
	// SandboxModeV2DangerFullAccess disables sandboxing.
	SandboxModeV2DangerFullAccess SandboxModeV2 = "danger-full-access"
)

// ApprovalsReviewerV2 configures who approval requests are routed to.
//
// Rust: v2::shared::ApprovalsReviewer. Custom serde: it serializes "user" and
// "guardian_subagent"; on deserialize it also accepts the legacy alias
// "auto_review" for the AutoReview variant.
type ApprovalsReviewerV2 string

const (
	// ApprovalsReviewerV2User routes approvals to the user (default).
	ApprovalsReviewerV2User ApprovalsReviewerV2 = "user"
	// ApprovalsReviewerV2AutoReview routes approvals to a guardian subagent.
	// Serializes as "guardian_subagent"; accepts "auto_review" on input.
	ApprovalsReviewerV2AutoReview ApprovalsReviewerV2 = "guardian_subagent"
)

// NormalizeApprovalsReviewerV2 maps the accepted alias to the canonical value,
// mirroring the Rust `alias = "auto_review"` on the AutoReview variant.
func NormalizeApprovalsReviewerV2(v string) ApprovalsReviewerV2 {
	switch v {
	case "auto_review", "guardian_subagent":
		return ApprovalsReviewerV2AutoReview
	default:
		return ApprovalsReviewerV2(v)
	}
}
