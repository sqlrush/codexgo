package multiagent

// Tests for the CollabAdapter: the core.CollabControl seam driven through a
// REAL Control + ThreadManager, verifying the production wiring end-to-end
// (spawn -> status/metadata/snapshot -> send_input -> close).

import (
	"context"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/rollout"

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

// TestCollabAdapterRejectsForkContext documents the fork STUB: full-history
// forks need the parent rollout path plumbing (DEVIATIONS).
func TestCollabAdapterRejectsForkContext(t *testing.T) {
	ctrl, mgr, _ := setup(t)
	adapter := NewCollabAdapter(ctrl)

	_, err := adapter.SpawnAgent(context.Background(), core.CollabSpawnRequest{
		Configuration: core.SessionConfiguration{},
		InitialOp:     userInput("fork"),
		Source:        rollout.NewCliSource(),
		ForkContext:   true,
	})
	if err == nil {
		t.Fatal("fork_context spawn should be rejected until rollout plumbing lands")
	}
	_ = mgr
}
