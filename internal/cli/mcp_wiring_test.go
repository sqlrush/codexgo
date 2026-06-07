package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sqlrush/codexgo/internal/config"
)

// gaussdbPluginBin is the built gaussdb MCP server binary, relative to this
// package. The test launches it as a real configured MCP server to verify the
// end-to-end wiring: config -> NewManager -> stdio subprocess -> initialize ->
// tools/list -> lowered model-visible specs.
const gaussdbPluginBin = "../../plugins/codexgo-db-gaussdb/bin/codexgo-db-gaussdb"

func TestBuildMcpManagerNoServers(t *testing.T) {
	if mgr := buildMcpManager(context.Background(), "", nil); mgr != nil {
		t.Fatal("expected nil manager when no servers configured")
	}
	if mgr := buildMcpManager(context.Background(), "", map[string]config.McpServerConfig{}); mgr != nil {
		t.Fatal("expected nil manager for empty server map")
	}
}

func TestBuildMcpManagerLaunchesGaussdbPlugin(t *testing.T) {
	abs, err := filepath.Abs(gaussdbPluginBin)
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Skipf("gaussdb plugin binary not built (%s); run `make -C plugins/codexgo-db-gaussdb build`", abs)
	}

	var cfg config.McpServerConfig
	if err := json.Unmarshal([]byte(`{"command":`+strconvQuote(abs)+`,"args":[]}`), &cfg); err != nil {
		t.Fatalf("unmarshal server config: %v", err)
	}

	mgr := buildMcpManager(context.Background(), "", map[string]config.McpServerConfig{"gaussdb": cfg})
	if mgr == nil {
		t.Fatal("expected a manager after launching the gaussdb plugin")
	}
	defer mgr.Shutdown()

	// ListAllToolInfos is the path the assembly feeds into the tool router. Each
	// info keeps the raw model-visible CallableName while carrying the server
	// identity so the canonical "mcp__gaussdb__<tool>" routes the call back.
	infos := mgr.ListAllToolInfos()
	if len(infos) != 13 {
		t.Fatalf("expected 13 gaussdb tools, got %d", len(infos))
	}
	byName := map[string]string{} // raw callable name -> qualified dispatch name
	for _, info := range infos {
		if info.ServerName != "gaussdb" {
			t.Errorf("tool %q has server %q, want gaussdb", info.CallableName, info.ServerName)
		}
		byName[info.CallableName] = info.CanonicalToolName().String()
	}
	// Tool names match opendb's command names (no db_ prefix), incl. help.
	for _, raw := range []string{"connect", "health", "slowsql", "wdranalyze", "help"} {
		qn, ok := byName[raw]
		if !ok {
			t.Errorf("missing MCP tool %q", raw)
			continue
		}
		if want := "mcp__gaussdb__" + raw; qn != want {
			t.Errorf("tool %q dispatch name = %q, want %q", raw, qn, want)
		}
	}
}

// strconvQuote is strconv.Quote without importing strconv at the call site for a
// single use; keeps the JSON literal readable.
func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
