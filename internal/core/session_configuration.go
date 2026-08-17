package core

import (
	"github.com/sqlrush/codexgo/internal/features"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// SessionConfiguration is the snapshot of per-session settings that the turn
// path reads when constructing each turn context. It is a faithful, reduced port
// of the Rust `session::session::SessionConfiguration`: provider/model
// identity, instructions, approval policy, permission profile, cwd, and the
// sticky turn environments.
//
// Immutability contract: SessionConfiguration is treated as a value type.
// Settings updates produce a NEW configuration via [SessionConfiguration.Apply]
// rather than mutating in place, mirroring the Rust `apply` which clones first.
//
// STUB: the full permission-profile state machine, network-proxy spec,
// windows-sandbox level derivation, and config-layer-stack are owned by the
// config/permissions area agents. Core carries the effective values it needs and
// leaves richer derivations to those packages.
type SessionConfiguration struct {
	// ProviderID identifies the model provider ("openai", "openrouter", ...).
	ProviderID string

	// ProviderCapabilities are the active provider's feature upper bounds
	// (namespace tools, hosted image generation, hosted web search), resolved
	// from modelproviderinfo.CapabilitiesForProvider at assembly time. The zero
	// value (all false) is treated as "not configured" and resolves to the
	// codex all-true default; see [turnProviderCapabilities]. This mirrors the
	// Rust `TurnContext::provider.capabilities()` upper bound threaded into the
	// turn (model-provider/src/provider.rs ProviderCapabilities).
	ProviderCapabilities ProviderCapabilities

	// StreamMaxRetries is the provider's reconnect budget for a dropped model
	// stream (model-provider-info stream_max_retries); nil = default 5.
	StreamMaxRetries *uint64

	// AutoCompactTokenLimitScope mirrors config.model_auto_compact_token_limit_scope
	// (total or body_after_prefix); empty = total.
	AutoCompactTokenLimitScope protocol.AutoCompactTokenLimitScope
	// TokenBudget mirrors config.token_budget (0.147 features.token_budget);
	// nil = disabled.
	TokenBudget *TokenBudgetConfig

	// CollaborationMode bundles the model + reasoning effort + developer
	// instructions for the turn.
	CollaborationMode protocol.CollaborationMode
	// ModelReasoningSummary configures reasoning-summary verbosity.
	ModelReasoningSummary *protocol.ReasoningSummary
	// ServiceTier is the optional service-tier request value.
	ServiceTier *string

	// DeveloperInstructions supplement the base instructions.
	DeveloperInstructions *string
	// UserInstructions are appended to the base instructions.
	UserInstructions *string
	// Personality is the model personality preference.
	Personality *protocol.Personality
	// BaseInstructions is the resolved base instruction text for the session.
	BaseInstructions string
	// CompactPrompt overrides the default compaction prompt.
	CompactPrompt *string

	// ApprovalPolicy controls when execution escalates for approval.
	ApprovalPolicy protocol.AskForApproval
	// ApprovalsReviewer selects the reviewer for approvals.
	ApprovalsReviewer protocol.ApprovalsReviewer
	// WindowsSandboxLevel is the effective Windows sandbox level.
	WindowsSandboxLevel protocol.WindowsSandboxLevel

	// IncludeEnvironmentContext gates the codex `<permissions instructions>` +
	// `<environment_context>` initial-context messages that are seeded into a new
	// thread's history at session start (mirrors config.include_environment_context).
	// It defaults to false so unit tests that build a SessionConfiguration directly
	// are unaffected; the real binary's assembly sets it true.
	IncludeEnvironmentContext bool
	// SandboxMode is the effective filesystem sandbox mode used to render the
	// permissions + environment-context messages (defaults to read-only).
	SandboxMode protocol.SandboxMode
	// NetworkAccessEnabled toggles the rendered network-access word.
	NetworkAccessEnabled bool
	// Shell is the user's default shell name reported in the environment context
	// (empty falls back to the host default shell at render time).
	Shell string

	// Cwd is the absolute working-directory root for the session.
	Cwd string
	// WorkspaceRoots are the thread-scoped workspace roots.
	WorkspaceRoots []string
	// CodexHome is the directory holding all Codex state for the session.
	CodexHome string
	// ThreadName is the optional user-facing thread name.
	ThreadName *string

	// Environments are the sticky turn environments inherited when a turn does
	// not provide an override.
	Environments []protocol.TurnEnvironmentSelection

	// SessionSource is the opaque source classification (cli, vscode, ...). Core
	// does not interpret it; it is carried for telemetry and rollout.
	SessionSource any
	// ForkedFromThreadID is the thread this session was forked from, if any.
	ForkedFromThreadID *protocol.ThreadID
	// ThreadSource is the opaque analytics source classification.
	ThreadSource any
	// DynamicTools are the thread-defined dynamic tool specs.
	DynamicTools []protocol.DynamicToolSpec
	// PersistExtendedHistory selects extended vs. limited event persistence.
	PersistExtendedHistory bool

	// Features is the resolved feature set for the session (config [features]
	// table over the per-feature defaults). Nil means "defaults"; turn
	// construction resolves through [turnFeatures].
	Features *features.Features
	// WebSearchMode is the effective web-search mode (config.web_search_mode).
	// Empty means the codex default (cached).
	WebSearchMode protocol.WebSearchMode
	// ExperimentalRequestUserInput gates the request_user_input tool
	// (config.experimental_request_user_input_enabled). Nil means the codex
	// default (enabled).
	ExperimentalRequestUserInput *bool

	// AppServerClientName / AppServerClientVersion identify the app-server
	// client, when one is attached.
	AppServerClientName    *string
	AppServerClientVersion *string
	// MetricsServiceName is an optional service-name tag for session metrics.
	MetricsServiceName *string
}

// SessionSettingsUpdate carries an optional override for each mutable session
// setting. Nil fields leave the corresponding setting unchanged. It is a reduced
// port of the Rust `SessionSettingsUpdate`.
type SessionSettingsUpdate struct {
	Cwd                    *string
	WorkspaceRoots         *[]string
	ApprovalPolicy         *protocol.AskForApproval
	ApprovalsReviewer      *protocol.ApprovalsReviewer
	WindowsSandboxLevel    *protocol.WindowsSandboxLevel
	CollaborationMode      *protocol.CollaborationMode
	ReasoningSummary       *protocol.ReasoningSummary
	Personality            *protocol.Personality
	Environments           *[]protocol.TurnEnvironmentSelection
	AppServerClientName    *string
	AppServerClientVersion *string
	// FinalOutputJSONSchema is a turn-local override (double-option: outer nil =
	// unchanged, inner nil = clear). Core only forwards it to the turn context.
	FinalOutputJSONSchema *([]byte)
}

// Apply returns a NEW configuration with the update overlaid. The receiver is
// never mutated, matching the Rust `SessionConfiguration::apply` which clones
// before applying.
func (c SessionConfiguration) Apply(u SessionSettingsUpdate) SessionConfiguration {
	next := c.clone()
	if u.CollaborationMode != nil {
		next.CollaborationMode = *u.CollaborationMode
	}
	if u.ReasoningSummary != nil {
		v := *u.ReasoningSummary
		next.ModelReasoningSummary = &v
	}
	if u.Personality != nil {
		v := *u.Personality
		next.Personality = &v
	}
	if u.ApprovalPolicy != nil {
		next.ApprovalPolicy = *u.ApprovalPolicy
	}
	if u.ApprovalsReviewer != nil {
		next.ApprovalsReviewer = *u.ApprovalsReviewer
	}
	if u.WindowsSandboxLevel != nil {
		next.WindowsSandboxLevel = *u.WindowsSandboxLevel
	}
	if u.Cwd != nil {
		next.Cwd = *u.Cwd
	}
	if u.WorkspaceRoots != nil {
		next.WorkspaceRoots = append([]string(nil), (*u.WorkspaceRoots)...)
	}
	if u.Environments != nil {
		next.Environments = append([]protocol.TurnEnvironmentSelection(nil), (*u.Environments)...)
	}
	if u.AppServerClientName != nil {
		v := *u.AppServerClientName
		next.AppServerClientName = &v
	}
	if u.AppServerClientVersion != nil {
		v := *u.AppServerClientVersion
		next.AppServerClientVersion = &v
	}
	return next
}

// Model returns the configured model slug from the collaboration mode.
func (c SessionConfiguration) Model() string { return c.CollaborationMode.Settings.Model }

// clone returns a deep copy of the configuration's owned slices/pointers.
func (c SessionConfiguration) clone() SessionConfiguration {
	next := c
	next.WorkspaceRoots = append([]string(nil), c.WorkspaceRoots...)
	next.Environments = append([]protocol.TurnEnvironmentSelection(nil), c.Environments...)
	next.DynamicTools = append([]protocol.DynamicToolSpec(nil), c.DynamicTools...)
	return next
}
