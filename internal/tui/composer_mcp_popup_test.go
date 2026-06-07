package tui

import "testing"

// findPopupRow returns the popup row whose label matches, plus whether found.
func findPopupRow(c Composer, label string) (PopupRow, bool) {
	rows, _, ok := c.PopupRows()
	if !ok {
		return PopupRow{}, false
	}
	for _, r := range rows {
		if r.Label == label {
			return r, true
		}
	}
	return PopupRow{}, false
}

func TestSlashPopupIncludesMcpCommands(t *testing.T) {
	c := NewComposer(testTheme(), nil).WithMcpCommands([]mcpPopupCmd{
		{name: "health", desc: "instance health check"},
		{name: "slowsql", desc: "slow SQL"},
	})
	// Filter to the MCP command (an unfiltered "/" windows to the top built-in
	// rows, scrolling MCP commands — which follow the built-ins — out of view).
	c = typeString(c, "/health")
	if !c.PopupVisible() {
		t.Fatal("slash popup should be visible after typing '/health'")
	}
	row, ok := findPopupRow(c, "/health")
	if !ok {
		t.Fatal("/health (MCP command) missing from slash popup")
	}
	if !row.IsMcp {
		t.Error("/health should be flagged IsMcp for distinct coloring")
	}
	if row.Detail != "instance health check" {
		t.Errorf("detail = %q, want the tool description", row.Detail)
	}
}

func TestSlashPopupMcpCommandsFilterByPrefix(t *testing.T) {
	c := NewComposer(testTheme(), nil).WithMcpCommands([]mcpPopupCmd{
		{name: "health", desc: "h"},
		{name: "slowsql", desc: "s"},
	})
	c = typeString(c, "/slow")
	if _, ok := findPopupRow(c, "/slowsql"); !ok {
		t.Error("/slowsql should match prefix /slow")
	}
	if _, ok := findPopupRow(c, "/health"); ok {
		t.Error("/health should be filtered out by prefix /slow")
	}
}

func TestSlashPopupBuiltinsNotFlaggedMcp(t *testing.T) {
	c := NewComposer(testTheme(), nil).WithMcpCommands([]mcpPopupCmd{{name: "health", desc: "h"}})
	c = typeString(c, "/")
	// A representative built-in must not be flagged as MCP.
	if row, ok := findPopupRow(c, "/model"); ok && row.IsMcp {
		t.Error("built-in /model must not be flagged IsMcp")
	}
}
