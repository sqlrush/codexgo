package otel

// Metric names. These MUST match codex exactly for drop-in compatibility.
// Mirrors codex-rs/otel/src/metrics/names.rs.
const (
	ToolCallCountMetric         = "codex.tool.call"
	ToolCallDurationMetric      = "codex.tool.call.duration_ms"
	ToolCallUnifiedExecMetric   = "codex.tool.unified_exec"
	ProcessStartMetric          = "codex.process.start"
	APICallCountMetric          = "codex.api_request"
	APICallDurationMetric       = "codex.api_request.duration_ms"
	SSEEventCountMetric         = "codex.sse_event"
	SSEEventDurationMetric      = "codex.sse_event.duration_ms"
	WebsocketRequestCountMetric = "codex.websocket.request"

	WebsocketRequestDurationMetric = "codex.websocket.request.duration_ms"
	WebsocketEventCountMetric      = "codex.websocket.event"
	WebsocketEventDurationMetric   = "codex.websocket.event.duration_ms"

	ResponsesAPIOverheadDurationMetric          = "codex.responses_api_overhead.duration_ms"
	ResponsesAPIInferenceTimeDurationMetric     = "codex.responses_api_inference_time.duration_ms"
	ResponsesAPIEngineIAPITtftDurationMetric    = "codex.responses_api_engine_iapi_ttft.duration_ms"
	ResponsesAPIEngineServiceTtftDurationMetric = "codex.responses_api_engine_service_ttft.duration_ms"
	ResponsesAPIEngineIAPITbtDurationMetric     = "codex.responses_api_engine_iapi_tbt.duration_ms"
	ResponsesAPIEngineServiceTbtDurationMetric  = "codex.responses_api_engine_service_tbt.duration_ms"

	TurnE2EDurationMetric  = "codex.turn.e2e_duration_ms"
	TurnTtftDurationMetric = "codex.turn.ttft.duration_ms"
	TurnTtfmDurationMetric = "codex.turn.ttfm.duration_ms"
	TurnNetworkProxyMetric = "codex.turn.network_proxy"
	TurnMemoryMetric       = "codex.turn.memory"
	TurnToolCallMetric     = "codex.turn.tool.call"
	TurnTokenUsageMetric   = "codex.turn.token_usage"

	GuardianReviewCountMetric        = "codex.guardian.review"
	GuardianReviewDurationMetric     = "codex.guardian.review.duration_ms"
	GuardianReviewTtftDurationMetric = "codex.guardian.review.ttft.duration_ms"
	GuardianReviewTokenUsageMetric   = "codex.guardian.review.token_usage"

	GoalCreatedMetric         = "codex.goal.created"
	GoalResumedMetric         = "codex.goal.resumed"
	GoalCompletedMetric       = "codex.goal.completed"
	GoalBudgetLimitedMetric   = "codex.goal.budget_limited"
	GoalUsageLimitedMetric    = "codex.goal.usage_limited"
	GoalBlockedMetric         = "codex.goal.blocked"
	GoalTokenCountMetric      = "codex.goal.token_count"
	GoalDurationSecondsMetric = "codex.goal.duration_s"

	PluginInstallElicitationSentMetric   = "codex.plugins.install_elicitation.sent"
	PluginInstallSuggestionMetric        = "codex.plugins.install_suggestion"
	CuratedPluginsStartupSyncMetric      = "codex.plugins.startup_sync"
	CuratedPluginsStartupSyncFinalMetric = "codex.plugins.startup_sync.final"

	HookRunMetric         = "codex.hooks.run"
	HookRunDurationMetric = "codex.hooks.run.duration_ms"

	StartupPhaseDurationMetric         = "codex.startup.phase.duration_ms"
	StartupPrewarmDurationMetric       = "codex.startup_prewarm.duration_ms"
	StartupPrewarmAgeAtFirstTurnMetric = "codex.startup_prewarm.age_at_first_turn_ms"

	ThreadStartedMetric                         = "codex.thread.started"
	ThreadSkillsEnabledTotalMetric              = "codex.thread.skills.enabled_total"
	ThreadSkillsKeptTotalMetric                 = "codex.thread.skills.kept_total"
	ThreadSkillsDescriptionTruncatedCharsMetric = "codex.thread.skills.description_truncated_chars"
	ThreadSkillsTruncatedMetric                 = "codex.thread.skills.truncated"
)
