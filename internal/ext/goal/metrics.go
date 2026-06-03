package goal

// Goal telemetry metric names. Mirror the Rust `codex_otel` GOAL_* metric name
// constants byte-for-byte.
const (
	goalCreatedMetric         = "codex.goal.created"
	goalResumedMetric         = "codex.goal.resumed"
	goalCompletedMetric       = "codex.goal.completed"
	goalBudgetLimitedMetric   = "codex.goal.budget_limited"
	goalUsageLimitedMetric    = "codex.goal.usage_limited"
	goalBlockedMetric         = "codex.goal.blocked"
	goalTokenCountMetric      = "codex.goal.token_count"
	goalDurationSecondsMetric = "codex.goal.duration_s"
)

// GoalMetrics records goal lifecycle telemetry. Mirrors the Rust `GoalMetrics`;
// a nil client makes every method a no-op.
type GoalMetrics struct {
	client MetricsClient
}

// NewGoalMetrics builds a metrics recorder over an optional client. Mirrors Rust
// `GoalMetrics::new`.
func NewGoalMetrics(client MetricsClient) GoalMetrics {
	return GoalMetrics{client: client}
}

// recordCreated records a goal-created counter. Mirrors Rust
// `GoalMetrics::record_created`.
func (m GoalMetrics) recordCreated() {
	if m.client == nil {
		return
	}
	m.client.Counter(goalCreatedMetric, 1, nil)
}

// recordResumed records a goal-resumed counter. Mirrors Rust
// `GoalMetrics::record_resumed`.
func (m GoalMetrics) recordResumed() {
	if m.client == nil {
		return
	}
	m.client.Counter(goalResumedMetric, 1, nil)
}

// recordResumedIfStatusChanged records a resume when an active goal transitions
// from a paused/blocked/usage-limited status. Mirrors Rust
// `GoalMetrics::record_resumed_if_status_changed`.
func (m GoalMetrics) recordResumedIfStatusChanged(previousStatus *StateThreadGoalStatus, goalStatus StateThreadGoalStatus) {
	if goalStatus != StateGoalStatusActive {
		return
	}
	if previousStatus == nil {
		return
	}
	switch *previousStatus {
	case StateGoalStatusPaused, StateGoalStatusBlocked, StateGoalStatusUsageLimited:
		m.recordResumed()
	}
}

// recordTerminalIfStatusChanged records terminal counters and usage histograms
// when a goal reaches a new terminal/stopped status. Mirrors Rust
// `GoalMetrics::record_terminal_if_status_changed`.
func (m GoalMetrics) recordTerminalIfStatusChanged(previousStatus *StateThreadGoalStatus, goal *StateThreadGoal) {
	if previousStatus != nil && *previousStatus == goal.Status {
		return
	}

	var counter string
	switch goal.Status {
	case StateGoalStatusBlocked:
		counter = goalBlockedMetric
	case StateGoalStatusUsageLimited:
		counter = goalUsageLimitedMetric
	case StateGoalStatusBudgetLimited:
		counter = goalBudgetLimitedMetric
	case StateGoalStatusComplete:
		counter = goalCompletedMetric
	case StateGoalStatusActive, StateGoalStatusPaused:
		return
	default:
		return
	}

	if m.client == nil {
		return
	}
	statusTag := [][2]string{{"status", goal.Status.AsStr()}}
	m.client.Counter(counter, 1, nil)
	m.client.Histogram(goalTokenCountMetric, goal.TokensUsed, statusTag)
	m.client.Histogram(goalDurationSecondsMetric, goal.TimeUsedSeconds, statusTag)
}
