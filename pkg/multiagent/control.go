package multiagent

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/sqlrush/codexgo/pkg/agentgraph"
	"github.com/sqlrush/codexgo/pkg/core"
	"github.com/sqlrush/codexgo/pkg/protocol"
	"github.com/sqlrush/codexgo/pkg/rollout"
)

// rootLastTaskMessage is the last-task-message reported for the root agent in
// [Control.ListAgents], mirroring the Rust `ROOT_LAST_TASK_MESSAGE`.
const rootLastTaskMessage = "Main thread"

// ForkMode selects how a forked child agent's history is derived from its
// parent, mirroring the Rust `agent::control::SpawnAgentForkMode`.
type ForkMode struct {
	// FullHistory forks the parent's full committed history.
	FullHistory bool
	// LastNTurns, when > 0, forks only the parent's last N turns. It is honored
	// only when FullHistory is false.
	LastNTurns int
}

// SpawnOptions controls a single agent spawn. It is a reduced, faithful port of
// the Rust `agent::control::SpawnAgentOptions`.
type SpawnOptions struct {
	// Fork, when non-nil, requests a forked spawn instead of a fresh thread.
	Fork *ForkMode
	// ForkParentRolloutPath is the parent rollout path used to read the history a
	// fork is derived from. It is required when Fork is non-nil.
	ForkParentRolloutPath string
	// Environments overrides the inherited turn environments for the child.
	Environments *[]protocol.TurnEnvironmentSelection
}

// Config bundles the dependencies and limits for a [Control]. All fields except
// the limits are required.
type Config struct {
	// Engine spawns and manages child threads (dependency injection).
	Engine ThreadEngine
	// Graph persists the directional spawn-edge topology.
	Graph agentgraph.AgentGraphStore
	// SessionID is the identity shared by every agent under this root.
	SessionID protocol.SessionID
	// MaxThreads bounds the number of spawned sub-agents per session. When nil the
	// session is unbounded, mirroring the Rust `Config.agent_max_threads`.
	MaxThreads *uint64
	// NicknameCandidates resolves the nickname pool for a given agent role. When
	// nil the built-in pool is used for every role.
	NicknameCandidates func(role string) []string
	// NicknamePicker selects a nickname from the available pool (testability seam).
	NicknamePicker NicknamePicker
	// ExecutionLimiter bounds concurrently EXECUTING sub-agent turns (0.147
	// AgentExecutionLimiter); nil = unlimited. See [NewCountingExecutionLimiter].
	ExecutionLimiter ExecutionLimiter
}

// Control is the control-plane handle for multi-agent operations. It is held by
// a root session and shared with every sub-agent spawned from that root,
// mirroring the Rust `agent::control::AgentControl`.
//
// Control is safe for concurrent use; its mutable state lives in the [Registry]
// and the [agentgraph.AgentGraphStore], both of which are concurrency-safe.
type Control struct {
	engine             ThreadEngine
	graph              agentgraph.AgentGraphStore
	registry           *Registry
	sessionID          protocol.SessionID
	maxThreads         *uint64
	nicknameCandidates func(role string) []string
	executionLimiter   ExecutionLimiter
}

// NewControl constructs a [Control], validating required dependencies at the
// system boundary.
func NewControl(cfg Config) (*Control, error) {
	if cfg.Engine == nil {
		return nil, fmt.Errorf("multiagent: Config.Engine is required")
	}
	if cfg.Graph == nil {
		return nil, fmt.Errorf("multiagent: Config.Graph is required")
	}
	candidates := cfg.NicknameCandidates
	if candidates == nil {
		candidates = func(string) []string { return defaultAgentNicknameList() }
	}
	return &Control{
		engine:             cfg.Engine,
		graph:              cfg.Graph,
		registry:           NewRegistry(cfg.NicknamePicker),
		sessionID:          cfg.SessionID,
		maxThreads:         cloneUint64(cfg.MaxThreads),
		nicknameCandidates: candidates,
		executionLimiter:   cfg.ExecutionLimiter,
	}, nil
}

