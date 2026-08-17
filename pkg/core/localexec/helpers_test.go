package localexec

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sqlrush/codexgo/pkg/core"
	"github.com/sqlrush/codexgo/pkg/core/coretest"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// newTestSession spawns a session through coretest and returns it with its
// event stream (SessionConfigured already drained).
func newTestSession(t *testing.T) (*core.Session, <-chan protocol.Event) {
	t.Helper()
	f := coretest.NewSession(t)
	return f.Session, f.Events
}

func recvEvent(t *testing.T, events <-chan protocol.Event) protocol.Event {
	t.Helper()
	return coretest.RecvEvent(t, events)
}

func newTestTurn(cwd string) *core.TurnContext { return coretest.NewTurn(cwd) }

// mockExecService records the request it receives and returns canned results.
type mockExecService struct {
	res    ExecResult
	err    error
	gotReq ExecRequest
}

func (m *mockExecService) Run(_ context.Context, req ExecRequest) (ExecResult, error) {
	m.gotReq = req
	return m.res, m.err
}

func mustMarshal(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(raw)
}
