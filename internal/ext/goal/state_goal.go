// Package goal is the faithful Go port of codex's ext/goal crate: the
// goal/intention tracking state machine persisted per thread. It exposes the
// goal tools (get/create/update), the per-thread goal runtime, accounting state,
// and the extension wiring that installs them into the engine's extension
// registry.
//
// The crate depends on the host's persistent goal store, thread manager, metrics
// client, and turn steering. Those host capabilities are not yet ported into
// dedicated Go packages, so this package defines the minimal interfaces it needs
// (StateRuntime, ThreadGoalsStore, ThreadManager, MetricsClient) and a
// package-local value type for the persisted goal. These mirror the Rust
// `codex_state` types field-for-field; once a Go state-goals store exists, hosts
// implement these interfaces over it.
package goal

import (
	"context"
	"time"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// StateThreadGoalStatus is the persisted status of a thread goal. Mirrors the
// Rust `codex_state::ThreadGoalStatus`. Note that AsStr uses snake_case
// (usage_limited / budget_limited), distinct from the protocol camelCase form.
type StateThreadGoalStatus int

// StateThreadGoalStatus variants, in the Rust declaration order.
const (
	StateGoalStatusActive StateThreadGoalStatus = iota
	StateGoalStatusPaused
	StateGoalStatusBlocked
	StateGoalStatusUsageLimited
	StateGoalStatusBudgetLimited
	StateGoalStatusComplete
)

// AsStr returns the canonical persisted string for the status. Mirrors Rust
// `ThreadGoalStatus::as_str`.
func (s StateThreadGoalStatus) AsStr() string {
	switch s {
	case StateGoalStatusActive:
		return "active"
	case StateGoalStatusPaused:
		return "paused"
	case StateGoalStatusBlocked:
		return "blocked"
	case StateGoalStatusUsageLimited:
		return "usage_limited"
	case StateGoalStatusBudgetLimited:
		return "budget_limited"
	case StateGoalStatusComplete:
		return "complete"
	default:
		return ""
	}
}

// IsActive reports whether the status is Active. Mirrors Rust
// `ThreadGoalStatus::is_active`.
func (s StateThreadGoalStatus) IsActive() bool {
	return s == StateGoalStatusActive
}

// IsTerminal reports whether the status is a terminal accounting status. Mirrors
// Rust `ThreadGoalStatus::is_terminal`.
func (s StateThreadGoalStatus) IsTerminal() bool {
	return s == StateGoalStatusBudgetLimited || s == StateGoalStatusComplete
}

// StateThreadGoal is the persisted thread goal record. Mirrors the Rust
// `codex_state::ThreadGoal` struct field-for-field. CreatedAt/UpdatedAt are
// wall-clock timestamps (Rust uses `DateTime<Utc>`); their unix-second forms
// populate the protocol ThreadGoal.
type StateThreadGoal struct {
	ThreadID        protocol.ThreadID
	GoalID          string
	Objective       string
	Status          StateThreadGoalStatus
	TokenBudget     *int64
	TokensUsed      int64
	TimeUsedSeconds int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// GoalUpdate is the partial-update payload for an existing goal. Mirrors the Rust
// `codex_state::GoalUpdate`.
//
// The doubly-optional TokenBudget mirrors `Option<Option<i64>>`: the outer
// pointer marks "field present in the update"; the inner pointer marks "set vs.
// clear". ExpectedGoalID, when present, scopes the update to a specific goal id.
type GoalUpdate struct {
	Objective      *string
	Status         *StateThreadGoalStatus
	TokenBudget    **int64
	ExpectedGoalID *string
}

// GoalAccountingMode selects which goal statuses are eligible for usage
// accounting. Mirrors the Rust `codex_state::GoalAccountingMode`.
type GoalAccountingMode int

// GoalAccountingMode variants, in the Rust declaration order.
const (
	GoalAccountingModeActiveStatusOnly GoalAccountingMode = iota
	GoalAccountingModeActiveOnly
	GoalAccountingModeActiveOrComplete
	GoalAccountingModeActiveOrStopped
)

// GoalAccountingOutcomeKind discriminates a [GoalAccountingOutcome]. Mirrors the
// Rust `GoalAccountingOutcome` enum.
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

// GoalAccountingOutcome is the result of accounting goal usage. Mirrors the Rust
// `GoalAccountingOutcome` enum (struct + discriminator). For the Unchanged
// variant, Goal is nil when no goal exists.
type GoalAccountingOutcome struct {
	Kind GoalAccountingOutcomeKind
	Goal *StateThreadGoal
}

// ThreadGoalsStore is the persistent goal store the goal extension reads and
// mutates. Mirrors the host-facing methods of the Rust `codex_state::GoalStore`.
//
// Each method returns the affected goal (nil when none matched) so callers can
// emit events and account metrics. Errors are returned wrapped by the caller.
type ThreadGoalsStore interface {
	// GetThreadGoal returns the current goal for the thread, or nil.
	GetThreadGoal(ctx context.Context, threadID protocol.ThreadID) (*StateThreadGoal, error)

	// InsertThreadGoal creates a new goal only when none exists, returning nil
	// when the thread already has a goal.
	InsertThreadGoal(ctx context.Context, threadID protocol.ThreadID, objective string, status StateThreadGoalStatus, tokenBudget *int64) (*StateThreadGoal, error)

	// UpdateThreadGoal applies a partial update, returning nil when no goal
	// matched.
	UpdateThreadGoal(ctx context.Context, threadID protocol.ThreadID, update GoalUpdate) (*StateThreadGoal, error)

	// AccountThreadGoalUsage advances a goal's token/time usage under the given
	// mode, returning the accounting outcome.
	AccountThreadGoalUsage(ctx context.Context, threadID protocol.ThreadID, timeDeltaSeconds, tokenDelta int64, mode GoalAccountingMode, expectedGoalID *string) (GoalAccountingOutcome, error)

	// UsageLimitActiveThreadGoal marks an active (or budget-limited) goal
	// usage-limited, returning nil when no goal matched.
	UsageLimitActiveThreadGoal(ctx context.Context, threadID protocol.ThreadID) (*StateThreadGoal, error)
}

// StateRuntime is the subset of the host state runtime the goal extension needs:
// the goal store plus the thread-preview helper. Mirrors the relevant methods of
// the Rust `codex_state::StateRuntime`.
type StateRuntime interface {
	// ThreadGoals returns the persistent goal store.
	ThreadGoals() ThreadGoalsStore

	// SetThreadPreviewIfEmpty sets a thread's preview only when it is currently
	// empty, returning whether it was set.
	SetThreadPreviewIfEmpty(ctx context.Context, threadID protocol.ThreadID, preview string) (bool, error)
}
