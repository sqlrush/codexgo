package state

// SQLite-backed thread-goal store, faithfully porting the Rust
// `state/src/runtime/goals.rs` GoalStore: get/insert/replace/update/delete plus
// usage accounting with budget-limit promotion. All rows live in the dedicated
// goals database (goals.sqlite) keyed by thread id.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ThreadGoalStatus is the persisted status of a thread goal. Mirrors the Rust
// `crate::ThreadGoalStatus` (snake_case persisted strings).
type ThreadGoalStatus int

// ThreadGoalStatus variants, in the Rust declaration order.
const (
	GoalStatusActive ThreadGoalStatus = iota
	GoalStatusPaused
	GoalStatusBlocked
	GoalStatusUsageLimited
	GoalStatusBudgetLimited
	GoalStatusComplete
)

// AsStr returns the canonical persisted string for the status. Mirrors Rust
// `ThreadGoalStatus::as_str`.
func (s ThreadGoalStatus) AsStr() string {
	switch s {
	case GoalStatusActive:
		return "active"
	case GoalStatusPaused:
		return "paused"
	case GoalStatusBlocked:
		return "blocked"
	case GoalStatusUsageLimited:
		return "usage_limited"
	case GoalStatusBudgetLimited:
		return "budget_limited"
	case GoalStatusComplete:
		return "complete"
	default:
		return ""
	}
}

// ParseThreadGoalStatus parses a persisted status string. Mirrors the Rust
// `ThreadGoalStatus::from_str`.
func ParseThreadGoalStatus(value string) (ThreadGoalStatus, error) {
	switch value {
	case "active":
		return GoalStatusActive, nil
	case "paused":
		return GoalStatusPaused, nil
	case "blocked":
		return GoalStatusBlocked, nil
	case "usage_limited":
		return GoalStatusUsageLimited, nil
	case "budget_limited":
		return GoalStatusBudgetLimited, nil
	case "complete":
		return GoalStatusComplete, nil
	default:
		return 0, fmt.Errorf("unknown thread goal status %q", value)
	}
}

// ThreadGoal is the persisted thread goal record. Mirrors the Rust
// `crate::ThreadGoal` field-for-field.
type ThreadGoal struct {
	ThreadID        string
	GoalID          string
	Objective       string
	Status          ThreadGoalStatus
	TokenBudget     *int64
	TokensUsed      int64
	TimeUsedSeconds int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// GoalUpdate is the partial-update payload for an existing goal. Mirrors the
// Rust `GoalUpdate`. The doubly-optional TokenBudget mirrors
// `Option<Option<i64>>`: the outer pointer marks "field present in the update";
// the inner pointer marks "set vs. clear". ExpectedGoalID, when present, scopes
// the update to a specific goal id.
type GoalUpdate struct {
	Objective      *string
	Status         *ThreadGoalStatus
	TokenBudget    **int64
	ExpectedGoalID *string
}

// GoalAccountingMode selects which goal statuses are eligible for usage
// accounting. Mirrors the Rust `GoalAccountingMode`.
type GoalAccountingMode int

// GoalAccountingMode variants, in the Rust declaration order.
const (
	GoalAccountingModeActiveStatusOnly GoalAccountingMode = iota
	GoalAccountingModeActiveOnly
	GoalAccountingModeActiveOrComplete
	GoalAccountingModeActiveOrStopped
)

// GoalAccountingOutcomeKind discriminates a [GoalAccountingOutcome].
type GoalAccountingOutcomeKind int

// GoalAccountingOutcomeKind variants.
const (
	// GoalAccountingOutcomeUnchanged indicates no row was updated; Goal holds the
	// current goal when one exists.
	GoalAccountingOutcomeUnchanged GoalAccountingOutcomeKind = iota
	// GoalAccountingOutcomeUpdated indicates the goal was updated; Goal holds the
	// updated record.
	GoalAccountingOutcomeUpdated
)

// GoalAccountingOutcome is the result of accounting goal usage. Mirrors the
// Rust `GoalAccountingOutcome` enum (struct + discriminator).
type GoalAccountingOutcome struct {
	Kind GoalAccountingOutcomeKind
	Goal *ThreadGoal
}

// GoalStore reads and mutates persisted thread goals. Mirrors the Rust
// `GoalStore` over the goals pool.
type GoalStore struct {
	pool *sql.DB
}

// ThreadGoals returns the goal store backed by the goals database. Mirrors the
// Rust `StateRuntime::thread_goals`.
func (r *StateRuntime) ThreadGoals() *GoalStore {
	return &GoalStore{pool: r.goalsPool}
}

// threadGoalColumns is the canonical SELECT/RETURNING column list shared by
// every goal query, in the Rust query order.
const threadGoalColumns = `
    thread_id,
    goal_id,
    objective,
    status,
    token_budget,
    tokens_used,
    time_used_seconds,
    created_at_ms,
    updated_at_ms`

// GetThreadGoal returns the current goal for the thread, or nil. Mirrors the
// Rust `get_thread_goal`.
func (g *GoalStore) GetThreadGoal(ctx context.Context, threadID string) (*ThreadGoal, error) {
	row := g.pool.QueryRowContext(ctx, `
SELECT`+threadGoalColumns+`
FROM thread_goals
WHERE thread_id = ?
`, threadID)
	goal, err := threadGoalFromRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get thread goal %s: %w", threadID, err)
	}
	return goal, nil
}