// SessionID returns the session id shared by all agents under this root,
// mirroring the Rust `AgentControl::session_id`.
func (c *Control) SessionID() protocol.SessionID { return c.sessionID }

// Registry returns the in-memory agent registry backing this control plane.
func (c *Control) Registry() *Registry { return c.registry }

// RegisterSessionRoot records the current thread as the root agent when its
// session source is not itself a thread-spawn child, mirroring the Rust
// `register_session_root`.
func (c *Control) RegisterSessionRoot(currentThreadID protocol.ThreadID, currentSource rollout.SessionSource) {
	if threadSpawnParentThreadID(currentSource) == nil {
		c.registry.RegisterRootThread(currentThreadID)
	}
}

// SpawnAgent spawns a new agent thread and submits its initial operation,
// returning a [LiveAgent]. It is a faithful port of the Rust
// `spawn_agent_with_metadata` / `spawn_agent_internal` (turn-running subset).
//
// cfg is the resolved configuration for the child thread. initialOp is the first
// operation routed to the child after spawn (typically an OpUserInput). source
// classifies the child's origin; a thread-spawn source drives nickname/path
// reservation, spawn-edge persistence, and inherited bookkeeping.
func (c *Control) SpawnAgent(
	ctx context.Context,
	cfg core.SessionConfiguration,
	initialOp protocol.Op,
	source rollout.SessionSource,
	options SpawnOptions,
) (LiveAgent, error) {
	reservation, err := c.registry.ReserveSpawnSlot(c.maxThreads)
	if err != nil {
		return LiveAgent{}, err
	}
	// Release the reservation if we return before committing it. Commit clears the
	// active flag, so this is a no-op on the success path.
	committed := false
	defer func() {
		if !committed {
			reservation.Release()
		}
	}()

	resolvedSource, metadata, err := c.prepareSource(reservation, cfg, source)
	if err != nil {
		return LiveAgent{}, err
	}

	newThread, err := c.spawnThread(ctx, cfg, resolvedSource, options)
	if err != nil {
		return LiveAgent{}, err
	}

	id := newThread.ThreadID
	metadata.AgentID = &id
	reservation.Commit(metadata.clone())
	committed = true

	if err := c.persistThreadSpawnEdge(ctx, newThread.ThreadID, resolvedSource); err != nil {
		return LiveAgent{}, err
	}

	if _, err := c.SendInput(ctx, newThread.ThreadID, initialOp); err != nil {
		return LiveAgent{}, err
	}

	return LiveAgent{
		ThreadID: newThread.ThreadID,
		Metadata: metadata.clone(),
		Status:   c.GetStatus(newThread.ThreadID),
	}, nil
}

