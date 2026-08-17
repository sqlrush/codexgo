package agentgraph_test

import (
	"testing"

	"github.com/sqlrush/codexgo/pkg/agentgraph"
	"github.com/sqlrush/codexgo/pkg/agentgraph/agentgraphtest"
)

func TestThreadSpawnEdgeStatusWireValue(t *testing.T) {
	if agentgraph.ThreadSpawnEdgeStatusOpen.String() != "open" {
		t.Fatalf("open status = %q, want open", agentgraph.ThreadSpawnEdgeStatusOpen)
	}
	if agentgraph.ThreadSpawnEdgeStatusClosed.String() != "closed" {
		t.Fatalf("closed status = %q, want closed", agentgraph.ThreadSpawnEdgeStatusClosed)
	}
}

func TestInMemoryStoreConformance(t *testing.T) {
	agentgraphtest.RunSuite(t, func(t *testing.T) (agentgraph.AgentGraphStore, func()) {
		return agentgraph.NewInMemoryAgentGraphStore(), func() {}
	})
}
