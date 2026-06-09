package appserver

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/tools"
)

type fakeMcpGateway struct {
	infos  []tools.McpToolInfo
	result protocol.CallToolResult
	err    error
	gotQN  string
}

func (f *fakeMcpGateway) ListAllToolInfos() []tools.McpToolInfo { return f.infos }

func (f *fakeMcpGateway) CallQualifiedTool(_ context.Context, qn string, _, _ json.RawMessage) (protocol.CallToolResult, error) {
	f.gotQN = qn
	return f.result, f.err
}

func strptr(s string) *string { return &s }

func TestHandleMcpListTools(t *testing.T) {
	desc := "Health check"
	gw := &fakeMcpGateway{infos: []tools.McpToolInfo{
		{
			ServerName:        "gaussdb",
			CallableName:      "health",
			CallableNamespace: "mcp__gaussdb__",
			Tool:              protocol.Tool{Name: "health", Description: &desc, InputSchema: json.RawMessage(`{"type":"object"}`)},
		},
		{
			ServerName:        "gaussdb",
			CallableName:      "connect",
			CallableNamespace: "mcp__gaussdb__",
			Tool:              protocol.Tool{Name: "connect"},
		},
	}}
	p := &Processor{assembly: &Assembly{McpGateway: gw}}

	out, rpcErr := p.handleMcpListTools(&appserverproto.McpListToolsParams{})
	if rpcErr != nil {
		t.Fatalf("rpc error: %v", rpcErr)
	}
	resp := out.(appserverproto.McpListToolsResponse)
	if len(resp.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(resp.Tools))
	}
	// Sorted by qualified name: connect before health.
	if resp.Tools[0].QualifiedName != "mcp__gaussdb__connect" {
		t.Errorf("tool[0] = %q, want mcp__gaussdb__connect", resp.Tools[0].QualifiedName)
	}
	if resp.Tools[1].QualifiedName != "mcp__gaussdb__health" {
		t.Errorf("tool[1] = %q, want mcp__gaussdb__health", resp.Tools[1].QualifiedName)
	}
	if resp.Tools[1].Server != "gaussdb" || resp.Tools[1].Tool != "health" || resp.Tools[1].Description != "Health check" {
		t.Errorf("tool[1] metadata wrong: %+v", resp.Tools[1])
	}
}

func TestHandleMcpListToolsNoGateway(t *testing.T) {
	p := &Processor{assembly: &Assembly{}}
	out, rpcErr := p.handleMcpListTools(&appserverproto.McpListToolsParams{})
	if rpcErr != nil {
		t.Fatalf("rpc error: %v", rpcErr)
	}
	if resp := out.(appserverproto.McpListToolsResponse); len(resp.Tools) != 0 {
		t.Errorf("expected empty tools with no gateway, got %d", len(resp.Tools))
	}
}

func TestHandleMcpCallTool(t *testing.T) {
	isErr := false
	gw := &fakeMcpGateway{result: protocol.CallToolResult{
		Content: []json.RawMessage{
			json.RawMessage(`{"type":"text","text":"{\"score\":88}"}`),
			json.RawMessage(`{"type":"image","data":"ignored"}`),
		},
		IsError: &isErr,
	}}
	p := &Processor{assembly: &Assembly{McpGateway: gw}}

	out, rpcErr := p.handleMcpCallTool(context.Background(), &appserverproto.McpCallToolParams{
		QualifiedName: "mcp__gaussdb__health",
	})
	if rpcErr != nil {
		t.Fatalf("rpc error: %v", rpcErr)
	}
	resp := out.(appserverproto.McpCallToolResponse)
	if resp.IsError {
		t.Error("expected IsError=false")
	}
	if resp.Text != `{"score":88}` {
		t.Errorf("text = %q, want the JSON text block (image block ignored)", resp.Text)
	}
	if gw.gotQN != "mcp__gaussdb__health" {
		t.Errorf("gateway invoked %q", gw.gotQN)
	}
}

func TestHandleMcpCallToolErrors(t *testing.T) {
	// No gateway -> invalidRequest.
	p := &Processor{assembly: &Assembly{}}
	if _, rpcErr := p.handleMcpCallTool(context.Background(), &appserverproto.McpCallToolParams{QualifiedName: "x"}); rpcErr == nil {
		t.Error("expected error when no gateway")
	}
	// Missing name -> invalidParams.
	p2 := &Processor{assembly: &Assembly{McpGateway: &fakeMcpGateway{}}}
	if _, rpcErr := p2.handleMcpCallTool(context.Background(), &appserverproto.McpCallToolParams{}); rpcErr == nil {
		t.Error("expected error when qualified_name empty")
	}
}

// TestFlattenMcpTextBlocksAudience verifies slash flattening returns only
// user-addressed blocks when present (model-only material is not leaked), and
// falls back to all blocks when nothing is annotated for the user.
func TestFlattenMcpTextBlocksAudience(t *testing.T) {
	mk := func(text string, audience []string) json.RawMessage {
		m := map[string]any{"type": "text", "text": text}
		if audience != nil {
			m["annotations"] = map[string]any{"audience": audience}
		}
		b, _ := json.Marshal(m)
		return b
	}
	// user + model blocks → only the user block.
	got := flattenMcpTextBlocks([]json.RawMessage{
		mk("EVIDENCE", []string{"user", "assistant"}),
		mk("MODEL-ONLY INSTRUCTION", []string{"assistant"}),
	})
	if got != "EVIDENCE" {
		t.Errorf("want only user block, got %q", got)
	}
	// no audience anywhere → all blocks (back-compat).
	got = flattenMcpTextBlocks([]json.RawMessage{mk("a", nil), mk("b", nil)})
	if got != "a\nb" {
		t.Errorf("want all blocks, got %q", got)
	}
	// assistant-only (no user) → returned (back-compat: don't blank the output).
	got = flattenMcpTextBlocks([]json.RawMessage{mk("digest", []string{"assistant"})})
	if got != "digest" {
		t.Errorf("want assistant block when no user audience, got %q", got)
	}
}