// prepareSource derives the resolved session source and seed metadata for a
// spawn, reserving the agent path and nickname for thread-spawn children. It is a
// faithful port of the Rust `prepare_thread_spawn` (and the non-thread-spawn
// fall-through).
func (c *Control) prepareSource(
	reservation *SpawnReservation,
	cfg core.SessionConfiguration,
	source rollout.SessionSource,
) (rollout.SessionSource, AgentMetadata, error) {
	if source.Kind != rollout.SessionSourceKindSubAgent ||
		source.SubAgent == nil ||
		source.SubAgent.Kind != rollout.SubAgentSourceKindThreadSpawn ||
		source.SubAgent.ThreadSpawn == nil {
		return source, AgentMetadata{}, nil
	}

	spawn := source.SubAgent.ThreadSpawn
	if spawn.Depth == 1 {
		c.registry.RegisterRootThread(spawn.ParentThreadID)
	}
	if spawn.AgentPath != nil {
		if err := reservation.ReserveAgentPath(*spawn.AgentPath); err != nil {
			return rollout.SessionSource{}, AgentMetadata{}, err
		}
	}

	role := ""
	if spawn.AgentRole != nil {
		role = *spawn.AgentRole
	}
	preferred := ""
	if spawn.AgentNickname != nil {
		preferred = *spawn.AgentNickname
	}
	nickname, err := reservation.ReserveAgentNicknameWithPreference(c.nicknameCandidates(role), preferred)
	if err != nil {
		return rollout.SessionSource{}, AgentMetadata{}, err
	}

	resolved := rollout.SessionSource{
		Kind: rollout.SessionSourceKindSubAgent,
		SubAgent: &rollout.SubAgentSource{
			Kind: rollout.SubAgentSourceKindThreadSpawn,
			ThreadSpawn: &rollout.ThreadSpawnSource{
				ParentThreadID: spawn.ParentThreadID,
				Depth:          spawn.Depth,
				AgentPath:      cloneAgentPath(spawn.AgentPath),
				AgentNickname:  &nickname,
				AgentRole:      cloneString(spawn.AgentRole),
			},
		},
	}
	metadata := AgentMetadata{
		AgentPath:     cloneAgentPath(spawn.AgentPath),
		AgentNickname: &nickname,
		AgentRole:     cloneString(spawn.AgentRole),
	}
	return resolved, metadata, nil
}

// spawnThread dispatches to a forked or fresh-thread spawn, mirroring the Rust
// match over `(session_source, fork_mode)`.
func (c *Control) spawnThread(
	ctx context.Context,
	cfg core.SessionConfiguration,
	source rollout.SessionSource,
	options SpawnOptions,
) (core.NewThread, error) {
	// A child's lifetime is not the parent's tool call: the Rust child thread keeps
	// running after spawn_agent returns (the parent waits on it later, or closes
	// it). Keep the caller's values (host tenant, tracing) but drop its cancellation.
	ctx = context.WithoutCancel(ctx)
	if options.Fork != nil {
		return c.spawnForkedThread(ctx, cfg, source, options)
	}
	startOptions := core.StartThreadOptions{
		Configuration:  withSource(cfg, source, options.Environments),
		InitialHistory: rollout.NewInitialHistory(),
		SessionSource:  &source,
		ThreadSource:   subagentThreadSource(source),
	}
	return c.engine.StartThreadWithOptions(ctx, startOptions)
}

// spawnForkedThread forks a child thread from the parent's persisted history,
// mirroring the Rust `spawn_forked_thread`. The history is read by the engine
// from the supplied parent rollout path.
func (c *Control) spawnForkedThread(
	ctx context.Context,
	cfg core.SessionConfiguration,
	source rollout.SessionSource,
	options SpawnOptions,
) (core.NewThread, error) {
	if options.ForkParentRolloutPath == "" {
		return core.NewThread{}, fatalf("spawn_agent fork requires a parent rollout path")
	}
	if source.Kind != rollout.SessionSourceKindSubAgent ||
		source.SubAgent == nil ||
		source.SubAgent.Kind != rollout.SubAgentSourceKindThreadSpawn {
		return core.NewThread{}, fatalf("spawn_agent fork requires a thread-spawn session source")
	}

	snapshot := forkSnapshotForMode(*options.Fork)
	return c.engine.ForkThread(
		ctx,
		snapshot,
		withSource(cfg, source, options.Environments),
		options.ForkParentRolloutPath,
		subagentThreadSource(source),
		/*persistExtendedHistory*/ false,
	)
}

// SendInput routes an operation to an existing agent thread and records its
// preview as the agent's last task message. It is a faithful port of the Rust
// `send_input`.
func (c *Control) SendInput(ctx context.Context, agentID protocol.ThreadID, op protocol.Op) (string, error) {
	preview := renderInputPreview(op)
	subID, err := c.sendOp(ctx, agentID, op)
	if err != nil {
		return "", err
	}
	c.registry.UpdateLastTaskMessage(agentID, preview)
	return subID, nil
}

