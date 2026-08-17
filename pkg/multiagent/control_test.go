package multiagent

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/pkg/agentgraph"
	"github.com/sqlrush/codexgo/pkg/api"
	"github.com/sqlrush/codexgo/pkg/core"
	"github.com/sqlrush/codexgo/pkg/protocol"
	"github.com/sqlrush/codexgo/pkg/rollout"
	"github.com/sqlrush/codexgo/pkg/threadstore"
)

// completingTurn returns a single mock turn that completes cleanly: created,
// final assistant message, completed(end_turn=true). It lets a spawned child run
// its initial turn to completion so shutdown does not race with an in-flight turn
// in the core engine.
func completingTurn() core.MockTurn {
	endTurn := true
	mid := "m1"
	return core.MockTurn{Events: []api.ResponseEvent{
		{Kind: api.ResponseEventCreated},
		{Kind: api.ResponseEventOutputItemDone, Item: &protocol.ResponseItem{
			Type:      protocol.ResponseItemKindMessage,
			Role:      "assistant",
			MessageID: &mid,
			Content:   []protocol.ContentItem{{Type: protocol.ContentItemKindOutputText, Text: "ok"}},
		}},
		{Kind: api.ResponseEventCompleted, EndTurn: &endTurn},
	}}
}

// drainToComplete reads events from a thread until it reaches a final agent
// status, so its turn goroutine has finished before the test shuts it down. This
// avoids a pre-existing core race between turn-event emission and channel close.
func drainToComplete(t *testing.T, engine *core.ThreadManager, id protocol.ThreadID) {
	t.Helper()
	thread, err := engine.GetThread(id)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		ev, err := thread.NextEvent(ctx)
		if err != nil {
			return
		}
		if status, ok := StatusFromEvent(ev.Msg); ok && IsFinal(status) {
			return
		}
	}
}

