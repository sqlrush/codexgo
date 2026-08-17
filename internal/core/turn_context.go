package core

import (
	"encoding/json"

	"github.com/sqlrush/codexgo/internal/features"
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/rollout"
	"github.com/sqlrush/codexgo/internal/tools"
)

// TurnContext is the per-turn, read-only snapshot of everything a single turn
// needs to run: the submission id that started it, the resolved model identity
// and tools, the permission profile, environments, approval policy, and
// collaboration mode. It is a faithful, reduced port of the Rust
// `session::turn_context::TurnContext`.
//
// A TurnContext is constructed once per turn from the current
// [SessionConfiguration] and then never mutated; the turn runner and tool
// router read it concurrently. Treat all fields as immutable after
// construction (immutability contract).
//
// STUB: network proxy handles, shell-environment policy, ghost-snapshot config,
// truncation policy, telemetry spans, and the rich model-provider object are
// owned by other area agents and are carried here only as opaque values or
// omitted. The deprecated single `cwd` and the resolved-environment list are
// collapsed into Cwd + Environments for the turn-running subset.
type TurnContext struct {
	// SubID is the submission id that initiated this turn; it is the turn id
	// used to correlate events.
	SubID string
	// TraceID correlates the turn with telemetry traces, if enabled.
	TraceID *string
	// RealtimeActive reports whether a realtime conversation is running.
	RealtimeActive bool

	// ModelSlug is the canonical slug of the model for this turn.
	ModelSlug string
	// ModelInfo is opaque model metadata (from [ModelsManager]); core does not
	// interpret it but threads it through for the model client.
	ModelInfo any
	// Features is the resolved feature set for this turn (mirrors the Rust
	// `turn_context.features`). A nil value means "defaults"; resolve through
	// [turnFeatures] rather than reading the field directly.
	Features *features.Features

	// WebSearchMode is the effective web-search mode (config.web_search_mode).
	// Empty means the codex default (cached); resolve through
	// [turnWebSearchMode].
	WebSearchMode protocol.WebSearchMode
	// ProviderCapabilities are the active provider's feature upper bounds
	// (namespace tools, hosted image generation, hosted web search). The zero
	// value resolves to the codex all-true default; resolve through
	// [turnProviderCapabilities]. Mirrors the Rust
	// `turn_context.provider.capabilities()`.
	ProviderCapabilities ProviderCapabilities
	// StreamMaxRetries is the provider's reconnect budget for a dropped model
	// stream (model-provider-info stream_max_retries); nil = default 5, capped
	// at 100. Consumed by the sampling retry loop.
	StreamMaxRetries *uint64
	// ExperimentalRequestUserInput gates the request_user_input tool
	// (config.experimental_request_user_input_enabled). Nil means the codex
	// default (enabled); resolve through [turnRequestUserInputEnabled].
	ExperimentalRequestUserInput *bool
	// ModelContextWindow is the effective context window in tokens, if known.
	ModelContextWindow *int64
	// AutoCompactTokenLimit is the resolved auto-compaction token budget for the
	// turn's model, if known. When the running total reaches it, runTurn fires a
	// pre-sampling inline compaction (port of run_pre_sampling_compact's
	// auto_compact_token_status check) and mid-turn rollover (0.147
	// context_window_token_status). Nil/<=0 disables auto-compaction.
	AutoCompactTokenLimit *int64
	// AutoCompactTokenLimitScope selects whether the whole active context or
	// only the tokens after the window prefill count against the limit
	// (config.model_auto_compact_token_limit_scope); empty = Total.
	AutoCompactTokenLimitScope protocol.AutoCompactTokenLimitScope
	// TokenBudget, when set, enables the 0.147 token-budget mode: a reminder is
	// recorded when the remaining window drops to the threshold, the fallback
	// prompt when it hits zero, and compaction installs a fresh context window
	// instead of summarizing. Nil = classic summarizing compaction.
	TokenBudget *TokenBudgetConfig

	// Tools is the resolved tool specification visible to the model this turn.
	Tools []tools.ToolSpec

	// ReasoningEffort / ReasoningSummary configure model reasoning.
	ReasoningEffort  *protocol.ReasoningEffort
	ReasoningSummary protocol.ReasoningSummary

	// Cwd is the absolute working directory for this turn.
	Cwd string
	// CurrentDate / Timezone stamp the turn for context construction.
	CurrentDate *string
	Timezone    *string

	// DeveloperInstructions / UserInstructions / CompactPrompt mirror the
	// session configuration but are captured per-turn so a turn-local override
	// can diverge.
	DeveloperInstructions *string
	UserInstructions      *string
	CompactPrompt         *string
	BaseInstructions      string

	// CollaborationMode is the effective collaboration mode for this turn.
	CollaborationMode protocol.CollaborationMode
	// Personality is the model personality preference.
	Personality *protocol.Personality

	// ApprovalPolicy controls when execution escalates for approval.
	ApprovalPolicy protocol.AskForApproval
	// PermissionProfile is the effective permission profile (opaque to core;
	// owned by the permissions area agent).
	PermissionProfile any
	// SandboxMode is the effective filesystem sandbox mode for the turn (the
	// reduced projection of PermissionProfile core needs to resolve the real
	// per-turn sandbox policy for exec). Defaults to read-only for the zero
	// value, mirroring codex's config default.
	SandboxMode protocol.SandboxMode
	// NetworkAccessEnabled reports whether outbound network access is granted to
	// sandboxed commands this turn (the network half of the resolved policy).
	NetworkAccessEnabled bool
	// WindowsSandboxLevel is the effective Windows sandbox level.
	WindowsSandboxLevel protocol.WindowsSandboxLevel

	// Environments are the resolved turn environments.
	Environments []protocol.TurnEnvironmentSelection

	// DynamicTools are the thread-defined dynamic tool specs for this turn.
	DynamicTools []protocol.DynamicToolSpec
	// Skills is the opaque skills-load outcome for this turn (from
	// [SkillsManager]).
	Skills any
	// FinalOutputJSONSchema constrains the model's final output, if set.
	FinalOutputJSONSchema []byte

	// AppServerClientName identifies the attached app-server client, if any.
	AppServerClientName *string

	// SessionSource is the opaque session-origin classification
	// (rollout.SessionSource when populated). It drives source-scoped tool
	// gating (e.g. goal tools are hidden for the review sub-agent).
	SessionSource any
}