// ReplaceThreadGoal unconditionally installs a fresh goal for the thread,
// resetting usage. Mirrors the Rust `replace_thread_goal`.
func (g *GoalStore) ReplaceThreadGoal(ctx context.Context, threadID, objective string, status ThreadGoalStatus, tokenBudget *int64) (*ThreadGoal, error) {
	goalID := uuid.NewString()
	nowMs := datetimeToEpochMillis(time.Now().UTC())
	status = statusAfterBudgetLimit(status, 0 /* tokensUsed */, tokenBudget)
	row := g.pool.QueryRowContext(ctx, `
INSERT INTO thread_goals (
    thread_id,
    goal_id,
    objective,
    status,
    token_budget,
    tokens_used,
    time_used_seconds,
    created_at_ms,
    updated_at_ms
) VALUES (?, ?, ?, ?, ?, 0, 0, ?, ?)
ON CONFLICT(thread_id) DO UPDATE SET
    goal_id = excluded.goal_id,
    objective = excluded.objective,
    status = excluded.status,
    token_budget = excluded.token_budget,
    tokens_used = 0,
    time_used_seconds = 0,
    created_at_ms = excluded.created_at_ms,
    updated_at_ms = excluded.updated_at_ms
RETURNING`+threadGoalColumns+`
`, threadID, goalID, objective, status.AsStr(), tokenBudget, nowMs, nowMs)
	goal, err := threadGoalFromRow(row)
	if err != nil {
		return nil, fmt.Errorf("replace thread goal %s: %w", threadID, err)
	}
	return goal, nil
}

// InsertThreadGoal creates a new goal only when none exists, returning nil when
// the thread already has a goal. Mirrors the Rust `insert_thread_goal`.
func (g *GoalStore) InsertThreadGoal(ctx context.Context, threadID, objective string, status ThreadGoalStatus, tokenBudget *int64) (*ThreadGoal, error) {
	goalID := uuid.NewString()
	nowMs := datetimeToEpochMillis(time.Now().UTC())
	status = statusAfterBudgetLimit(status, 0 /* tokensUsed */, tokenBudget)
	row := g.pool.QueryRowContext(ctx, `
INSERT INTO thread_goals (
    thread_id,
    goal_id,
    objective,
    status,
    token_budget,
    tokens_used,
    time_used_seconds,
    created_at_ms,
    updated_at_ms
) VALUES (?, ?, ?, ?, ?, 0, 0, ?, ?)
ON CONFLICT(thread_id) DO NOTHING
RETURNING`+threadGoalColumns+`
`, threadID, goalID, objective, status.AsStr(), tokenBudget, nowMs, nowMs)
	goal, err := threadGoalFromRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("insert thread goal %s: %w", threadID, err)
	}
	return goal, nil
}