// newEngine builds a real core.ThreadManager backed by an in-memory store and
// mock model client, so the multiagent control plane drives genuine threads.
func newEngine(t *testing.T) *core.ThreadManager {
	t.Helper()
	n := 0
	gen := func() protocol.ThreadID {
		id := protocol.NewThreadID(fmt.Sprintf("child-%d", n))
		n++
		return id
	}
	mgr, err := core.NewThreadManager(core.ThreadManagerConfig{
		Store:          threadstore.NewInMemoryThreadStore(),
		NewThreadID:    gen,
		SessionSource:  rollout.NewCliSource(),
		InstallationID: "install-test",
		Originator:     "codex_go_test",
		CliVersion:     "0.0.0",
		Now:            func() time.Time { return time.Unix(0, 0).UTC() },
		ServicesFactory: func(_ context.Context, _ protocol.ThreadID, _ core.SessionConfiguration) (core.SessionServices, error) {
			router, rerr := core.NewDefaultToolRouter()
			if rerr != nil {
				return core.SessionServices{}, rerr
			}
			// Each child gets several completing turns so its initial op (and any
			// follow-up SendInput) runs to completion rather than erroring.
			turns := []core.MockTurn{completingTurn(), completingTurn(), completingTurn()}
			return core.SessionServices{
				ModelClient: core.NewMockModelClient("gpt-test", nil, turns...),
				ToolRouter:  router,
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewThreadManager: %v", err)
	}
	return mgr
}

func newControl(t *testing.T, engine ThreadEngine) (*Control, agentgraph.AgentGraphStore) {
	t.Helper()
	graph := agentgraph.NewInMemoryAgentGraphStore()
	ctrl, err := NewControl(Config{
		Engine:         engine,
		Graph:          graph,
		SessionID:      protocol.NewSessionID("session-1"),
		NicknamePicker: firstPicker,
	})
	if err != nil {
		t.Fatalf("NewControl: %v", err)
	}
	return ctrl, graph
}

// setup builds a control plane backed by a fresh real engine, returning all
// three so tests can drain child events to avoid the core shutdown race.
func setup(t *testing.T) (*Control, *core.ThreadManager, agentgraph.AgentGraphStore) {
	t.Helper()
	engine := newEngine(t)
	ctrl, graph := newControl(t, engine)
	return ctrl, engine, graph
}

func userInput(text string) protocol.Op {
	return protocol.Op{Type: protocol.OpUserInput, Items: []protocol.UserInput{
		{Type: protocol.UserInputKindText, Text: text},
	}}
}

func TestNewControlValidation(t *testing.T) {
	graph := agentgraph.NewInMemoryAgentGraphStore()
	if _, err := NewControl(Config{Graph: graph}); err == nil {
		t.Fatalf("expected error when engine missing")
	}
	if _, err := NewControl(Config{Engine: newEngine(t)}); err == nil {
		t.Fatalf("expected error when graph missing")
	}
}

func TestSpawnAgentThreadSpawnRegistersAndPersistsEdge(t *testing.T) {
	ctx := context.Background()
	engine := newEngine(t)
	ctrl, graph := newControl(t, engine)

	parent := protocol.NewThreadID("parent-root")
	ctrl.RegisterSessionRoot(parent, rollout.NewCliSource())

	childPath := mustPath(t, "/root/researcher")
	role := "researcher"
	source := threadSpawnSource(parent, 1, &childPath, nil, &role)

	live, err := ctrl.SpawnAgent(ctx, core.SessionConfiguration{}, userInput("investigate"), source, SpawnOptions{})
	if err != nil {
		t.Fatalf("SpawnAgent: %v", err)
	}

	// Registry committed metadata with the reserved nickname (firstPicker => Euclid).
	if live.Metadata.AgentNickname == nil || *live.Metadata.AgentNickname != "Euclid" {
		t.Fatalf("nickname = %v, want Euclid", live.Metadata.AgentNickname)
	}
	if live.Metadata.AgentPath == nil || live.Metadata.AgentPath.String() != "/root/researcher" {
		t.Fatalf("agent path = %v, want /root/researcher", live.Metadata.AgentPath)
	}
	if live.Metadata.AgentID == nil || *live.Metadata.AgentID != live.ThreadID {
		t.Fatalf("metadata agent id mismatch: %v vs %v", live.Metadata.AgentID, live.ThreadID)
	}

	// The spawn edge is persisted as open under the parent.
	children, err := graph.ListThreadSpawnChildren(ctx, parent, statusPtr(agentgraph.ThreadSpawnEdgeStatusOpen))
	if err != nil {
		t.Fatalf("list children: %v", err)
	}
	if len(children) != 1 || children[0] != live.ThreadID {
		t.Fatalf("open children = %v, want [%v]", children, live.ThreadID)
	}

	// The child resolves by its registered agent path.
	resolved, err := ctrl.ResolveAgentReference(rollout.NewCliSource(), "/root/researcher")
	if err != nil {
		t.Fatalf("resolve reference: %v", err)
	}
	if resolved != live.ThreadID {
		t.Fatalf("resolved = %v, want %v", resolved, live.ThreadID)
	}
}

func statusPtr(s agentgraph.ThreadSpawnEdgeStatus) *agentgraph.ThreadSpawnEdgeStatus { return &s }

func TestSpawnAgentRespectsThreadLimit(t *testing.T) {
	ctx := context.Background()
	engine := newEngine(t)
	graph := agentgraph.NewInMemoryAgentGraphStore()
	max := uint64(1)
	ctrl, err := NewControl(Config{
		Engine:         engine,
		Graph:          graph,
		MaxThreads:     &max,
		NicknamePicker: firstPicker,
	})
	if err != nil {
		t.Fatalf("NewControl: %v", err)
	}

	parent := protocol.NewThreadID("parent")
	p1 := mustPath(t, "/root/a")
	if _, err := ctrl.SpawnAgent(ctx, core.SessionConfiguration{}, userInput("a"), threadSpawnSource(parent, 1, &p1, nil, nil), SpawnOptions{}); err != nil {
		t.Fatalf("first spawn: %v", err)
	}

	p2 := mustPath(t, "/root/b")
	_, err = ctrl.SpawnAgent(ctx, core.SessionConfiguration{}, userInput("b"), threadSpawnSource(parent, 1, &p2, nil, nil), SpawnOptions{})
	var limitErr *AgentLimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("second spawn error = %v, want AgentLimitError", err)
	}

	// A failed spawn must not leave a reserved path behind.
	if id := ctrl.Registry().AgentIDForPath(p2); id != nil {
		t.Fatalf("path /root/b should not be reserved after a failed spawn, got %v", id)
	}
}

func TestSpawnAgentDuplicatePathFails(t *testing.T) {
	ctx := context.Background()
	ctrl, _ := newControl(t, newEngine(t))
	parent := protocol.NewThreadID("parent")
	path := mustPath(t, "/root/dup")

	if _, err := ctrl.SpawnAgent(ctx, core.SessionConfiguration{}, userInput("1"), threadSpawnSource(parent, 1, &path, nil, nil), SpawnOptions{}); err != nil {
		t.Fatalf("first spawn: %v", err)
	}
	_, err := ctrl.SpawnAgent(ctx, core.SessionConfiguration{}, userInput("2"), threadSpawnSource(parent, 1, &path, nil, nil), SpawnOptions{})
	if !errors.Is(err, ErrUnsupportedOperation) {
		t.Fatalf("duplicate path spawn error = %v, want ErrUnsupportedOperation", err)
	}
}

func TestSendInputUpdatesLastTaskMessage(t *testing.T) {
	ctx := context.Background()
	ctrl, engine, _ := setup(t)
	parent := protocol.NewThreadID("parent")
	path := mustPath(t, "/root/talker")

	live, err := ctrl.SpawnAgent(ctx, core.SessionConfiguration{}, userInput("first"), threadSpawnSource(parent, 1, &path, nil, nil), SpawnOptions{})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	// Spawn already routed the initial op, so the last task message reflects it.
	md := ctrl.GetAgentMetadata(live.ThreadID)
	if md == nil || md.LastTaskMessage == nil || *md.LastTaskMessage != "first" {
		t.Fatalf("last task after spawn = %v, want \"first\"", md)
	}

	drainToComplete(t, engine, live.ThreadID)
	if _, err := ctrl.SendInput(ctx, live.ThreadID, userInput("second")); err != nil {
		t.Fatalf("send input: %v", err)
	}
	md = ctrl.GetAgentMetadata(live.ThreadID)
	if md == nil || md.LastTaskMessage == nil || *md.LastTaskMessage != "second" {
		t.Fatalf("last task after send = %v, want \"second\"", md)
	}
	drainToComplete(t, engine, live.ThreadID)
}

func TestSendInterAgentCommunicationUpdatesLastTaskMessage(t *testing.T) {
	ctx := context.Background()
	ctrl, engine, _ := setup(t)
	parent := protocol.NewThreadID("parent")
	path := mustPath(t, "/root/peer")

	live, err := ctrl.SpawnAgent(ctx, core.SessionConfiguration{}, userInput("hi"), threadSpawnSource(parent, 1, &path, nil, nil), SpawnOptions{})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	drainToComplete(t, engine, live.ThreadID)

	comm := protocol.InterAgentCommunication{
		Author:    mustPath(t, "/root"),
		Recipient: path,
		Content:   "please continue",
	}
	if _, err := ctrl.SendInterAgentCommunication(ctx, live.ThreadID, comm); err != nil {
		t.Fatalf("send inter-agent communication: %v", err)
	}
	md := ctrl.GetAgentMetadata(live.ThreadID)
	if md == nil || md.LastTaskMessage == nil || *md.LastTaskMessage != "please continue" {
		t.Fatalf("last task = %v, want \"please continue\"", md)
	}
}

func TestSendInputUnknownAgentFails(t *testing.T) {
	ctx := context.Background()
	ctrl, _ := newControl(t, newEngine(t))
	_, err := ctrl.SendInput(ctx, protocol.NewThreadID("ghost"), userInput("hello"))
	if err == nil {
		t.Fatalf("expected error sending to unknown agent")
	}
}

func TestGetStatusUnknownReturnsNotFound(t *testing.T) {
	ctrl, _ := newControl(t, newEngine(t))
	status := ctrl.GetStatus(protocol.NewThreadID("nope"))
	if status.Kind != protocol.AgentStatusNotFound {
		t.Fatalf("status = %q, want not_found", status.Kind)
	}
}

func TestResolveAgentReferenceNotFound(t *testing.T) {
	ctrl, _ := newControl(t, newEngine(t))
	_, err := ctrl.ResolveAgentReference(rollout.NewCliSource(), "/root/missing")
	if !errors.Is(err, ErrUnsupportedOperation) {
		t.Fatalf("resolve error = %v, want ErrUnsupportedOperation", err)
	}
}

func TestListAgentsOrdersRootThenChildren(t *testing.T) {
	ctx := context.Background()
	engine := newEngine(t)
	ctrl, _ := newControl(t, engine)

	// Register the root thread as a real spawned thread so ListAgents can read it.
	rootThread, err := engine.StartThreadWithOptions(ctx, core.StartThreadOptions{
		Configuration:  core.SessionConfiguration{},
		InitialHistory: rollout.NewInitialHistory(),
	})
	if err != nil {
		t.Fatalf("start root thread: %v", err)
	}
	ctrl.Registry().RegisterRootThread(rootThread.ThreadID)

	parent := rootThread.ThreadID
	pathB := mustPath(t, "/root/bravo")
	pathA := mustPath(t, "/root/alpha")
	if _, err := ctrl.SpawnAgent(ctx, core.SessionConfiguration{}, userInput("b"), threadSpawnSource(parent, 1, &pathB, nil, nil), SpawnOptions{}); err != nil {
		t.Fatalf("spawn bravo: %v", err)
	}
	if _, err := ctrl.SpawnAgent(ctx, core.SessionConfiguration{}, userInput("a"), threadSpawnSource(parent, 1, &pathA, nil, nil), SpawnOptions{}); err != nil {
		t.Fatalf("spawn alpha: %v", err)
	}

	agents, err := ctrl.ListAgents(rollout.NewCliSource(), nil)
	if err != nil {
		t.Fatalf("list agents: %v", err)
	}
	if len(agents) != 3 {
		t.Fatalf("agent count = %d, want 3 (root + 2 children)", len(agents))
	}
	want := []string{"/root", "/root/alpha", "/root/bravo"}
	for i, w := range want {
		if agents[i].AgentName != w {
			t.Fatalf("agent[%d] = %q, want %q (full=%v)", i, agents[i].AgentName, w, agents)
		}
	}
	if agents[0].LastTaskMessage == nil || *agents[0].LastTaskMessage != rootLastTaskMessage {
		t.Fatalf("root last task = %v, want %q", agents[0].LastTaskMessage, rootLastTaskMessage)
	}
}

func TestListAgentsFiltersByPrefix(t *testing.T) {
	ctx := context.Background()
	ctrl, _ := newControl(t, newEngine(t))
	parent := protocol.NewThreadID("parent")

	teamPath := mustPath(t, "/root/team")
	memberPath := mustPath(t, "/root/team/member")
	otherPath := mustPath(t, "/root/other")
	for _, p := range []protocol.AgentPath{teamPath, memberPath, otherPath} {
		pp := p
		if _, err := ctrl.SpawnAgent(ctx, core.SessionConfiguration{}, userInput("x"), threadSpawnSource(parent, 1, &pp, nil, nil), SpawnOptions{}); err != nil {
			t.Fatalf("spawn %s: %v", p.String(), err)
		}
	}

	prefix := "/root/team"
	agents, err := ctrl.ListAgents(rollout.NewCliSource(), &prefix)
	if err != nil {
		t.Fatalf("list agents: %v", err)
	}
	got := make([]string, 0, len(agents))
	for _, a := range agents {
		got = append(got, a.AgentName)
	}
	want := []string{"/root/team", "/root/team/member"}
	if len(got) != len(want) {
		t.Fatalf("filtered agents = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("filtered[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestCloseAgentClosesEdgeAndShutsDownTree(t *testing.T) {
	ctx := context.Background()
	ctrl, engine, graph := setup(t)

	root := protocol.NewThreadID("root")
	parentPath := mustPath(t, "/root/parent")
	parent, err := ctrl.SpawnAgent(ctx, core.SessionConfiguration{}, userInput("parent"), threadSpawnSource(root, 1, &parentPath, nil, nil), SpawnOptions{})
	if err != nil {
		t.Fatalf("spawn parent: %v", err)
	}

	childPath := mustPath(t, "/root/parent/child")
	child, err := ctrl.SpawnAgent(ctx, core.SessionConfiguration{}, userInput("child"), threadSpawnSource(parent.ThreadID, 2, &childPath, nil, nil), SpawnOptions{})
	if err != nil {
		t.Fatalf("spawn child: %v", err)
	}

	// Let both turns finish so shutdown does not race the core turn loop.
	drainToComplete(t, engine, parent.ThreadID)
	drainToComplete(t, engine, child.ThreadID)

	if err := ctrl.CloseAgent(ctx, parent.ThreadID); err != nil {
		t.Fatalf("close agent: %v", err)
	}

	// The parent edge is now closed.
	openChildren, err := graph.ListThreadSpawnChildren(ctx, root, statusPtr(agentgraph.ThreadSpawnEdgeStatusOpen))
	if err != nil {
		t.Fatalf("list open children: %v", err)
	}
	if len(openChildren) != 0 {
		t.Fatalf("open children after close = %v, want none", openChildren)
	}

	// Both parent and child threads are removed from the engine.
	if _, err := engine.GetThread(parent.ThreadID); !errors.Is(err, core.ErrThreadNotFound) {
		t.Fatalf("parent thread should be gone, err = %v", err)
	}
	if _, err := engine.GetThread(child.ThreadID); !errors.Is(err, core.ErrThreadNotFound) {
		t.Fatalf("child thread should be gone, err = %v", err)
	}

	// And the registry no longer tracks them.
	if md := ctrl.GetAgentMetadata(parent.ThreadID); md != nil {
		t.Fatalf("parent metadata should be released, got %v", md)
	}
}

func TestShutdownLiveAgentRemovesFromRegistry(t *testing.T) {
	ctx := context.Background()
	ctrl, engine, _ := setup(t)
	parent := protocol.NewThreadID("parent")
	path := mustPath(t, "/root/solo")

	live, err := ctrl.SpawnAgent(ctx, core.SessionConfiguration{}, userInput("hi"), threadSpawnSource(parent, 1, &path, nil, nil), SpawnOptions{})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	drainToComplete(t, engine, live.ThreadID)
	if err := ctrl.ShutdownLiveAgent(ctx, live.ThreadID); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if md := ctrl.GetAgentMetadata(live.ThreadID); md != nil {
		t.Fatalf("metadata should be released after shutdown, got %v", md)
	}
	if _, err := engine.GetThread(live.ThreadID); !errors.Is(err, core.ErrThreadNotFound) {
		t.Fatalf("thread should be removed, err = %v", err)
	}
}

func TestListLiveSubtreeThreadIDs(t *testing.T) {
	ctx := context.Background()
	ctrl, _ := newControl(t, newEngine(t))
	root := protocol.NewThreadID("root")

	parentPath := mustPath(t, "/root/p")
	parent, err := ctrl.SpawnAgent(ctx, core.SessionConfiguration{}, userInput("p"), threadSpawnSource(root, 1, &parentPath, nil, nil), SpawnOptions{})
	if err != nil {
		t.Fatalf("spawn parent: %v", err)
	}
	childPath := mustPath(t, "/root/p/c")
	child, err := ctrl.SpawnAgent(ctx, core.SessionConfiguration{}, userInput("c"), threadSpawnSource(parent.ThreadID, 2, &childPath, nil, nil), SpawnOptions{})
	if err != nil {
		t.Fatalf("spawn child: %v", err)
	}

	ids, err := ctrl.ListLiveSubtreeThreadIDs(ctx, parent.ThreadID)
	if err != nil {
		t.Fatalf("list subtree: %v", err)
	}
	if len(ids) != 2 || ids[0] != parent.ThreadID || ids[1] != child.ThreadID {
		t.Fatalf("subtree = %v, want [%v %v]", ids, parent.ThreadID, child.ThreadID)
	}
}

func TestSpawnNonThreadSpawnSourceCommitsMetadataWithoutEdge(t *testing.T) {
	ctx := context.Background()
	ctrl, engine, graph := setup(t)

	live, err := ctrl.SpawnAgent(ctx, core.SessionConfiguration{}, userInput("hello"), rollout.NewExecSource(), SpawnOptions{})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	drainToComplete(t, engine, live.ThreadID)

	// Mirroring Rust: metadata is committed (id only, no path/nickname/role) even
	// for a non thread-spawn source.
	md := ctrl.GetAgentMetadata(live.ThreadID)
	if md == nil {
		t.Fatalf("expected committed metadata for non thread-spawn source")
	}
	if md.AgentID == nil || *md.AgentID != live.ThreadID {
		t.Fatalf("metadata agent id = %v, want %v", md.AgentID, live.ThreadID)
	}
	if md.AgentPath != nil || md.AgentNickname != nil || md.AgentRole != nil {
		t.Fatalf("non thread-spawn metadata should carry no path/nickname/role: %+v", md)
	}

	// No thread-spawn parent => no persisted spawn edge.
	descendants, err := graph.ListThreadSpawnDescendants(ctx, live.ThreadID, nil)
	if err != nil {
		t.Fatalf("list descendants: %v", err)
	}
	if len(descendants) != 0 {
		t.Fatalf("descendants = %v, want none", descendants)
	}
}

func TestSpawnForkRequiresParentRolloutPath(t *testing.T) {
	ctx := context.Background()
	ctrl, _ := newControl(t, newEngine(t))
	parent := protocol.NewThreadID("parent")
	path := mustPath(t, "/root/forked")

	_, err := ctrl.SpawnAgent(ctx, core.SessionConfiguration{}, userInput("x"),
		threadSpawnSource(parent, 1, &path, nil, nil),
		SpawnOptions{Fork: &ForkMode{FullHistory: true}})
	if !errors.Is(err, ErrFatal) {
		t.Fatalf("fork without rollout path error = %v, want ErrFatal", err)
	}
	// The failed spawn must not leave the path reserved.
	if id := ctrl.Registry().AgentIDForPath(path); id != nil {
		t.Fatalf("path should be free after failed fork, got %v", id)
	}
}