// CompactPromptText returns the effective compaction prompt for the turn,
// falling back to the default summarization prompt when none is configured.
// Mirrors the Rust `TurnContext::compact_prompt`.
func (tc *TurnContext) CompactPromptText() string {
	if tc.CompactPrompt != nil && *tc.CompactPrompt != "" {
		return *tc.CompactPrompt
	}
	return defaultSummarizationPrompt
}

// defaultSummarizationPrompt is the inline-compaction fallback prompt. The full
// prompt lives in the compaction area; this placeholder keeps the foundation
// self-contained.
//
// STUB: the real summarization prompt (compact::SUMMARIZATION_PROMPT) is owned
// by the compaction area agent.
const defaultSummarizationPrompt = "Summarize the conversation so far, preserving all facts, decisions, and open tasks needed to continue."

// ToTurnContextItem produces the rollout turn-context record describing this
// turn's effective settings, mirroring the Rust
// `TurnContext::to_turn_context_item` (reduced surface). The result is a
// raw-backed [rollout.TurnContextItem] carrying the model-visible fields the
// turn path is responsible for.
//
// STUB: sandbox-policy projection, network item, file-system sandbox policy, and
// permission-profile serialization are owned by the permissions/sandbox area
// agents and are omitted from the raw payload here.
func (tc *TurnContext) ToTurnContextItem() (rollout.TurnContextItem, error) {
	turnID := tc.SubID
	payload := map[string]any{
		"turn_id":            turnID,
		"cwd":                tc.Cwd,
		"current_date":       tc.CurrentDate,
		"timezone":           tc.Timezone,
		"approval_policy":    tc.ApprovalPolicy,
		"model":              tc.ModelSlug,
		"personality":        tc.Personality,
		"collaboration_mode": tc.CollaborationMode,
		"realtime_active":    tc.RealtimeActive,
		"effort":             tc.ReasoningEffort,
		"summary":            tc.ReasoningSummary,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return rollout.TurnContextItem{}, err
	}
	var item rollout.TurnContextItem
	if err := json.Unmarshal(raw, &item); err != nil {
		return rollout.TurnContextItem{}, err
	}
	return item, nil
}
