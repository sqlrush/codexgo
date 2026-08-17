package local

import (
	"context"
	"errors"
	"testing"

	"github.com/sqlrush/codexgo/internal/agentgraph"
	"github.com/sqlrush/codexgo/internal/agentgraph/agentgraphtest"
	"github.com/sqlrush/codexgo/internal/state"
)

func TestLocalSQLiteStoreConformance(t *testing.T) {
	agentgraphtest.RunSuite(t, func(t *testing.T) (agentgraph.AgentGraphStore, func()) {
		ctx := context.Background()
		rt, err := state.InitRuntime(ctx, t.TempDir(), "test-provider")
		if err != nil {
			t.Fatalf("init state runtime: %v", err)
		}
		store, err := NewLocalAgentGraphStore(ctx, rt)
		if err != nil {
			_ = rt.Close()
			t.Fatalf("new local store: %v", err)
		}
		return store, func() {
			_ = store.Close()
			_ = rt.Close()
		}
	})
}

func TestNewLocalAgentGraphStoreRejectsNilRuntime(t *testing.T) {
	_, err := NewLocalAgentGraphStore(context.Background(), nil)
	if err == nil {
		t.Fatalf("expected error for nil runtime")
	}
	var storeErr *agentgraph.Error
	if !errors.As(err, &storeErr) || storeErr.Kind != agentgraph.ErrorKindInvalidRequest {
		t.Fatalf("expected invalid request error, got %v", err)
	}
}
