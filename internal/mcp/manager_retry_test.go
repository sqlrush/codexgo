package mcp

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/sqlrush/codexgo/internal/config"
)

// flakyFactory fails the first failTimes NewTransport calls per server, then
// succeeds — modeling a transient startup failure.
type flakyFactory struct {
	mu        sync.Mutex
	failTimes int
	calls     map[string]int
	respond   responder
}

func (f *flakyFactory) NewTransport(_ context.Context, serverName string, _ config.McpServerConfig, _ string) (Transport, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[serverName]++
	if f.calls[serverName] <= f.failTimes {
		return nil, errors.New("transient transport failure")
	}
	return newFakeTransport(f.respond), nil
}

// TestStartServerRetriesTransientFailure covers spec 49 need 4 step 2: a server
// that fails its first attempt is retried and eventually becomes ready.
func TestStartServerRetriesTransientFailure(t *testing.T) {
	t.Parallel()
	factory := &flakyFactory{failTimes: 2, respond: scriptedServer()} // fail twice, succeed on 3rd
	cfg := map[string]config.McpServerConfig{"alpha": stdioServerConfig()}

	mgr, results, err := NewManager(context.Background(), cfg, ManagerOptions{Factory: factory})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown()
	if len(results) != 1 || results[0].Status != StartupReady {
		t.Fatalf("expected ready after retries, got %+v", results)
	}
	if factory.calls["alpha"] != 3 {
		t.Fatalf("attempts = %d, want 3 (2 fail + 1 success)", factory.calls["alpha"])
	}
}

// TestStartServerGivesUpAfterMaxAttempts: a permanently-failing server exhausts
// the bounded retries and is reported failed (does not stall).
func TestStartServerGivesUpAfterMaxAttempts(t *testing.T) {
	t.Parallel()
	factory := &flakyFactory{failTimes: 100, respond: scriptedServer()}
	cfg := map[string]config.McpServerConfig{"alpha": stdioServerConfig()}

	_, results, err := NewManager(context.Background(), cfg, ManagerOptions{Factory: factory})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if len(results) != 1 || results[0].Status != StartupFailed {
		t.Fatalf("expected failed after max attempts, got %+v", results)
	}
	if factory.calls["alpha"] != maxStartupAttempts {
		t.Fatalf("attempts = %d, want %d", factory.calls["alpha"], maxStartupAttempts)
	}
}

// TestStartupBackoffBounded: backoff never exceeds the cap.
func TestStartupBackoffBounded(t *testing.T) {
	t.Parallel()
	for attempt := 1; attempt <= 10; attempt++ {
		if d := startupBackoff(attempt); d > startupRetryCap {
			t.Fatalf("backoff(%d) = %v exceeds cap %v", attempt, d, startupRetryCap)
		}
	}
}
