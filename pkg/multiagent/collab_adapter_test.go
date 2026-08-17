package multiagent

// Tests for the CollabAdapter: the core.CollabControl seam driven through a
// REAL Control + ThreadManager, verifying the production wiring end-to-end
// (spawn -> status/metadata/snapshot -> send_input -> close).

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/rollout"
	"github.com/sqlrush/codexgo/internal/threadstore"

	"github.com/sqlrush/codexgo/internal/core"
)

// TestCollabAdapterSpawnSendCloseRoundTrip drives the adapter against a real
// control plane backed by the mock model client.
func TestCollabAdapterSpawnSendCloseRoundTrip(t *testing.T) {
	ctrl, mgr, _ := setup(t)
	adapter := NewCollabAdapter(ctrl)
	ctx := context.Background()

	var _ core.CollabControl = adapter

	source := rollout.SessionSource{
		Kind: rollout.SessionSourceKindSubAgent,
		SubAgent: &rollout.SubAgentSource{
			Kind: rollout.SubAgentSourceKindThreadSpawn,
			ThreadSpawn: &rollout.ThreadSpawnSource{
				ParentThreadID: protocol.NewThreadID("root-1"),
				Depth:          1,
			},
		},
	}
	result, err := adapter.SpawnAgent(ctx, core.CollabSpawnRequest{
		Configuration: core.SessionConfiguration{},
		InitialOp:     userInput("hi"),
		Source:        source,
	})
	if err != nil {
		t.Fatalf("SpawnAgent: %v", err)
	}
	if result.ThreadID == (protocol.ThreadID{}) {
		t.Fatal("spawn returned a zero thread id")
	}
	if status := adapter.GetStatus(result.ThreadID); status.Kind == protocol.AgentStatusNotFound {
		t.Errorf("status = %v, want a live status", status.Kind)
	}
	if metadata := adapter.GetAgentMetadata(result.ThreadID); metadata == nil {
		t.Error("metadata should be tracked for the spawned agent")
	}
	if snapshot := adapter.AgentConfigSnapshot(result.ThreadID); snapshot == nil {
		t.Error("config snapshot should resolve for a live agent")
	}

	if _, err := adapter.SendInput(ctx, result.ThreadID, userInput("more")); err != nil {
		t.Fatalf("SendInput: %v", err)
	}

	// Let the child's turns finish before closing so shutdown does not race the
	// core turn loop (the control_test pattern).
	drainToComplete(t, mgr, result.ThreadID)

	if err := adapter.CloseAgent(ctx, result.ThreadID); err != nil {
		t.Fatalf("CloseAgent: %v", err)
	}
}

// TestCollabAdapterRejectsForkWithoutParentRolloutPath documents the remaining
// limitation: when the host has no rollout persistence for the parent (nil/empty
// ParentRolloutPath) a full-history fork cannot be served and is rejected with a
// model-facing error (DEVIATIONS).
func TestCollabAdapterRejectsForkWithoutParentRolloutPath(t *testing.T) {
	ctrl, _, _ := setup(t)
	adapter := NewCollabAdapter(ctrl)

	_, err := adapter.SpawnAgent(context.Background(), core.CollabSpawnRequest{
		Configuration: core.SessionConfiguration{},
		InitialOp:     userInput("fork"),
		Source:        rollout.NewCliSource(),
		ForkContext:   true,
		// ParentRolloutPath omitted: the host has no rollout persistence.
	})
	if err == nil {
		t.Fatal("fork_context spawn without a parent rollout path should be rejected")
	}
}

