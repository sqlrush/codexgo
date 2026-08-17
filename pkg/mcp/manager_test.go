package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/sqlrush/codexgo/pkg/config"
)

// stubFactory builds fake transports keyed by server name.
type stubFactory struct {
	servers map[string]responder
	failOn  map[string]error
}

func (f *stubFactory) NewTransport(_ context.Context, serverName string, _ config.McpServerConfig, _ string) (Transport, error) {
	if f.failOn != nil {
		if err, ok := f.failOn[serverName]; ok {
			return nil, err
		}
	}
	respond, ok := f.servers[serverName]
	if !ok {
		return nil, errors.New("stubFactory: no server " + serverName)
	}
	return newFakeTransport(respond), nil
}

func stdioServerConfig() config.McpServerConfig {
	return config.McpServerConfig{
		Enabled: true,
		Transport: config.McpServerTransportConfig{
			Kind:    config.McpTransportStdio,
			Command: "fake",
		},
	}
}

func TestManagerStartupAndDiscovery(t *testing.T) {
	t.Parallel()
	factory := &stubFactory{servers: map[string]responder{
		"alpha": scriptedServer(),
		"beta":  scriptedServer(),
	}}
	cfg := map[string]config.McpServerConfig{
		"alpha": stdioServerConfig(),
		"beta":  stdioServerConfig(),
	}

	mgr, results, err := NewManager(context.Background(), cfg, ManagerOptions{Factory: factory})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown()

	if !mgr.HasServers() {
		t.Fatal("expected servers")
	}
	if names := mgr.ServerNames(); len(names) != 2 || names[0] != "alpha" || names[1] != "beta" {
		t.Fatalf("ServerNames=%v", names)
	}
	for _, r := range results {
		if r.Status != StartupReady {
			t.Fatalf("server %q status=%v err=%v", r.ServerName, r.Status, r.Err)
		}
	}

	// Two tools per server, namespaced and sorted by (server, tool).
	all := mgr.ListAllTools()
	if len(all) != 4 {
		t.Fatalf("expected 4 namespaced tools, got %d", len(all))
	}
	if all[0].QualifiedName() != "mcp__alpha__echo" {
		t.Fatalf("first tool=%q", all[0].QualifiedName())
	}

	specs, err := mgr.ListAllToolSpecs(false)
	if err != nil {
		t.Fatalf("ListAllToolSpecs: %v", err)
	}
	if len(specs) != 4 {
		t.Fatalf("expected 4 specs, got %d", len(specs))
	}

	if info, ok := mgr.ServerInfo("alpha"); !ok || info.Name != "test-server" {
		t.Fatalf("ServerInfo=%+v ok=%v", info, ok)
	}
}

func TestManagerToolFiltering(t *testing.T) {
	t.Parallel()
	factory := &stubFactory{servers: map[string]responder{"alpha": scriptedServer()}}
	cfg := stdioServerConfig()
	// Only "echo" is enabled; "secret" must be filtered out.
	enabled := []string{"echo"}
	cfg.EnabledTools = &enabled

	mgr, _, err := NewManager(context.Background(), map[string]config.McpServerConfig{"alpha": cfg}, ManagerOptions{Factory: factory})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown()

	all := mgr.ListAllTools()
	if len(all) != 1 || all[0].Tool.Name != "echo" {
		t.Fatalf("expected only echo, got %+v", all)
	}

	// Calling a filtered-out tool must be rejected before hitting the wire.
	_, err = mgr.CallTool(context.Background(), "alpha", "secret", json.RawMessage(`{}`), nil)
	if err == nil {
		t.Fatal("expected error calling disabled tool")
	}
}

func TestManagerCallQualifiedTool(t *testing.T) {
	t.Parallel()
	factory := &stubFactory{servers: map[string]responder{"alpha": scriptedServer()}}
	mgr, _, err := NewManager(context.Background(), map[string]config.McpServerConfig{"alpha": stdioServerConfig()}, ManagerOptions{Factory: factory})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown()

	res, err := mgr.CallQualifiedTool(context.Background(), "mcp__alpha__echo", json.RawMessage(`{}`), nil)
	if err != nil {
		t.Fatalf("CallQualifiedTool: %v", err)
	}
	if len(res.Content) != 1 {
		t.Fatalf("content=%+v", res.Content)
	}

	if _, err := mgr.CallQualifiedTool(context.Background(), "not-an-mcp-name", nil, nil); err == nil {
		t.Fatal("expected error for malformed qualified name")
	}
}

func TestManagerReadResource(t *testing.T) {
	t.Parallel()
	factory := &stubFactory{servers: map[string]responder{"alpha": scriptedServer()}}
	mgr, _, err := NewManager(context.Background(), map[string]config.McpServerConfig{"alpha": stdioServerConfig()}, ManagerOptions{Factory: factory})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown()

	res, err := mgr.ReadResource(context.Background(), "alpha", "file:///x")
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(res.Contents) != 1 {
		t.Fatalf("contents=%+v", res.Contents)
	}
}

