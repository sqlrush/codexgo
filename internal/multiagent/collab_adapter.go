package multiagent

// CollabAdapter adapts a [*Control] onto the narrow [core.CollabControl] seam
// the deferred collab tool executors dispatch through. It is the production
// wiring between core's tool handlers and the multi-agent control plane (the
// Rust handlers reach `session.services.agent_control` directly; Go inverts the
// dependency through the interface because multiagent imports core).

import (
	"context"
	"fmt"

	"github.com/sqlrush/codexgo/internal/core"
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/rollout"
)

// CollabAdapter implements core.CollabControl over a [*Control].
type CollabAdapter struct {
	control *Control
}

var _ core.CollabControl = (*CollabAdapter)(nil)

// NewCollabAdapter wraps control for the core tool-executor seam.
func NewCollabAdapter(control *Control) *CollabAdapter {
	return &CollabAdapter{control: control}
}

// SpawnAgent spawns a child agent thread and routes its initial op. Mirrors
// `AgentControl::spawn_agent_with_metadata` (fresh-thread path).
//
// STUB: ForkContext (full-history fork) needs the parent thread's rollout path
// threaded from the originating session; until the rollout-path plumbing lands
// the fork request is rejected with a model-facing error (DEVIATIONS).
func (a *CollabAdapter) SpawnAgent(ctx context.Context, req core.CollabSpawnRequest) (core.CollabSpawnResult, error) {
	if req.ForkContext {
		return core.CollabSpawnResult{}, fmt.Errorf("full-history forked spawns are not yet supported; spawn without fork_context")
	}
	live, err := a.control.SpawnAgent(ctx, req.Configuration, req.InitialOp, req.Source, SpawnOptions{
		Environments: req.Environments,
	})
	if err != nil {
		return core.CollabSpawnResult{}, err
	}
	return core.CollabSpawnResult{
		ThreadID: live.ThreadID,
		Metadata: collabMetadataFrom(live.Metadata),
		Status:   live.Status,
	}, nil
}

// SendInput routes an op to an existing agent thread.
func (a *CollabAdapter) SendInput(ctx context.Context, agentID protocol.ThreadID, op protocol.Op) (string, error) {
	return a.control.SendInput(ctx, agentID, op)
}

// InterruptAgent submits an interrupt to an existing agent thread.
func (a *CollabAdapter) InterruptAgent(ctx context.Context, agentID protocol.ThreadID) (string, error) {
	return a.control.InterruptAgent(ctx, agentID)
}

// CloseAgent closes the agent and its live descendants.
func (a *CollabAdapter) CloseAgent(ctx context.Context, agentID protocol.ThreadID) error {
	return a.control.CloseAgent(ctx, agentID)
}

// ResumeAgent reopens a closed agent from its persisted state.
func (a *CollabAdapter) ResumeAgent(ctx context.Context, req core.CollabResumeRequest) error {
	return a.control.ResumeAgent(ctx, req.Configuration, req.ThreadID, req.Source)
}

// GetStatus returns the latest status for an agent.
func (a *CollabAdapter) GetStatus(agentID protocol.ThreadID) protocol.AgentStatus {
	return a.control.GetStatus(agentID)
}

// GetAgentMetadata returns the registry metadata for an agent, or nil.
func (a *CollabAdapter) GetAgentMetadata(agentID protocol.ThreadID) *core.CollabAgentMetadata {
	metadata := a.control.GetAgentMetadata(agentID)
	if metadata == nil {
		return nil
	}
	out := collabMetadataFrom(*metadata)
	return &out
}

// AgentConfigSnapshot returns the effective config snapshot for an agent.
func (a *CollabAdapter) AgentConfigSnapshot(agentID protocol.ThreadID) *core.CollabAgentConfigSnapshot {
	snapshot := a.control.GetAgentConfigSnapshot(agentID)
	if snapshot == nil {
		return nil
	}
	out := core.CollabAgentConfigSnapshot{
		Model:           snapshot.Model,
		ReasoningEffort: snapshot.ReasoningEffort,
	}
	if source, ok := snapshot.SessionSource.(rollout.SessionSource); ok {
		out.Source = source
	}
	return &out
}

// SubscribeStatus subscribes to an agent's status transitions.
func (a *CollabAdapter) SubscribeStatus(ctx context.Context, agentID protocol.ThreadID) (protocol.AgentStatus, <-chan protocol.AgentStatus, func(), error) {
	return a.control.SubscribeStatus(ctx, agentID)
}

// collabMetadataFrom projects registry metadata onto the core seam type.
func collabMetadataFrom(metadata AgentMetadata) core.CollabAgentMetadata {
	return core.CollabAgentMetadata{
		AgentPath:     metadata.AgentPath,
		AgentNickname: metadata.AgentNickname,
		AgentRole:     metadata.AgentRole,
	}
}