// TestCollabAdapterForkContextForksParentHistory drives a full-history fork end
// to end: a parent thread's stored rollout is seeded under a known path, the
// adapter forks from it, and the child's initial history mirrors the parent's
// committed response items (the Rust spawn fork path).
func TestCollabAdapterForkContextForksParentHistory(t *testing.T) {
	ctx := context.Background()
	store := threadstore.NewInMemoryThreadStore()
	engine := newEngineWithStore(t, store)
	ctrl, _ := newControl(t, engine)
	adapter := NewCollabAdapter(ctrl)

	// Seed a parent thread with committed history under a known rollout path so
	// the fork can read it back, mirroring a parent session that has persisted
	// its rollout. The in-memory store maps a rollout path to a thread id via
	// ResumeThread; CreateThread + AppendItems give that thread its history.
	parentID := protocol.NewThreadID("fork-parent")
	parentPath := "/rollouts/fork-parent.jsonl"
	parentItems := []rollout.RolloutItem{
		responseUserMsg("parent question"),
		responseAssistantMsg("parent answer"),
	}
	seedStoredThread(t, store, parentID, parentPath, parentItems)

	// Fork-spawn a child from the parent's rollout path.
	childPath := mustPath(t, "/root/forked_child")
	source := threadSpawnSource(parentID, 1, &childPath, nil, nil)
	result, err := adapter.SpawnAgent(ctx, core.CollabSpawnRequest{
		Configuration:     core.SessionConfiguration{},
		InitialOp:         userInput("continue from here"),
		Source:            source,
		ForkContext:       true,
		ParentRolloutPath: &parentPath,
	})
	if err != nil {
		t.Fatalf("fork SpawnAgent: %v", err)
	}

	// The child's initial history must contain the parent's committed response
	// items (a full fork of the parent thread's history).
	child, err := engine.GetThread(result.ThreadID)
	if err != nil {
		t.Fatalf("get forked child thread: %v", err)
	}
	hist := child.Codex().Session().HistoryItems()
	if !historyContainsText(hist, "parent question") || !historyContainsText(hist, "parent answer") {
		t.Fatalf("forked child history missing parent items; got %d items: %+v", len(hist), hist)
	}

	drainToComplete(t, engine, result.ThreadID)
	if err := adapter.CloseAgent(ctx, result.ThreadID); err != nil {
		t.Fatalf("CloseAgent: %v", err)
	}
}

// newEngineWithStore builds a real core.ThreadManager over the supplied store,
// mirroring newEngine but letting the caller seed the store for fork reads.
func newEngineWithStore(t *testing.T, store threadstore.ThreadStore) *core.ThreadManager {
	t.Helper()
	n := 0
	gen := func() protocol.ThreadID {
		id := protocol.NewThreadID(fmt.Sprintf("forked-child-%d", n))
		n++
		return id
	}
	mgr, err := core.NewThreadManager(core.ThreadManagerConfig{
		Store:          store,
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

// seedStoredThread records a thread with committed history into the in-memory
// store and maps a rollout path to it, so ReadThreadByRolloutPath returns the
// thread's history (the input a fork reads from).
func seedStoredThread(t *testing.T, store *threadstore.InMemoryThreadStore, id protocol.ThreadID, path string, items []rollout.RolloutItem) {
	t.Helper()
	ctx := context.Background()
	if err := store.CreateThread(ctx, threadstore.CreateThreadParams{ThreadID: id}); err != nil {
		t.Fatalf("seed CreateThread: %v", err)
	}
	if err := store.AppendItems(ctx, threadstore.AppendThreadItemsParams{ThreadID: id, Items: items}); err != nil {
		t.Fatalf("seed AppendItems: %v", err)
	}
	if err := store.ResumeThread(ctx, threadstore.ResumeThreadParams{ThreadID: id, RolloutPath: &path}); err != nil {
		t.Fatalf("seed ResumeThread (rollout path map): %v", err)
	}
}

// responseUserMsg / responseAssistantMsg build rollout response items for fork
// seeding.
func responseUserMsg(text string) rollout.RolloutItem {
	return rollout.NewResponseItem(protocol.ResponseItem{
		Type:    protocol.ResponseItemKindMessage,
		Role:    "user",
		Content: []protocol.ContentItem{{Type: protocol.ContentItemKindInputText, Text: text}},
	})
}

func responseAssistantMsg(text string) rollout.RolloutItem {
	return rollout.NewResponseItem(protocol.ResponseItem{
		Type:    protocol.ResponseItemKindMessage,
		Role:    "assistant",
		Content: []protocol.ContentItem{{Type: protocol.ContentItemKindOutputText, Text: text}},
	})
}

// historyContainsText reports whether any message item in hist carries text.
func historyContainsText(hist []protocol.ResponseItem, text string) bool {
	for _, it := range hist {
		for _, c := range it.Content {
			if c.Text == text {
				return true
			}
		}
	}
	return false
}