func TestManagerUnknownServer(t *testing.T) {
	t.Parallel()
	factory := &stubFactory{servers: map[string]responder{"alpha": scriptedServer()}}
	mgr, _, err := NewManager(context.Background(), map[string]config.McpServerConfig{"alpha": stdioServerConfig()}, ManagerOptions{Factory: factory})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown()

	if _, err := mgr.CallTool(context.Background(), "ghost", "echo", nil, nil); err == nil {
		t.Fatal("expected error for unknown server")
	}
	if _, err := mgr.ReadResource(context.Background(), "ghost", "x"); err == nil {
		t.Fatal("expected error for unknown server")
	}
}

func TestManagerSkipsDisabledAndInvalidServers(t *testing.T) {
	t.Parallel()
	factory := &stubFactory{servers: map[string]responder{"alpha": scriptedServer()}}

	disabled := stdioServerConfig()
	disabled.Enabled = false

	cfg := map[string]config.McpServerConfig{
		"alpha":     stdioServerConfig(),
		"disabled":  disabled,
		"bad name!": stdioServerConfig(), // invalid name -> reported failed
	}

	mgr, results, err := NewManager(context.Background(), cfg, ManagerOptions{Factory: factory})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown()

	if names := mgr.ServerNames(); len(names) != 1 || names[0] != "alpha" {
		t.Fatalf("expected only alpha connected, got %v", names)
	}

	// "disabled" is skipped entirely (not in results); "bad name!" is reported
	// as a failure.
	var sawBad, sawAlpha bool
	for _, r := range results {
		switch r.ServerName {
		case "bad name!":
			sawBad = true
			if r.Status != StartupFailed {
				t.Fatalf("bad name status=%v", r.Status)
			}
		case "alpha":
			sawAlpha = true
		case "disabled":
			t.Fatalf("disabled server should not appear in results")
		}
	}
	if !sawBad || !sawAlpha {
		t.Fatalf("results=%+v", results)
	}
}

func TestManagerStartupFailurePropagates(t *testing.T) {
	t.Parallel()
	factory := &stubFactory{
		servers: map[string]responder{"good": scriptedServer()},
		failOn:  map[string]error{"broken": errors.New("transport boom")},
	}
	cfg := map[string]config.McpServerConfig{
		"good":   stdioServerConfig(),
		"broken": stdioServerConfig(),
	}

	mgr, results, err := NewManager(context.Background(), cfg, ManagerOptions{Factory: factory})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Shutdown()

	if names := mgr.ServerNames(); len(names) != 1 || names[0] != "good" {
		t.Fatalf("expected only good, got %v", names)
	}

	var brokenResult *ServerStartupResult
	for i := range results {
		if results[i].ServerName == "broken" {
			brokenResult = &results[i]
		}
	}
	if brokenResult == nil || brokenResult.Status != StartupFailed || brokenResult.Err == nil {
		t.Fatalf("broken result=%+v", brokenResult)
	}
}

func TestManagerShutdownClearsClients(t *testing.T) {
	t.Parallel()
	factory := &stubFactory{servers: map[string]responder{"alpha": scriptedServer()}}
	mgr, _, err := NewManager(context.Background(), map[string]config.McpServerConfig{"alpha": stdioServerConfig()}, ManagerOptions{Factory: factory})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	mgr.Shutdown()
	if mgr.HasServers() {
		t.Fatal("expected no servers after shutdown")
	}
	// Shutdown is idempotent.
	mgr.Shutdown()
}

func TestToolFilterAllows(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		filter ToolFilter
		tool   string
		want   bool
	}{
		{name: "no constraints", filter: ToolFilter{}, tool: "x", want: true},
		{
			name:   "enabled allowlist hit",
			filter: ToolFilter{Enabled: map[string]struct{}{"x": {}}},
			tool:   "x", want: true,
		},
		{
			name:   "enabled allowlist miss",
			filter: ToolFilter{Enabled: map[string]struct{}{"x": {}}},
			tool:   "y", want: false,
		},
		{
			name:   "disabled wins",
			filter: ToolFilter{Disabled: map[string]struct{}{"x": {}}},
			tool:   "x", want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.filter.Allows(tc.tool); got != tc.want {
				t.Fatalf("Allows(%q)=%v want %v", tc.tool, got, tc.want)
			}
		})
	}
}

func TestToolFilterFromConfig(t *testing.T) {
	t.Parallel()
	enabled := []string{"a", "b"}
	disabled := []string{"b"}
	cfg := config.McpServerConfig{EnabledTools: &enabled, DisabledTools: &disabled}
	f := ToolFilterFromConfig(cfg)
	if !f.Allows("a") {
		t.Error("a should be allowed")
	}
	if f.Allows("b") {
		t.Error("b is disabled and should not be allowed")
	}
	if f.Allows("c") {
		t.Error("c is not in enabled allowlist")
	}
}
