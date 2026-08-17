package multiagent

import (
	"context"

	"github.com/sqlrush/codexgo/internal/core"
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/rollout"
)

// ThreadEngine is the narrow seam over the core thread engine that [Control]
// needs to spawn, route to, inspect, and tear down child agent threads. It is
// satisfied directly by *core.ThreadManager; tests inject a fake.
//
// It is deliberately small: it exposes only the spawn/resume/fork lifecycle plus
// the in-memory thread registry lookups, mirroring the subset of the Rust
// `ThreadManagerState` that `AgentControl` reaches.
type ThreadEngine interface {
	// StartThreadWithOptions spawns a brand-new child thread with the supplied
	// options (configuration, initial history, session source).
	StartThreadWithOptions(ctx context.Context, options core.StartThreadOptions) (core.NewThread, error)
	// ResumeThreadFromRollout resumes a child thread by reading its persisted
	// history from a rollout path.
	ResumeThreadFromRollout(ctx context.Context, cfg core.SessionConfiguration, rolloutPath string) (core.NewThread, error)
	// ResumeThreadByID resumes a child thread by reading its persisted history
	// from the store keyed by thread id, under the supplied session source.
	ResumeThreadByID(ctx context.Context, cfg core.SessionConfiguration, threadID protocol.ThreadID, source rollout.SessionSource) (core.NewThread, error)
	// ForkThread forks a child thread by reading the parent's persisted history
	// from a rollout path and snapshotting it per snapshot.
	ForkThread(ctx context.Context, snapshot core.ForkSnapshot, cfg core.SessionConfiguration, rolloutPath string, threadSource *rollout.ThreadSource, persistExtendedHistory bool) (core.NewThread, error)
	// GetThread returns the live thread for id, or an error when not tracked.
	GetThread(id protocol.ThreadID) (*core.CodexThread, error)
	// RemoveThread removes id from the in-memory registry, returning the thread
	// when present.
	RemoveThread(id protocol.ThreadID) *core.CodexThread
}

// compile-time assertion that *core.ThreadManager satisfies ThreadEngine.
var _ ThreadEngine = (*core.ThreadManager)(nil)
