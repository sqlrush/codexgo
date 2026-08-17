package mcp

import (
	"encoding/json"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/tools"
)

func TestNamespaceForServer(t *testing.T) {
	t.Parallel()
	if got := NamespaceForServer("github"); got != "mcp__github" {
		t.Fatalf("got %q", got)
	}
}

func TestFullyQualifiedToolName(t *testing.T) {
	t.Parallel()
	if got := FullyQualifiedToolName("github", "search"); got != "mcp__github__search" {
		t.Fatalf("got %q", got)
	}
}

func TestQualifiedToolNameRoundTrip(t *testing.T) {
	t.Parallel()
	qn := QualifiedToolName("srv", "tool")
	if got := qn.String(); got != "mcp__srv__tool" {
		t.Fatalf("ToolName.String()=%q", got)
	}
}

func TestParseFullyQualifiedToolName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		in         string
		wantServer string
		wantTool   string
		wantErr    bool
	}{
		{name: "ok", in: "mcp__github__search", wantServer: "github", wantTool: "search"},
		{name: "tool with delimiter", in: "mcp__srv__a__b", wantServer: "srv", wantTool: "a__b"},
		{name: "missing prefix", in: "github__search", wantErr: true},
		{name: "missing tool", in: "mcp__github", wantErr: true},
		{name: "empty server", in: "mcp____tool", wantErr: true},
		{name: "empty", in: "", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server, tool, err := ParseFullyQualifiedToolName(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if server != tc.wantServer || tool != tc.wantTool {
				t.Fatalf("got (%q,%q) want (%q,%q)", server, tool, tc.wantServer, tc.wantTool)
			}
		})
	}
}

func TestNamespacedToolToolSpec(t *testing.T) {
	t.Parallel()
	nt := NamespacedTool{
		ServerName: "srv",
		Tool: protocol.Tool{
			Name:        "echo",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		},
	}
	if got := nt.QualifiedName(); got != "mcp__srv__echo" {
		t.Fatalf("QualifiedName=%q", got)
	}

	for _, deferred := range []bool{false, true} {
		spec, err := nt.ToolSpec(deferred)
		if err != nil {
			t.Fatalf("ToolSpec(deferred=%v): %v", deferred, err)
		}
		if spec.Kind != tools.ToolSpecKindFunction {
			t.Fatalf("expected function spec, got kind %v", spec.Kind)
		}
		if spec.Function == nil {
			t.Fatal("function spec is nil")
		}
		// The lowered function carries the raw tool name; the "mcp__srv__" prefix
		// lives in the ToolName namespace (matching the reference, which renames
		// to toolName.Name only).
		if spec.Function.Name != "echo" {
			t.Fatalf("function name=%q want echo", spec.Function.Name)
		}
		gotDeferred := spec.Function.DeferLoading != nil && *spec.Function.DeferLoading
		if gotDeferred != deferred {
			t.Fatalf("deferred flag=%v want %v", gotDeferred, deferred)
		}
	}
}

func TestToolSpecsDedupByName(t *testing.T) {
	t.Parallel()
	schema := json.RawMessage(`{"type":"object"}`)
	in := []NamespacedTool{
		{ServerName: "srv", Tool: protocol.Tool{Name: "dup", InputSchema: schema}},
		{ServerName: "srv", Tool: protocol.Tool{Name: "dup", InputSchema: schema}},
		{ServerName: "srv", Tool: protocol.Tool{Name: "unique", InputSchema: schema}},
	}
	specs, err := ToolSpecs(in, false)
	if err != nil {
		t.Fatalf("ToolSpecs: %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("expected 2 deduped specs, got %d", len(specs))
	}
	names := map[string]bool{}
	for _, s := range specs {
		if s.Function == nil {
			t.Fatal("non-function spec")
		}
		names[s.Function.Name] = true
	}
	// Dedup is by fully-qualified name; the two "dup" tools collapse to one. The
	// lowered function names are the raw tool names.
	if !names["dup"] || !names["unique"] {
		t.Fatalf("unexpected spec names: %v", names)
	}
}