// UpdateThreadGoal applies a partial update, returning nil when no goal
// matched. Mirrors the Rust `update_thread_goal`, including the status CASE
// rules: paused/blocked never clobber a budget_limited goal, and re-activating
// a goal already over budget keeps it budget_limited.
func (g *GoalStore) UpdateThreadGoal(ctx context.Context, threadID string, update GoalUpdate) (*ThreadGoal, error) {
	nowMs := datetimeToEpochMillis(time.Now().UTC())
	budgetLimited := GoalStatusBudgetLimited.AsStr()
	var result sql.Result
	var err error
	switch {
	case update.Status != nil && update.TokenBudget != nil:
		status := update.Status.AsStr()
		tokenBudget := *update.TokenBudget
		result, err = g.pool.ExecContext(ctx, `
UPDATE thread_goals
SET
    objective = COALESCE(?, objective),
    status = CASE
        WHEN status = ? AND ? IN (?, ?) THEN status
        WHEN ? = 'active' AND ? IS NOT NULL AND tokens_used >= ? THEN ?
        ELSE ?
    END,
    token_budget = ?,
    updated_at_ms = ?
WHERE thread_id = ?
  AND (? IS NULL OR goal_id = ?)
`,
			update.Objective,
			budgetLimited, status, GoalStatusPaused.AsStr(), GoalStatusBlocked.AsStr(),
			status, tokenBudget, tokenBudget, budgetLimited,
			status,
			tokenBudget,
			nowMs,
			threadID,
			update.ExpectedGoalID, update.ExpectedGoalID,
		)
	case update.Status != nil:
		status := update.Status.AsStr()
		result, err = g.pool.ExecContext(ctx, `
UPDATE thread_goals
SET
    objective = COALESCE(?, objective),
    status = CASE
        WHEN status = ? AND ? IN (?, ?) THEN status
        WHEN ? = 'active' AND token_budget IS NOT NULL AND tokens_used >= token_budget THEN ?
        ELSE ?
    END,
    updated_at_ms = ?
WHERE thread_id = ?
  AND (? IS NULL OR goal_id = ?)
`,
			update.Objective,
			budgetLimited, status, GoalStatusPaused.AsStr(), GoalStatusBlocked.AsStr(),
			status, budgetLimited,
			status,
			nowMs,
			threadID,
			update.ExpectedGoalID, update.ExpectedGoalID,
		)
	case update.TokenBudget != nil:
		tokenBudget := *update.TokenBudget
		result, err = g.pool.ExecContext(ctx, `
UPDATE thread_goals
SET
    objective = COALESCE(?, objective),
    token_budget = ?,
    status = CASE
        WHEN status = 'active' AND ? IS NOT NULL AND tokens_used >= ? THEN ?
        ELSE status
    END,
    updated_at_ms = ?
WHERE thread_id = ?
  AND (? IS NULL OR goal_id = ?)
`,
			update.Objective,
			tokenBudget,
			tokenBudget, tokenBudget, budgetLimited,
			nowMs,
			threadID,
			update.ExpectedGoalID, update.ExpectedGoalID,
		)
	case update.Objective != nil:
		result, err = g.pool.ExecContext(ctx, `
UPDATE thread_goals
SET
    objective = ?,
    updated_at_ms = ?
WHERE thread_id = ?
  AND (? IS NULL OR goal_id = ?)
`,
			*update.Objective,
			nowMs,
			threadID,
			update.ExpectedGoalID, update.ExpectedGoalID,
		)
	default:
		// No fields to write: read the current goal, honoring the goal-id scope.
		goal, err := g.GetThreadGoal(ctx, threadID)
		if err != nil {
			return nil, err
		}
		if goal != nil && update.ExpectedGoalID != nil && goal.GoalID != *update.ExpectedGoalID {
			return nil, nil
		}
		return goal, nil
	}
	if err != nil {
		return nil, fmt.Errorf("update thread goal %s: %w", threadID, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("update thread goal %s rows affected: %w", threadID, err)
	}
	if affected == 0 {
		return nil, nil
	}
	// Read-back in a second round-trip, faithfully mirroring the Rust
	// update_thread_goal (execute + rows_affected + get_thread_goal). Like the
	// Rust, a concurrent writer can land between the UPDATE and this SELECT;
	// the returned record is the row's current state, not necessarily this
	// update's direct result.
	return g.GetThreadGoal(ctx, threadID)
}

// PauseActiveThreadGoal marks an active goal paused, returning nil when no goal
// matched. Mirrors the Rust `pause_active_thread_goal`.
func (g *GoalStore) PauseActiveThreadGoal(ctx context.Context, threadID string) (*ThreadGoal, error) {
	return g.updateActiveThreadGoalStatus(ctx, threadID, GoalStatusPaused)
}

// UsageLimitActiveThreadGoal marks an active (or budget-limited) goal
// usage-limited, returning nil when no goal matched. Mirrors the Rust
// `usage_limit_active_thread_goal`.
func (g *GoalStore) UsageLimitActiveThreadGoal(ctx context.Context, threadID string) (*ThreadGoal, error) {
	return g.updateActiveThreadGoalStatus(ctx, threadID, GoalStatusUsageLimited)
}

// updateActiveThreadGoalStatus transitions an active goal to the given status;
// usage_limited additionally captures budget_limited goals. Mirrors the Rust
// `update_active_thread_goal_status`.
func (g *GoalStore) updateActiveThreadGoalStatus(ctx context.Context, threadID string, status ThreadGoalStatus) (*ThreadGoal, error) {
	nowMs := datetimeToEpochMillis(time.Now().UTC())
	result, err := g.pool.ExecContext(ctx, `
UPDATE thread_goals
SET
    status = ?,
    updated_at_ms = ?
WHERE thread_id = ?
  AND (
      status = 'active'
      OR (
          ? = 'usage_limited'
          AND status = 'budget_limited'
      )
  )
`, status.AsStr(), nowMs, threadID, status.AsStr())
	if err != nil {
		return nil, fmt.Errorf("update active thread goal %s: %w", threadID, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("update active thread goal %s rows affected: %w", threadID, err)
	}
	if affected == 0 {
		return nil, nil
	}
	// Read-back mirrors the Rust update_active_thread_goal_status; see the
	// concurrency note in UpdateThreadGoal.
	return g.GetThreadGoal(ctx, threadID)
}

// DeleteThreadGoal removes the thread's goal, reporting whether a row was
// deleted. Mirrors the Rust `delete_thread_goal`.
func (g *GoalStore) DeleteThreadGoal(ctx context.Context, threadID string) (bool, error) {
	result, err := g.pool.ExecContext(ctx, `
DELETE FROM thread_goals
WHERE thread_id = ?
`, threadID)
	if err != nil {
		return false, fmt.Errorf("delete thread goal %s: %w", threadID, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("delete thread goal %s rows affected: %w", threadID, err)
	}
	return affected > 0, nil
}

// AccountThreadGoalUsage advances a goal's token/time usage under the given
// mode, promoting an eligible goal to budget_limited when the accumulated
// tokens reach the budget. Mirrors the Rust `account_thread_goal_usage`.
func (g *GoalStore) AccountThreadGoalUsage(ctx context.Context, threadID string, timeDeltaSeconds, tokenDelta int64, mode GoalAccountingMode, expectedGoalID *string) (GoalAccountingOutcome, error) {
	timeDeltaSeconds = max(timeDeltaSeconds, 0)
	tokenDelta = max(tokenDelta, 0)
	if timeDeltaSeconds == 0 && tokenDelta == 0 {
		goal, err := g.GetThreadGoal(ctx, threadID)
		if err != nil {
			return GoalAccountingOutcome{}, err
		}
		return GoalAccountingOutcome{Kind: GoalAccountingOutcomeUnchanged, Goal: goal}, nil
	}

	nowMs := datetimeToEpochMillis(time.Now().UTC())
	const activeOrStoppedStatusFilter = "status IN ('active', 'paused', 'blocked', 'usage_limited', 'budget_limited')"
	var statusFilter string
	switch mode {
	case GoalAccountingModeActiveStatusOnly:
		statusFilter = "status = 'active'"
	case GoalAccountingModeActiveOnly:
		statusFilter = "status IN ('active', 'budget_limited')"
	case GoalAccountingModeActiveOrComplete:
		statusFilter = "status IN ('active', 'budget_limited', 'complete')"
	case GoalAccountingModeActiveOrStopped:
		statusFilter = activeOrStoppedStatusFilter
	default:
		return GoalAccountingOutcome{}, fmt.Errorf("unknown goal accounting mode %d", mode)
	}
	budgetLimitStatusFilter := "status = 'active'"
	if mode == GoalAccountingModeActiveOrStopped {
		budgetLimitStatusFilter = activeOrStoppedStatusFilter
	}

	var sb strings.Builder
	args := make([]any, 0, 9)
	sb.WriteString(`
UPDATE thread_goals
SET
    time_used_seconds = time_used_seconds + ?,
    tokens_used = tokens_used + ?,
    status = CASE
        WHEN `)
	args = append(args, timeDeltaSeconds, tokenDelta)
	sb.WriteString(budgetLimitStatusFilter)
	sb.WriteString(`
            AND token_budget IS NOT NULL
            AND tokens_used + ? >= token_budget
            THEN ?
        ELSE status
    END,
    updated_at_ms = ?
WHERE thread_id = ? AND `)
	args = append(args, tokenDelta, GoalStatusBudgetLimited.AsStr(), nowMs, threadID)
	sb.WriteString(statusFilter)
	if expectedGoalID != nil {
		sb.WriteString(" AND goal_id = ?")
		args = append(args, *expectedGoalID)
	}
	sb.WriteString("\nRETURNING" + threadGoalColumns + "\n")

	row := g.pool.QueryRowContext(ctx, sb.String(), args...)
	updated, err := threadGoalFromRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		goal, err := g.GetThreadGoal(ctx, threadID)
		if err != nil {
			return GoalAccountingOutcome{}, err
		}
		return GoalAccountingOutcome{Kind: GoalAccountingOutcomeUnchanged, Goal: goal}, nil
	}
	if err != nil {
		return GoalAccountingOutcome{}, fmt.Errorf("account thread goal usage %s: %w", threadID, err)
	}
	return GoalAccountingOutcome{Kind: GoalAccountingOutcomeUpdated, Goal: updated}, nil
}

// rowScanner abstracts *sql.Row / *sql.Rows scanning for goal rows.
type rowScanner interface {
	Scan(dest ...any) error
}

// threadGoalFromRow decodes one thread_goals row. Mirrors the Rust
// `thread_goal_from_row` (ThreadGoalRow::try_from_row + ThreadGoal::try_from).
func threadGoalFromRow(row rowScanner) (*ThreadGoal, error) {
	var (
		goal        ThreadGoal
		status      string
		tokenBudget sql.NullInt64
		createdAtMs int64
		updatedAtMs int64
	)
	if err := row.Scan(
		&goal.ThreadID,
		&goal.GoalID,
		&goal.Objective,
		&status,
		&tokenBudget,
		&goal.TokensUsed,
		&goal.TimeUsedSeconds,
		&createdAtMs,
		&updatedAtMs,
	); err != nil {
		return nil, err
	}
	parsed, err := ParseThreadGoalStatus(status)
	if err != nil {
		return nil, err
	}
	goal.Status = parsed
	if tokenBudget.Valid {
		budget := tokenBudget.Int64
		goal.TokenBudget = &budget
	}
	createdAt, err := epochMillisToDatetime(createdAtMs)
	if err != nil {
		return nil, err
	}
	updatedAt, err := epochMillisToDatetime(updatedAtMs)
	if err != nil {
		return nil, err
	}
	goal.CreatedAt = createdAt
	goal.UpdatedAt = updatedAt
	return &goal, nil
}

// statusAfterBudgetLimit promotes an active goal to budget_limited when the
// usage already meets the budget. Mirrors the Rust `status_after_budget_limit`.
func statusAfterBudgetLimit(status ThreadGoalStatus, tokensUsed int64, tokenBudget *int64) ThreadGoalStatus {
	if status == GoalStatusActive && tokenBudget != nil && tokensUsed >= *tokenBudget {
		return GoalStatusBudgetLimited
	}
	return status
}
