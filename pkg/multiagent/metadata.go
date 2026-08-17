package multiagent

import (
	"github.com/sqlrush/codexgo/pkg/protocol"
	"github.com/sqlrush/codexgo/pkg/rollout"
)

// AgentMetadata is the per-agent bookkeeping tracked in the in-memory
// [Registry]. It is a faithful port of the Rust `agent::registry::AgentMetadata`.
//
// AgentMetadata is treated as an immutable value: registry methods that mutate
// an agent's metadata clone the entry, apply the change, and store the copy.
type AgentMetadata struct {
	// AgentID is the live thread id once the agent has been spawned, or nil while
	// only a path/nickname has been reserved.
	AgentID *protocol.ThreadID
	// AgentPath is the canonical agent path (e.g. "/root/researcher"), if any.
	AgentPath *protocol.AgentPath
	// AgentNickname is the human-facing nickname reserved for this agent, if any.
	AgentNickname *string
	// AgentRole names the role config used to seed the agent, if any.
	AgentRole *string
	// LastTaskMessage is a preview of the most recent input routed to the agent.
	LastTaskMessage *string
}

// clone returns a deep copy of the metadata so callers never observe internal
// mutation (immutability).
func (m AgentMetadata) clone() AgentMetadata {
	return AgentMetadata{
		AgentID:         cloneThreadID(m.AgentID),
		AgentPath:       cloneAgentPath(m.AgentPath),
		AgentNickname:   cloneString(m.AgentNickname),
		AgentRole:       cloneString(m.AgentRole),
		LastTaskMessage: cloneString(m.LastTaskMessage),
	}
}

// pathString returns the canonical agent-path string, or "" when no path is set,
// mirroring the Rust `agent_path.as_deref().unwrap_or_default()` ordering key.
func (m AgentMetadata) pathString() string {
	if m.AgentPath == nil {
		return ""
	}
	return m.AgentPath.String()
}

// LiveAgent is the result of a successful spawn: the new thread id, its metadata,
// and the status observed immediately after spawn. It is a faithful port of the
// Rust `agent::control::LiveAgent`.
type LiveAgent struct {
	// ThreadID is the spawned agent's thread id.
	ThreadID protocol.ThreadID
	// Metadata is the registry bookkeeping committed for the agent.
	Metadata AgentMetadata
	// Status is the agent status captured right after the spawn.
	Status protocol.AgentStatus
}

// ListedAgent is a single entry in the result of [Control.ListAgents]. It is a
// faithful port of the Rust `agent::control::ListedAgent`. Field tags mirror the
// Rust `#[derive(Serialize)]` snake_case wire form.
type ListedAgent struct {
	// AgentName is the canonical agent path, or the thread id when no path exists.
	AgentName string `json:"agent_name"`
	// AgentStatus is the current lifecycle status of the agent.
	AgentStatus protocol.AgentStatus `json:"agent_status"`
	// LastTaskMessage is the most recent task preview, if any.
	LastTaskMessage *string `json:"last_task_message"`
}

// NextThreadSpawnDepth returns the depth for a child spawned from a thread whose
// session source is the given value, mirroring the Rust
// `registry::next_thread_spawn_depth`: the parent's depth saturating-incremented
// by one (non-sub-agent sources have depth zero, so children start at one).
func NextThreadSpawnDepth(source rollout.SessionSource) int32 {
	depth := sessionDepth(source)
	if depth == int32(^uint32(0)>>1) { // saturate at math.MaxInt32
		return depth
	}
	return depth + 1
}

// ExceedsThreadSpawnDepthLimit reports whether depth exceeds maxDepth, mirroring
// the Rust `registry::exceeds_thread_spawn_depth_limit`.
func ExceedsThreadSpawnDepthLimit(depth, maxDepth int32) bool {
	return depth > maxDepth
}

// sessionDepth returns the thread-spawn depth recorded in a session source, or
// zero for non thread-spawn sources, mirroring the Rust `registry::session_depth`.
func sessionDepth(source rollout.SessionSource) int32 {
	if source.Kind == rollout.SessionSourceKindSubAgent &&
		source.SubAgent != nil &&
		source.SubAgent.Kind == rollout.SubAgentSourceKindThreadSpawn &&
		source.SubAgent.ThreadSpawn != nil {
		return source.SubAgent.ThreadSpawn.Depth
	}
	return 0
}

// threadSpawnParentThreadID returns the parent thread id recorded in a
// thread-spawn session source, mirroring the Rust
// `thread_spawn_parent_thread_id`.
func threadSpawnParentThreadID(source rollout.SessionSource) *protocol.ThreadID {
	if source.Kind == rollout.SessionSourceKindSubAgent &&
		source.SubAgent != nil &&
		source.SubAgent.Kind == rollout.SubAgentSourceKindThreadSpawn &&
		source.SubAgent.ThreadSpawn != nil {
		id := source.SubAgent.ThreadSpawn.ParentThreadID
		return &id
	}
	return nil
}

func cloneString(p *string) *string {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func cloneThreadID(p *protocol.ThreadID) *protocol.ThreadID {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func cloneAgentPath(p *protocol.AgentPath) *protocol.AgentPath {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