// SendInterAgentCommunication routes an inter-agent message to an existing agent
// thread, recording the message content as the last task message. It is a
// faithful port of the Rust `send_inter_agent_communication`.
func (c *Control) SendInterAgentCommunication(ctx context.Context, agentID protocol.ThreadID, communication protocol.InterAgentCommunication) (string, error) {
	preview := communication.Content
	op := protocol.Op{Type: protocol.OpInterAgentCommunication, Communication: &communication}
	subID, err := c.sendOp(ctx, agentID, op)
	if err != nil {
		return "", err
	}
	c.registry.UpdateLastTaskMessage(agentID, preview)
	return subID, nil
}

// InterruptAgent submits an interrupt to an existing agent thread, mirroring the
// Rust `interrupt_agent`.
func (c *Control) InterruptAgent(ctx context.Context, agentID protocol.ThreadID) (string, error) {
	return c.sendOp(ctx, agentID, protocol.Op{Type: protocol.OpInterrupt})
}

// sendOp routes a single op to a thread, removing a dead thread from both the
// engine and registry. It is a faithful port of the Rust
// `handle_thread_request_result` wrapping `state.send_op`.
func (c *Control) sendOp(ctx context.Context, agentID protocol.ThreadID, op protocol.Op) (string, error) {
	thread, err := c.engine.GetThread(agentID)
	if err != nil {
		return "", fmt.Errorf("multiagent: get agent %s: %w", agentID, err)
	}
	if err := c.ensureExecutionCapacity(thread, agentID, op); err != nil {
		return "", err
	}
	subID, err := thread.Submit(op)
	if err != nil {
		if errors.Is(err, core.ErrInternalAgentDied) {
			c.engine.RemoveThread(agentID)
			c.registry.ReleaseSpawnedThread(agentID)
		}
		return "", fmt.Errorf("multiagent: submit to agent %s: %w", agentID, err)
	}
	return subID, nil
}

// ShutdownLiveAgent shuts down a live agent without marking its spawn edge
// closed, then removes it from the engine and registry. It is a faithful port of
// the Rust `shutdown_live_agent`.
func (c *Control) ShutdownLiveAgent(ctx context.Context, agentID protocol.ThreadID) error {
	var shutdownErr error
	if thread, err := c.engine.GetThread(agentID); err == nil {
		if thread.AgentStatus().Kind != protocol.AgentStatusShutdown {
			shutdownErr = thread.ShutdownAndWait(ctx)
		}
	}
	c.engine.RemoveThread(agentID)
	c.registry.ReleaseSpawnedThread(agentID)
	return shutdownErr
}

// CloseAgent marks an agent's spawn edge closed in persisted state, then shuts
// down the agent and any live descendants reachable from the persisted spawn
// tree. It is a faithful port of the Rust `close_agent` + `shutdown_agent_tree`.
func (c *Control) CloseAgent(ctx context.Context, agentID protocol.ThreadID) error {
	known := c.registry.AgentMetadataForThread(agentID) != nil
	if err := c.graph.SetThreadSpawnEdgeStatus(ctx, agentID, agentgraph.ThreadSpawnEdgeStatusClosed); err != nil {
		if !known {
			return fmt.Errorf("multiagent: persist closed edge for %s: %w", agentID, err)
		}
	}
	return c.shutdownAgentTree(ctx, agentID)
}

// shutdownAgentTree shuts down agentID and every live descendant reachable from
// the persisted spawn tree, mirroring the Rust `shutdown_agent_tree`.
func (c *Control) shutdownAgentTree(ctx context.Context, agentID protocol.ThreadID) error {
	descendants, err := c.graph.ListThreadSpawnDescendants(ctx, agentID, nil)
	if err != nil {
		return fmt.Errorf("multiagent: list descendants for %s: %w", agentID, err)
	}
	result := c.ShutdownLiveAgent(ctx, agentID)
	for _, descendantID := range descendants {
		if err := c.ShutdownLiveAgent(ctx, descendantID); err != nil {
			return err
		}
	}
	return result
}

