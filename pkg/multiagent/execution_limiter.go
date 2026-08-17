package multiagent

import (
	"errors"
	"fmt"
	"sync"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// This file ports the 0.147 `AgentExecutionLimiter` (core/src/agent/control/
// execution.rs; spec 50 D0.7): sub-agent concurrency is bounded by the number
// of sub-agents currently EXECUTING a turn, not by the number of live threads
// (that is the spawn-slot limit, MaxThreads). An op that would start a turn on
// an idle sub-agent is rejected with [ErrAgentLimitReached] when no execution
// capacity is left; a running agent's follow-up input is never blocked. The
// guard is released when the agent's status leaves Running.
//
// Upstream only limits multi-agent v2 sub-agents; codexgo has the v1 tool set,
// so the limiter applies to every sub-agent thread the Control drives when a
// limiter is configured (nil = unlimited, the default). Hosts that schedule
// agents themselves (airush) either leave it nil or inject their own
// [ExecutionLimiter] to fold sub-agent turns into a tenant-level budget.

// ErrAgentLimitReached is returned when starting another sub-agent turn would
// exceed the execution limit. Mirrors `CodexErrorDetails::AgentLimitReached`.
type ErrAgentLimitReached struct {
	MaxThreads int
}

func (e *ErrAgentLimitReached) Error() string {
	return fmt.Sprintf("multiagent: agent execution limit reached (max_threads=%d)", e.MaxThreads)
}

// ExecutionLimiter bounds concurrently executing sub-agent turns.
type ExecutionLimiter interface {
	// TryAcquire takes an execution slot for the agent, returning
	// [ErrAgentLimitReached] (wrapped) when none is free. Acquiring twice for the
	// same agent without releasing is a no-op (one slot per agent).
	TryAcquire(agentID protocol.ThreadID) error
	// Release frees the agent's slot; unknown agents are ignored.
	Release(agentID protocol.ThreadID)
}

// CountingExecutionLimiter is the in-process [ExecutionLimiter]: at most
// maxThreads distinct agents hold a slot at once (upstream `active <
// max_threads`).
type CountingExecutionLimiter struct {
	maxThreads int

	mu     sync.Mutex
	active map[string]struct{}
}

// NewCountingExecutionLimiter returns a limiter allowing maxThreads concurrent
// executing sub-agents; maxThreads <= 0 means unlimited.
func NewCountingExecutionLimiter(maxThreads int) *CountingExecutionLimiter {
	return &CountingExecutionLimiter{maxThreads: maxThreads, active: make(map[string]struct{})}
}

// TryAcquire implements [ExecutionLimiter].
func (l *CountingExecutionLimiter) TryAcquire(agentID protocol.ThreadID) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := agentID.String()
	if _, held := l.active[key]; held {
		return nil
	}
	if l.maxThreads > 0 && len(l.active) >= l.maxThreads {
		return &ErrAgentLimitReached{MaxThreads: l.maxThreads}
	}
	l.active[key] = struct{}{}
	return nil
}

// Release implements [ExecutionLimiter].
func (l *CountingExecutionLimiter) Release(agentID protocol.ThreadID) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.active, agentID.String())
}

// Active reports how many agents currently hold a slot.
func (l *CountingExecutionLimiter) Active() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.active)
}

// IsAgentLimitReached reports whether err is (or wraps) [ErrAgentLimitReached].
func IsAgentLimitReached(err error) bool {
	var target *ErrAgentLimitReached
	return errors.As(err, &target)
}

// opStartsTurn mirrors upstream `op_starts_turn`: user input always starts a
// turn; inter-agent communication only when it asks to trigger one.
func opStartsTurn(op protocol.Op) bool {
	switch op.Type {
	case protocol.OpUserInput:
		return true
	case protocol.OpInterAgentCommunication:
		return op.Communication != nil && op.Communication.TriggerTurn
	default:
		return false
	}
}
