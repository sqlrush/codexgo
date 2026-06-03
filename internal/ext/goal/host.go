package goal

import (
	"context"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// MetricsClient is the subset of the host metrics client the goal extension
// uses. Mirrors the relevant methods of the Rust `codex_otel::MetricsClient`.
//
// Implementations record telemetry; errors are intentionally ignored by callers
// (the Rust code discards the Result), so this interface returns none.
type MetricsClient interface {
	// Counter increments a named counter by inc with the supplied tags.
	Counter(name string, inc int64, tags [][2]string)

	// Histogram records value into a named histogram with the supplied tags.
	Histogram(name string, value int64, tags [][2]string)
}

// LiveThread is a running thread the goal runtime can steer. Mirrors the subset
// of the Rust `CodexThread` the goal crate calls.
type LiveThread interface {
	// InjectIfRunning injects response items into the active turn, returning an
	// error when no turn is currently active.
	InjectIfRunning(ctx context.Context, items []protocol.ResponseItem) error
}

// ThreadManager resolves live threads for steering. Mirrors the subset of the
// Rust `ThreadManager` the goal crate calls.
//
// GetThread returns ErrThreadUnavailable (or any error) when the live thread is
// not available; the goal runtime treats any error as "skip steering", matching
// the Rust behavior.
type ThreadManager interface {
	// GetThread resolves the live thread for the supplied id.
	GetThread(ctx context.Context, threadID protocol.ThreadID) (LiveThread, error)
}

// GoalsEnabledFunc reports whether goals are enabled for the supplied host
// configuration. Mirrors the Rust `goals_enabled: Fn(&C) -> bool` closure.
type GoalsEnabledFunc[C any] func(config C) bool