// GetStatus returns the latest status for an agent, or NotFound when the agent
// is unavailable. It is a faithful port of the Rust `get_status`.
func (c *Control) GetStatus(agentID protocol.ThreadID) protocol.AgentStatus {
	thread, err := c.engine.GetThread(agentID)
	if err != nil {
		return protocol.AgentStatus{Kind: protocol.AgentStatusNotFound}
	}
	return thread.AgentStatus()
}

// GetAgentMetadata returns the registry metadata for an agent, mirroring the Rust
// `get_agent_metadata`.
func (c *Control) GetAgentMetadata(agentID protocol.ThreadID) *AgentMetadata {
	return c.registry.AgentMetadataForThread(agentID)
}

// SubscribeStatus returns the current status, a channel of subsequent status
// transitions, and an unsubscribe function for an agent, mirroring the Rust
// `subscribe_status` (which forwards the thread's watch channel). It returns
// [core.ErrThreadNotFound] when the agent is not live.
func (c *Control) SubscribeStatus(_ context.Context, agentID protocol.ThreadID) (protocol.AgentStatus, <-chan protocol.AgentStatus, func(), error) {
	thread, err := c.engine.GetThread(agentID)
	if err != nil {
		return protocol.AgentStatus{Kind: protocol.AgentStatusNotFound}, nil, nil, fmt.Errorf("multiagent: subscribe status for %s: %w", agentID, core.ErrThreadNotFound)
	}
	current, ch, unsubscribe := thread.SubscribeAgentStatus()
	return current, ch, unsubscribe, nil
}

// ResumeAgent reopens a closed agent from its persisted rollout, mirroring the
// single-agent subset of the Rust `resume_agent_from_rollout` /
// `resume_single_agent_from_rollout`: it reserves a spawn slot, prepares the
// thread-spawn source/metadata, resumes the thread by id, commits the metadata,
// and persists the open spawn edge.
//
// STUB: re-resuming the open thread-spawn descendant tree (the Rust BFS over the
// persisted state DB) and re-reading the closed agent's nickname/role from the
// state DB are owned by the state-DB-backed agentgraph; the in-memory path
// resumes only the directly-targeted agent and re-reserves a fresh nickname. See
// DEVIATIONS.
func (c *Control) ResumeAgent(ctx context.Context, cfg core.SessionConfiguration, threadID protocol.ThreadID, source rollout.SessionSource) error {
	reservation, err := c.registry.ReserveSpawnSlot(c.maxThreads)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			reservation.Release()
		}
	}()

	resolvedSource, metadata, err := c.prepareSource(reservation, cfg, source)
	if err != nil {
		return err
	}

	resumed, err := c.engine.ResumeThreadByID(ctx, withSource(cfg, resolvedSource, nil), threadID, resolvedSource)
	if err != nil {
		return fmt.Errorf("multiagent: resume agent %s: %w", threadID, err)
	}

	id := resumed.ThreadID
	metadata.AgentID = &id
	reservation.Commit(metadata.clone())
	committed = true

	if err := c.persistThreadSpawnEdge(ctx, resumed.ThreadID, resolvedSource); err != nil {
		return err
	}
	return nil
}

// GetAgentConfigSnapshot returns the effective config snapshot for a live agent,
// mirroring the Rust `get_agent_config_snapshot`. It returns nil when the agent
// is not live.
func (c *Control) GetAgentConfigSnapshot(agentID protocol.ThreadID) *core.ThreadConfigSnapshot {
	thread, err := c.engine.GetThread(agentID)
	if err != nil {
		return nil
	}
	snapshot := thread.ConfigSnapshot()
	return &snapshot
}

