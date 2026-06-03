package analytics

import (
	"time"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// GuardianReviewDecision is the high-level decision of a guardian review.
// Mirrors Rust `GuardianReviewDecision` (serde rename_all = "snake_case").
type GuardianReviewDecision string

const (
	GuardianReviewDecisionApproved GuardianReviewDecision = "approved"
	GuardianReviewDecisionDenied   GuardianReviewDecision = "denied"
	GuardianReviewDecisionAborted  GuardianReviewDecision = "aborted"
)

// GuardianReviewTerminalStatus is the terminal status of a guardian review.
// Mirrors Rust `GuardianReviewTerminalStatus`.
type GuardianReviewTerminalStatus string

const (
	GuardianReviewTerminalStatusApproved     GuardianReviewTerminalStatus = "approved"
	GuardianReviewTerminalStatusDenied       GuardianReviewTerminalStatus = "denied"
	GuardianReviewTerminalStatusAborted      GuardianReviewTerminalStatus = "aborted"
	GuardianReviewTerminalStatusTimedOut     GuardianReviewTerminalStatus = "timed_out"
	GuardianReviewTerminalStatusFailedClosed GuardianReviewTerminalStatus = "failed_closed"
)

// GuardianReviewFailureReason enumerates failure causes. Mirrors Rust
// `GuardianReviewFailureReason`.
type GuardianReviewFailureReason string

const (
	GuardianReviewFailureReasonTimeout          GuardianReviewFailureReason = "timeout"
	GuardianReviewFailureReasonCancelled        GuardianReviewFailureReason = "cancelled"
	GuardianReviewFailureReasonPromptBuildError GuardianReviewFailureReason = "prompt_build_error"
	GuardianReviewFailureReasonSessionError     GuardianReviewFailureReason = "session_error"
	GuardianReviewFailureReasonParseError       GuardianReviewFailureReason = "parse_error"
)

// GuardianReviewSessionKind enumerates how the guardian session was created.
// Mirrors Rust `GuardianReviewSessionKind`.
type GuardianReviewSessionKind string

const (
	GuardianReviewSessionKindTrunkNew        GuardianReviewSessionKind = "trunk_new"
	GuardianReviewSessionKindTrunkReused     GuardianReviewSessionKind = "trunk_reused"
	GuardianReviewSessionKindEphemeralForked GuardianReviewSessionKind = "ephemeral_forked"
)

// GuardianApprovalRequestSource identifies who requested the approval. Mirrors
// Rust `GuardianApprovalRequestSource`.
type GuardianApprovalRequestSource string

const (
	GuardianApprovalRequestSourceMainTurn          GuardianApprovalRequestSource = "main_turn"
	GuardianApprovalRequestSourceDelegatedSubagent GuardianApprovalRequestSource = "delegated_subagent"
)

// GuardianReviewedAction is the internally-tagged enum describing the action a
// guardian reviewed. Mirrors Rust `GuardianReviewedAction` (serde tag = "type",
// rename_all = "snake_case"). Use Kind to select the variant; only the fields
// for that variant are serialized.
type GuardianReviewedAction struct {
	Kind GuardianReviewedActionKind

	// Shell / UnifiedExec
	SandboxPermissions    interface{} `json:"-"`
	AdditionalPermissions interface{} `json:"-"`
	TTY                   bool        `json:"-"`
	// Execve
	Source  interface{} `json:"-"`
	Program string      `json:"-"`
	// NetworkAccess
	Protocol interface{} `json:"-"`
	Port     uint16      `json:"-"`
	// McpToolCall
	Server        string  `json:"-"`
	ToolName      string  `json:"-"`
	ConnectorID   *string `json:"-"`
	ConnectorName *string `json:"-"`
	ToolTitle     *string `json:"-"`
}

// GuardianReviewedActionKind enumerates the variants of [GuardianReviewedAction].
type GuardianReviewedActionKind string

const (
	GuardianReviewedActionShell              GuardianReviewedActionKind = "shell"
	GuardianReviewedActionUnifiedExec        GuardianReviewedActionKind = "unified_exec"
	GuardianReviewedActionExecve             GuardianReviewedActionKind = "execve"
	GuardianReviewedActionApplyPatch         GuardianReviewedActionKind = "apply_patch"
	GuardianReviewedActionNetworkAccess      GuardianReviewedActionKind = "network_access"
	GuardianReviewedActionMcpToolCall        GuardianReviewedActionKind = "mcp_tool_call"
	GuardianReviewedActionRequestPermissions GuardianReviewedActionKind = "request_permissions"
)

// GuardianReviewEventParams is the flattened payload describing a guardian
// review outcome. Mirrors Rust `GuardianReviewEventParams`.
type GuardianReviewEventParams struct {
	ThreadID                string                        `json:"thread_id"`
	TurnID                  string                        `json:"turn_id"`
	ReviewID                string                        `json:"review_id"`
	TargetItemID            *string                       `json:"target_item_id"`
	ApprovalRequestSource   GuardianApprovalRequestSource `json:"approval_request_source"`
	ReviewedAction          GuardianReviewedAction        `json:"reviewed_action"`
	ReviewedActionTruncated bool                          `json:"reviewed_action_truncated"`
	Decision                GuardianReviewDecision        `json:"decision"`
	TerminalStatus          GuardianReviewTerminalStatus  `json:"terminal_status"`
	FailureReason           *GuardianReviewFailureReason  `json:"failure_reason"`
	RiskLevel               *string                       `json:"risk_level"`
	UserAuthorization       *string                       `json:"user_authorization"`
	Outcome                 *string                       `json:"outcome"`
	GuardianThreadID        *string                       `json:"guardian_thread_id"`
	GuardianSessionKind     *GuardianReviewSessionKind    `json:"guardian_session_kind"`
	GuardianModel           *string                       `json:"guardian_model"`
	GuardianReasoningEffort *string                       `json:"guardian_reasoning_effort"`
	HadPriorReviewContext   *bool                         `json:"had_prior_review_context"`
	ReviewTimeoutMs         uint64                        `json:"review_timeout_ms"`
	ToolCallCount           *uint64                       `json:"tool_call_count"`
	TimeToFirstTokenMs      *uint64                       `json:"time_to_first_token_ms"`
	CompletionLatencyMs     *uint64                       `json:"completion_latency_ms"`
	StartedAt               uint64                        `json:"started_at"`
	CompletedAt             *uint64                       `json:"completed_at"`
	InputTokens             *int64                        `json:"input_tokens"`
	CachedInputTokens       *int64                        `json:"cached_input_tokens"`
	OutputTokens            *int64                        `json:"output_tokens"`
	ReasoningOutputTokens   *int64                        `json:"reasoning_output_tokens"`
	TotalTokens             *int64                        `json:"total_tokens"`
}

// GuardianReviewAnalyticsResult holds the variable result fields produced once
// a guardian review finishes. Mirrors Rust `GuardianReviewAnalyticsResult`.
type GuardianReviewAnalyticsResult struct {
	Decision                GuardianReviewDecision
	TerminalStatus          GuardianReviewTerminalStatus
	FailureReason           *GuardianReviewFailureReason
	RiskLevel               *string
	UserAuthorization       *string
	Outcome                 *string
	GuardianThreadID        *string
	GuardianSessionKind     *GuardianReviewSessionKind
	GuardianModel           *string
	GuardianReasoningEffort *string
	HadPriorReviewContext   *bool
	ReviewedActionTruncated bool
	TokenUsage              *protocol.TokenUsage
	TimeToFirstTokenMs      *uint64
}

// GuardianReviewAnalyticsResultWithoutSession returns the default
// failed-closed/denied result used when no guardian session ran. Mirrors Rust
// `GuardianReviewAnalyticsResult::without_session`.
func GuardianReviewAnalyticsResultWithoutSession() GuardianReviewAnalyticsResult {
	return GuardianReviewAnalyticsResult{
		Decision:       GuardianReviewDecisionDenied,
		TerminalStatus: GuardianReviewTerminalStatusFailedClosed,
	}
}

// GuardianReviewAnalyticsResultFromSession constructs a result populated with
// session metadata. Mirrors Rust `GuardianReviewAnalyticsResult::from_session`.
func GuardianReviewAnalyticsResultFromSession(
	guardianThreadID string,
	guardianSessionKind GuardianReviewSessionKind,
	guardianModel string,
	guardianReasoningEffort *string,
	hadPriorReviewContext bool,
) GuardianReviewAnalyticsResult {
	result := GuardianReviewAnalyticsResultWithoutSession()
	result.GuardianThreadID = &guardianThreadID
	result.GuardianSessionKind = &guardianSessionKind
	result.GuardianModel = &guardianModel
	result.GuardianReasoningEffort = guardianReasoningEffort
	result.HadPriorReviewContext = &hadPriorReviewContext
	return result
}

// GuardianReviewTrackContext captures the fixed inputs of a guardian review and
// the start time so latency can be computed when the review completes. Mirrors
// Rust `GuardianReviewTrackContext`.
type GuardianReviewTrackContext struct {
	threadID              string
	turnID                string
	reviewID              string
	targetItemID          *string
	approvalRequestSource GuardianApprovalRequestSource
	reviewedAction        GuardianReviewedAction
	reviewTimeoutMs       uint64
	StartedAtMs           uint64
	startedInstant        time.Time
}

// NewGuardianReviewTrackContext starts tracking a guardian review. Mirrors Rust
// `GuardianReviewTrackContext::new`.
func NewGuardianReviewTrackContext(
	threadID string,
	turnID string,
	reviewID string,
	targetItemID *string,
	approvalRequestSource GuardianApprovalRequestSource,
	reviewedAction GuardianReviewedAction,
	reviewTimeoutMs uint64,
) *GuardianReviewTrackContext {
	return &GuardianReviewTrackContext{
		threadID:              threadID,
		turnID:                turnID,
		reviewID:              reviewID,
		targetItemID:          targetItemID,
		approvalRequestSource: approvalRequestSource,
		reviewedAction:        reviewedAction,
		reviewTimeoutMs:       reviewTimeoutMs,
		StartedAtMs:           NowUnixMillis(),
		startedInstant:        time.Now(),
	}
}

// EventParams combines the track context with the review result to produce the
// final event payload. Mirrors Rust `GuardianReviewTrackContext::event_params`.
func (c *GuardianReviewTrackContext) EventParams(
	result GuardianReviewAnalyticsResult,
	completedAtMs uint64,
) GuardianReviewEventParams {
	completionLatencyMs := uint64(time.Since(c.startedInstant).Milliseconds())
	startedAt := c.StartedAtMs / 1000
	completedAt := completedAtMs / 1000

	params := GuardianReviewEventParams{
		ThreadID:                c.threadID,
		TurnID:                  c.turnID,
		ReviewID:                c.reviewID,
		TargetItemID:            c.targetItemID,
		ApprovalRequestSource:   c.approvalRequestSource,
		ReviewedAction:          c.reviewedAction,
		ReviewedActionTruncated: result.ReviewedActionTruncated,
		Decision:                result.Decision,
		TerminalStatus:          result.TerminalStatus,
		FailureReason:           result.FailureReason,
		RiskLevel:               result.RiskLevel,
		UserAuthorization:       result.UserAuthorization,
		Outcome:                 result.Outcome,
		GuardianThreadID:        result.GuardianThreadID,
		GuardianSessionKind:     result.GuardianSessionKind,
		GuardianModel:           result.GuardianModel,
		GuardianReasoningEffort: result.GuardianReasoningEffort,
		HadPriorReviewContext:   result.HadPriorReviewContext,
		ReviewTimeoutMs:         c.reviewTimeoutMs,
		ToolCallCount:           nil,
		TimeToFirstTokenMs:      result.TimeToFirstTokenMs,
		CompletionLatencyMs:     &completionLatencyMs,
		StartedAt:               startedAt,
		CompletedAt:             &completedAt,
	}

	if usage := result.TokenUsage; usage != nil {
		input := usage.InputTokens
		cached := usage.CachedInputTokens
		output := usage.OutputTokens
		reasoning := usage.ReasoningOutputTokens
		total := usage.TotalTokens
		params.InputTokens = &input
		params.CachedInputTokens = &cached
		params.OutputTokens = &output
		params.ReasoningOutputTokens = &reasoning
		params.TotalTokens = &total
	}

	return params
}
