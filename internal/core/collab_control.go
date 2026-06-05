package core

import (
	"context"
	"errors"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/rollout"
)

// ErrCollabManagerUnavailable mirrors the Rust `CodexErr::UnsupportedOperation`
// path that `collab_spawn_error` / `collab_agent_error` map to "collab manager
// unavailable". The multiagent control adapter returns it (wrapped) when the
// underlying thread engine is no longer reachable.
var ErrCollabManagerUnavailable = errors.New("core: collab manager unavailable")

// CollabControl is the narrow control-plane seam the deferred multi-agent tool
// executors depend on. It is the core-side analogue of the Rust
// `session.services.agent_control` (`AgentControl`), reduced to the surface the
// spawn / send_input / resume_agent / wait_agent / close_agent handlers reach.
//
// It is satisfied by internal/multiagent (which wires a *multiagent.Control plus
// the per-thread services). Keeping it here, where it is consumed, avoids an
// import cycle: internal/multiagent imports internal/core, so core cannot import
// the concrete control plane and instead accepts this small interface through
// [BuiltinToolDeps].
//
// All methods are safe for concurrent use.
type CollabControl interface {
	// SpawnAgent spawns a new child agent thread and routes its initial op,
	// returning the spawned thread id, its committed metadata, and the status
	// observed right after spawn. Mirrors `AgentControl::spawn_agent_with_metadata`.
	SpawnAgent(ctx context.Context, req CollabSpawnRequest) (CollabSpawnResult, error)
	// SendInput routes an op to an existing agent thread, returning the generated
	// submission id. Mirrors `AgentControl::send_input`.
	SendInput(ctx context.Context, agentID protocol.ThreadID, op protocol.Op) (string, error)
	// InterruptAgent submits an interrupt to an existing agent thread. Mirrors
	// `AgentControl::interrupt_agent`.
	InterruptAgent(ctx context.Context, agentID protocol.ThreadID) (string, error)
	// CloseAgent marks the agent's spawn edge closed and shuts down the agent and
	// its live descendants. Mirrors `AgentControl::close_agent`.
	CloseAgent(ctx context.Context, agentID protocol.ThreadID) error
	// ResumeAgent reopens a closed agent from its persisted rollout. Mirrors
	// `AgentControl::resume_agent_from_rollout`.
	ResumeAgent(ctx context.Context, req CollabResumeRequest) error
	// GetStatus returns the latest status for an agent, or NotFound when the agent
	// is unavailable. Mirrors `AgentControl::get_status`.
	GetStatus(agentID protocol.ThreadID) protocol.AgentStatus
	// GetAgentMetadata returns the registry metadata for an agent, or nil when not
	// tracked. Mirrors `AgentControl::get_agent_metadata`.
	GetAgentMetadata(agentID protocol.ThreadID) *CollabAgentMetadata
	// AgentConfigSnapshot returns the effective config snapshot for a spawned
	// agent, or nil when unavailable. Mirrors
	// `AgentControl::get_agent_config_snapshot`.
	AgentConfigSnapshot(agentID protocol.ThreadID) *CollabAgentConfigSnapshot
	// SubscribeStatus returns the current status, a channel of subsequent status
	// transitions, and an unsubscribe function for an agent. It returns
	// [ErrThreadNotFound] when the agent is not live. Mirrors
	// `AgentControl::subscribe_status`.
	SubscribeStatus(ctx context.Context, agentID protocol.ThreadID) (protocol.AgentStatus, <-chan protocol.AgentStatus, func(), error)
}

// CollabSpawnRequest bundles the inputs for [CollabControl.SpawnAgent]. The
// child config is built by the spawn executor (the runtime-only turn overrides +
// base instructions) and the source classifies the child's origin.
type CollabSpawnRequest struct {
	// Configuration is the resolved configuration for the child thread.
	Configuration SessionConfiguration
	// InitialOp is the first operation routed to the child after spawn.
	InitialOp protocol.Op
	// Source classifies the child's origin (a thread-spawn source for collab).
	Source rollout.SessionSource
	// ForkContext requests a full-history fork of the parent instead of a fresh
	// thread. The control plane reads the parent's rollout to derive the history.
	ForkContext bool
	// Environments overrides the inherited turn environments for the child.
	Environments *[]protocol.TurnEnvironmentSelection
}

// CollabResumeRequest bundles the inputs for [CollabControl.ResumeAgent].
type CollabResumeRequest struct {
	// Configuration is the resolved configuration for the resumed thread.
	Configuration SessionConfiguration
	// ThreadID is the closed agent's thread id to reopen.
	ThreadID protocol.ThreadID
	// Source classifies the resumed child's origin (a thread-spawn source).
	Source rollout.SessionSource
}

// CollabSpawnResult is the outcome of a successful spawn: the new thread id, its
// committed metadata, and the status observed right after spawn. It mirrors the
// Rust `LiveAgent`.
type CollabSpawnResult struct {
	// ThreadID is the spawned agent's thread id.
	ThreadID protocol.ThreadID
	// Metadata is the registry bookkeeping committed for the agent.
	Metadata CollabAgentMetadata
	// Status is the agent status captured right after the spawn.
	Status protocol.AgentStatus
}

// CollabAgentMetadata is the per-agent bookkeeping the executors read for event
// fields (nickname/role/path). It mirrors the relevant fields of the Rust
// `AgentMetadata`.
type CollabAgentMetadata struct {
	// AgentPath is the canonical agent path, if any.
	AgentPath *protocol.AgentPath
	// AgentNickname is the human-facing nickname reserved for this agent, if any.
	AgentNickname *string
	// AgentRole names the role config used to seed the agent, if any.
	AgentRole *string
}

// CollabAgentConfigSnapshot is the effective per-agent config snapshot the spawn
// executor reads to report the child's effective model / reasoning / source. It
// mirrors the subset of the Rust `AgentConfigSnapshot` the spawn handler uses.
type CollabAgentConfigSnapshot struct {
	// Model is the effective model slug for the child.
	Model string
	// ReasoningEffort is the effective reasoning effort, if set.
	ReasoningEffort *protocol.ReasoningEffort
	// Source is the resolved session source for the child (carries path/nickname/
	// role once committed).
	Source rollout.SessionSource
}
