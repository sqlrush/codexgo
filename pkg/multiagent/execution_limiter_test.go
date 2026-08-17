package multiagent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/pkg/agentgraph"
	"github.com/sqlrush/codexgo/pkg/core"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

func TestCountingExecutionLimiter(t *testing.T) {
	l := NewCountingExecutionLimiter(2)
	a, b, c := protocol.NewThreadID("a"), protocol.NewThreadID("b"), protocol.NewThreadID("c")
	if err := l.TryAcquire(a); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if err := l.TryAcquire(a); err != nil {
		t.Fatalf("re-acquire for the same agent must be a no-op: %v", err)
	}
	if err := l.TryAcquire(b); err != nil {
		t.Fatalf("second acquire: %v", err)
	}
	err := l.TryAcquire(c)
	if !IsAgentLimitReached(err) {
		t.Fatalf("third acquire = %v, want ErrAgentLimitReached", err)
	}
	if got := l.Active(); got != 2 {
		t.Fatalf("active = %d, want 2", got)
	}
	l.Release(a)
	if err := l.TryAcquire(c); err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	l.Release(protocol.NewThreadID("unknown")) // ignored
	unlimited := NewCountingExecutionLimiter(0)
	for i := 0; i < 50; i++ {
		if err := unlimited.TryAcquire(protocol.NewThreadID(string(rune('a' + i)))); err != nil {
			t.Fatalf("unlimited limiter rejected: %v", err)
		}
	}
}

func TestOpStartsTurn(t *testing.T) {
	if !opStartsTurn(userInput("x")) {
		t.Fatal("user input starts a turn")
	}
	if opStartsTurn(protocol.Op{Type: protocol.OpInterrupt}) {
		t.Fatal("interrupt does not start a turn")
	}
	if opStartsTurn(protocol.Op{Type: protocol.OpInterAgentCommunication, Communication: &protocol.InterAgentCommunication{}}) {
		t.Fatal("inter-agent communication without trigger_turn does not start a turn")
	}
	if !opStartsTurn(protocol.Op{Type: protocol.OpInterAgentCommunication, Communication: &protocol.InterAgentCommunication{TriggerTurn: true}}) {
		t.Fatal("inter-agent communication with trigger_turn starts a turn")
	}
}

// recordingLimiter records acquire/release calls and can be told to refuse.
type recordingLimiter struct {
	mu       sync.Mutex
	refuse   bool
	acquired []string
	released []string
}

func (r *recordingLimiter) TryAcquire(id protocol.ThreadID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.refuse {
		return &ErrAgentLimitReached{MaxThreads: 1}
	}
	r.acquired = append(r.acquired, id.String())
	return nil
}

func (r *recordingLimiter) Release(id protocol.ThreadID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.released = append(r.released, id.String())
}

func (r *recordingLimiter) counts() (int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.acquired), len(r.released)
}

// TestControlExecutionLimiterGuardsTurnStarts asserts the Control asks the
// limiter for a slot when an op starts a turn on an idle sub-agent, releases it
// once the turn ends, and surfaces ErrAgentLimitReached when refused.
func TestControlExecutionLimiterGuardsTurnStarts(t *testing.T) {
	ctx := context.Background()
	engine := newEngine(t)
	limiter := &recordingLimiter{}
	ctrl, err := NewControl(Config{
		Engine:           engine,
		Graph:            agentgraph.NewInMemoryAgentGraphStore(),
		SessionID:        protocol.NewSessionID("session-1"),
		NicknamePicker:   firstPicker,
		ExecutionLimiter: limiter,
	})
	if err != nil {
		t.Fatalf("NewControl: %v", err)
	}
	parent := protocol.NewThreadID("parent")
	path := mustPath(t, "/root/worker")

	// The spawn's initial user input starts the first turn → one acquire.
	live, err := ctrl.SpawnAgent(ctx, core.SessionConfiguration{}, userInput("first"), threadSpawnSource(parent, 1, &path, nil, nil), SpawnOptions{})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	drainToComplete(t, engine, live.ThreadID)
	waitFor(t, func() bool { a, r := limiter.counts(); return a == 1 && r == 1 })

	// An op that does not start a turn never touches the limiter.
	if _, err := ctrl.SendInput(ctx, live.ThreadID, protocol.Op{Type: protocol.OpInterrupt}); err != nil {
		t.Fatalf("send interrupt: %v", err)
	}
	if a, _ := limiter.counts(); a != 1 {
		t.Fatalf("interrupt must not acquire, acquires = %d", a)
	}

	// A refused slot fails the turn-starting op with ErrAgentLimitReached.
	limiter.mu.Lock()
	limiter.refuse = true
	limiter.mu.Unlock()
	if _, err := ctrl.SendInput(ctx, live.ThreadID, userInput("second")); !IsAgentLimitReached(err) {
		t.Fatalf("send input under a full limiter = %v, want ErrAgentLimitReached", err)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met within 5s")
}
