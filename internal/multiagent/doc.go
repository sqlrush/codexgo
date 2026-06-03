// Package multiagent implements multi-agent orchestration: spawning and
// managing child agents (sub-agents), routing inter-agent communication, and
// agent lifecycle control. It is a faithful Go port of the Rust
// `codex-core` agent control plane (`core/src/agent/control.rs`,
// `core/src/agent/registry.rs`, `core/src/agent/status.rs`).
//
// # Topology
//
// A [Control] is the control-plane handle held by a root session. It is created
// at most once per root thread/session tree and shared with every sub-agent
// spawned from that root, so the in-memory [Registry] is scoped to that root
// rather than the whole thread manager.
//
// Control composes three collaborators, all reached through their public APIs:
//
//   - the core thread engine ([ThreadEngine], satisfied by *core.ThreadManager)
//     spawns/resumes/forks child threads and routes submissions to them via the
//     core.Codex facade;
//   - the persisted spawn-edge topology ([agentgraph.AgentGraphStore]) records
//     directional parent/child edges so the agent tree survives restarts;
//   - the in-memory [Registry] tracks live agent metadata (nicknames, paths,
//     last task message) and enforces the per-session sub-agent limit.
//
// # Lifecycle
//
// Control exposes spawn (with metadata), inter-agent message routing, status
// inspection/collection, and shutdown/close of an agent and its live
// descendants. These mirror the Rust `AgentControl` methods of the same intent.
//
// # Immutability and concurrency
//
// All exported value types ([AgentMetadata], [LiveAgent], [ListedAgent]) are
// returned by value or as fresh copies; callers never observe internal mutation.
// The [Registry] is safe for concurrent use.
package multiagent
