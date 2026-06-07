package tui

import (
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/internal/appserverproto"
)

func sampleMcpTools() []appserverproto.McpToolDescriptor {
	return []appserverproto.McpToolDescriptor{
		{QualifiedName: "mcp__gaussdb__db_health", Server: "gaussdb", Tool: "db_health"},
		{QualifiedName: "mcp__gaussdb__db_slowsql", Server: "gaussdb", Tool: "db_slowsql"},
	}
}

func TestMatchMcpSlash(t *testing.T) {
	m := Model{mcpTools: indexMcpTools(sampleMcpTools())}

	tests := []struct {
		in       string
		wantOK   bool
		wantQN   string
		wantArgs string
	}{
		{"/db_health", true, "mcp__gaussdb__db_health", ""},
		{"/DB_HEALTH", true, "mcp__gaussdb__db_health", ""}, // case-insensitive
		{`/db_slowsql {"threshold_ms":500}`, true, "mcp__gaussdb__db_slowsql", `{"threshold_ms":500}`},
		{"/unknown_tool", false, "", ""},
		{"hello world", false, "", ""},
		{"/", false, "", ""},
	}
	for _, tt := range tests {
		desc, args, ok := m.matchMcpSlash(tt.in)
		if ok != tt.wantOK {
			t.Errorf("matchMcpSlash(%q) ok=%v want %v", tt.in, ok, tt.wantOK)
			continue
		}
		if ok && (desc.QualifiedName != tt.wantQN || args != tt.wantArgs) {
			t.Errorf("matchMcpSlash(%q) = (%q,%q) want (%q,%q)", tt.in, desc.QualifiedName, args, tt.wantQN, tt.wantArgs)
		}
	}
}

func TestMatchMcpSlashNoToolsNeverMatches(t *testing.T) {
	m := Model{} // no tools loaded
	if _, _, ok := m.matchMcpSlash("/db_health"); ok {
		t.Error("expected no match when no MCP tools are loaded")
	}
}

func TestMcpSlashArgsToJSON(t *testing.T) {
	if js, err := mcpSlashArgsToJSON(""); err != "" || string(js) != "{}" {
		t.Errorf("empty args -> (%q,%q), want ({}, \"\")", js, err)
	}
	if js, err := mcpSlashArgsToJSON(`{"a":1}`); err != "" || string(js) != `{"a":1}` {
		t.Errorf("json args -> (%q,%q)", js, err)
	}
	if _, err := mcpSlashArgsToJSON("500"); err == "" {
		t.Error("non-JSON args should be rejected with guidance")
	}
	if _, err := mcpSlashArgsToJSON(`{"bad":`); err == "" {
		t.Error("malformed JSON should be rejected")
	}
}

func TestPrettyJSON(t *testing.T) {
	got := prettyJSON(`{"score":88}`)
	if !strings.Contains(got, "\n") || !strings.Contains(got, "\"score\": 88") {
		t.Errorf("prettyJSON did not indent: %q", got)
	}
	if prettyJSON("plain text") != "plain text" {
		t.Error("non-JSON should pass through unchanged")
	}
}

func TestTranscriptRendersMcpToolResult(t *testing.T) {
	tr := NewChatTranscript(testTheme())
	view, _ := tr.Update(McpToolResultMsg{Command: "db_health", Text: "health score 88", IsError: false})
	out := view.View(Rect{Width: 60, Height: 8})
	if !strings.Contains(out, "health score 88") {
		t.Fatalf("MCP tool result not rendered: %q", out)
	}
}

func TestIndexMcpToolsKeyedByLowerName(t *testing.T) {
	idx := indexMcpTools(sampleMcpTools())
	if _, ok := idx["db_health"]; !ok {
		t.Error("expected db_health key")
	}
	if len(idx) != 2 {
		t.Errorf("expected 2 tools, got %d", len(idx))
	}
}