// ResolveAgentReference resolves an absolute or relative agent reference against
// the current session source's agent path, returning the live thread id. It is a
// faithful port of the Rust `resolve_agent_reference`.
func (c *Control) ResolveAgentReference(currentSource rollout.SessionSource, reference string) (protocol.ThreadID, error) {
	base := protocol.AgentPathRootValue()
	if p := currentSource.AgentPath(); p != nil {
		base = *p
	}
	path, err := resolveAgentPath(base, reference)
	if err != nil {
		return protocol.ThreadID{}, err
	}
	if id := c.registry.AgentIDForPath(path); id != nil {
		return *id, nil
	}
	return protocol.ThreadID{}, unsupportedOperationf("live agent path `%s` not found", path.String())
}

// ListLiveSubtreeThreadIDs returns agentID followed by every live descendant
// reachable from the persisted spawn tree, mirroring the Rust
// `list_live_agent_subtree_thread_ids`.
func (c *Control) ListLiveSubtreeThreadIDs(ctx context.Context, agentID protocol.ThreadID) ([]protocol.ThreadID, error) {
	descendants, err := c.graph.ListThreadSpawnDescendants(ctx, agentID, nil)
	if err != nil {
		return nil, fmt.Errorf("multiagent: list subtree for %s: %w", agentID, err)
	}
	out := make([]protocol.ThreadID, 0, len(descendants)+1)
	out = append(out, agentID)
	out = append(out, descendants...)
	return out, nil
}

// ListAgents lists the live agents (optionally under a path prefix) plus the root
// agent, ordered by agent path then thread id. It is a faithful port of the Rust
// `list_agents`.
func (c *Control) ListAgents(currentSource rollout.SessionSource, pathPrefix *string) ([]ListedAgent, error) {
	var resolvedPrefix *protocol.AgentPath
	if pathPrefix != nil {
		base := protocol.AgentPathRootValue()
		if p := currentSource.AgentPath(); p != nil {
			base = *p
		}
		prefix, err := resolveAgentPath(base, *pathPrefix)
		if err != nil {
			return nil, err
		}
		resolvedPrefix = &prefix
	}

	liveAgents := c.registry.LiveAgents()
	sort.Slice(liveAgents, func(i, j int) bool {
		li, lj := liveAgents[i], liveAgents[j]
		if li.pathString() != lj.pathString() {
			return li.pathString() < lj.pathString()
		}
		return threadIDKey(li.AgentID) < threadIDKey(lj.AgentID)
	})

	root := protocol.AgentPathRootValue()
	agents := make([]ListedAgent, 0, len(liveAgents)+1)
	if resolvedPrefix == nil || agentMatchesPrefix(&root, *resolvedPrefix) {
		if rootID := c.registry.AgentIDForPath(root); rootID != nil {
			if thread, err := c.engine.GetThread(*rootID); err == nil {
				rootMsg := rootLastTaskMessage
				agents = append(agents, ListedAgent{
					AgentName:       root.String(),
					AgentStatus:     thread.AgentStatus(),
					LastTaskMessage: &rootMsg,
				})
			}
		}
	}

	for _, metadata := range liveAgents {
		if metadata.AgentID == nil {
			continue
		}
		if resolvedPrefix != nil && !agentMatchesPrefix(metadata.AgentPath, *resolvedPrefix) {
			continue
		}
		thread, err := c.engine.GetThread(*metadata.AgentID)
		if err != nil {
			continue
		}
		name := metadata.AgentID.String()
		if metadata.AgentPath != nil {
			name = metadata.AgentPath.String()
		}
		agents = append(agents, ListedAgent{
			AgentName:       name,
			AgentStatus:     thread.AgentStatus(),
			LastTaskMessage: cloneString(metadata.LastTaskMessage),
		})
	}
	return agents, nil
}

// persistThreadSpawnEdge records an open spawn edge for a thread-spawn child,
// mirroring the Rust `persist_thread_spawn_edge_for_source`. Non thread-spawn
// sources have no parent edge to persist.
func (c *Control) persistThreadSpawnEdge(ctx context.Context, childThreadID protocol.ThreadID, source rollout.SessionSource) error {
	parentThreadID := threadSpawnParentThreadID(source)
	if parentThreadID == nil {
		return nil
	}
	if err := c.graph.UpsertThreadSpawnEdge(ctx, *parentThreadID, childThreadID, agentgraph.ThreadSpawnEdgeStatusOpen); err != nil {
		return fmt.Errorf("multiagent: persist spawn edge for %s: %w", childThreadID, err)
	}
	return nil
}

// withSource returns a configuration carrying the resolved session source and
// any environment override, without mutating the input (immutability).
func withSource(cfg core.SessionConfiguration, source rollout.SessionSource, environments *[]protocol.TurnEnvironmentSelection) core.SessionConfiguration {
	next := cfg
	next.SessionSource = source
	if environments != nil {
		next.Environments = append([]protocol.TurnEnvironmentSelection(nil), (*environments)...)
	}
	return next
}

// subagentThreadSource returns the subagent thread-source classification for a
// thread-spawn source, mirroring the Rust `Some(ThreadSource::Subagent)` passed
// for spawned children.
func subagentThreadSource(source rollout.SessionSource) *rollout.ThreadSource {
	if source.Kind == rollout.SessionSourceKindSubAgent {
		ts := rollout.ThreadSourceSubagent
		return &ts
	}
	return nil
}

// forkSnapshotForMode maps a [ForkMode] onto a core fork snapshot. FullHistory
// and LastNTurns both fork a committed prefix; LastNTurns truncates before the
// matching user-message boundary.
func forkSnapshotForMode(mode ForkMode) core.ForkSnapshot {
	if !mode.FullHistory && mode.LastNTurns > 0 {
		return core.ForkBeforeNthUserMessage(mode.LastNTurns)
	}
	return core.ForkInterrupted()
}

// threadIDKey returns a stable sort key for an optional thread id.
func threadIDKey(id *protocol.ThreadID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func cloneUint64(p *uint64) *uint64 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// ensureExecutionCapacity mirrors the upstream
// `ensure_execution_capacity_for_op`: an op that starts a turn on an agent that
// is not already running must obtain an execution slot from the configured
// limiter; when the slot is granted a watcher releases it once the agent's
// status leaves Running (the upstream guard dropping at turn end). Ops that do
// not start a turn, agents mid-turn, and Controls without a limiter pass through.
func (c *Control) ensureExecutionCapacity(thread *core.CodexThread, agentID protocol.ThreadID, op protocol.Op) error {
	if c.executionLimiter == nil || !opStartsTurn(op) {
		return nil
	}
	if thread.AgentStatus().Kind == protocol.AgentStatusRunning {
		return nil
	}
	if err := c.executionLimiter.TryAcquire(agentID); err != nil {
		return fmt.Errorf("multiagent: start turn for agent %s: %w", agentID, err)
	}
	current, ch, unsubscribe := thread.SubscribeAgentStatus()
	go c.releaseWhenIdle(agentID, current, ch, unsubscribe)
	return nil
}

// releaseWhenIdle returns the agent's execution slot once its status is no
// longer Running (or the status stream closes). It tolerates the brief window
// in which the submitted op has not yet flipped the status to Running by
// waiting for the first Running→non-Running transition, or for the stream to
// close, so a slot is never held past the turn.
func (c *Control) releaseWhenIdle(agentID protocol.ThreadID, current protocol.AgentStatus, ch <-chan protocol.AgentStatus, unsubscribe func()) {
	defer unsubscribe()
	defer c.executionLimiter.Release(agentID)
	sawRunning := current.Kind == protocol.AgentStatusRunning
	for st := range ch {
		switch {
		case st.Kind == protocol.AgentStatusRunning:
			sawRunning = true
		case sawRunning:
			return
		case IsFinal(st):
			// The turn ended before we observed Running (very short turn or
			// rejected op): release now.
			return
		}
	}
}
