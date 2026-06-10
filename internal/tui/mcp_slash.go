package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sqlrush/codexgo/internal/appserverproto"
)

// This file implements the human "dual entry": typing a slash command whose name
// matches a connected MCP tool (e.g. /db_health) invokes that tool
// deterministically — no LLM turn — and renders the result. The command set is
// sourced entirely from the connected MCP servers (mcp/listTools), so codexgo
// core stays decoupled from any specific plugin's commands.
//
// The intercept lives in the model's SubmitUserMessageEvent handler: a leading
// "/name" that is not a built-in slash arrives here as plain text, and if name
// matches a known MCP tool we run it instead of starting an LLM turn.

// mcpToolsLoadedMsg delivers the connected MCP tools fetched at startup.
type mcpToolsLoadedMsg struct {
	tools []appserverproto.McpToolDescriptor
}

// McpToolResultMsg carries a deterministic MCP tool-call result to the
// transcript for rendering as a notice cell.
type McpToolResultMsg struct {
	Command string
	Text    string
	IsError bool
}

// loadMcpToolsCmd fetches the connected MCP tools off the UI loop and delivers
// them via mcpToolsLoadedMsg. It degrades silently (no dynamic commands) when
// MCP is unavailable.
func (m Model) loadMcpToolsCmd() tea.Cmd {
	engine := m.engine
	sender := m.sender
	if engine == nil || sender == nil {
		return nil
	}
	return func() tea.Msg {
		go func() {
			toolsList, err := engine.ListMcpTools(context.Background())
			if err != nil || len(toolsList) == 0 {
				return
			}
			sender.SendMsg(mcpToolsLoadedMsg{tools: toolsList})
		}()
		return nil
	}
}

// indexMcpTools keys the descriptors by their lowercased raw tool name (the
// model-visible slash name). On a raw-name collision across servers the last
// wins; such tools remain callable by the model and via /mcp-style addressing
// (a future enhancement could disambiguate by server).
func indexMcpTools(list []appserverproto.McpToolDescriptor) map[string]appserverproto.McpToolDescriptor {
	out := make(map[string]appserverproto.McpToolDescriptor, len(list))
	for _, d := range list {
		out[strings.ToLower(d.Tool)] = d
	}
	return out
}

// mcpPopupCommands converts the connected MCP tool descriptors into sorted
// composer slash-popup commands (name + description).
func mcpPopupCommands(toolList []appserverproto.McpToolDescriptor) []mcpPopupCmd {
	out := make([]mcpPopupCmd, 0, len(toolList))
	for _, d := range toolList {
		out = append(out, mcpPopupCmd{name: d.Tool, desc: d.Description})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// matchMcpSlash reports whether text is a "/name [args]" whose name matches a
// connected MCP tool, returning the descriptor and the trailing argument string.
func (m Model) matchMcpSlash(text string) (appserverproto.McpToolDescriptor, string, bool) {
	t := strings.TrimSpace(text)
	if len(m.mcpTools) == 0 || !strings.HasPrefix(t, "/") || t == "/" {
		return appserverproto.McpToolDescriptor{}, "", false
	}
	body := t[1:]
	name := body
	args := ""
	if i := strings.IndexAny(body, " \t\n"); i >= 0 {
		name = body[:i]
		args = strings.TrimSpace(body[i+1:])
	}
	desc, ok := m.mcpTools[strings.ToLower(name)]
	if !ok {
		return appserverproto.McpToolDescriptor{}, "", false
	}
	return desc, args, true
}

// mcpSlashArgsToJSON converts a slash argument string into the tool's JSON
// arguments object: empty -> {}, a JSON object -> itself (validated). Any other
// non-empty form is rejected with usage guidance, since tool arguments are
// structured.
func mcpSlashArgsToJSON(args string) (json.RawMessage, string) {
	trimmed := strings.TrimSpace(args)
	if trimmed == "" {
		return json.RawMessage("{}"), ""
	}
	if strings.HasPrefix(trimmed, "{") && json.Valid([]byte(trimmed)) {
		return json.RawMessage(trimmed), ""
	}
	return nil, "arguments must be a JSON object, e.g. {\"threshold_ms\":500}"
}

// runMcpTool echoes the command, validates args, and dispatches a deterministic
// tool call (or renders a usage error). It returns the updated model + command.
func (m Model) runMcpTool(desc appserverproto.McpToolDescriptor, args, rawText string) (tea.Model, tea.Cmd) {
	m.transcript = m.transcript.AppendUserMessage(strings.TrimSpace(rawText))

	argsJSON, argErr := mcpSlashArgsToJSON(args)
	if argErr != "" {
		sender := m.sender
		return m, func() tea.Msg {
			sender.SendMsg(McpToolResultMsg{Command: desc.Tool, Text: argErr, IsError: true})
			return nil
		}
	}
	return m, m.callMcpCmd(desc.Tool, desc.QualifiedName, argsJSON)
}

// callMcpCmd dispatches a deterministic MCP tool call off the UI loop and
// delivers the (markdown-rendered) result via McpToolResultMsg.
func (m Model) callMcpCmd(toolName, qualified string, argsJSON json.RawMessage) tea.Cmd {
	engine := m.engine
	sender := m.sender
	return func() tea.Msg {
		go func() {
			resp, err := engine.CallMcpTool(context.Background(), qualified, argsJSON)
			if err != nil {
				sender.SendMsg(McpToolResultMsg{Command: toolName, Text: err.Error(), IsError: true})
				return
			}
			sender.SendMsg(McpToolResultMsg{Command: toolName, Text: prettyJSON(resp.Text), IsError: resp.IsError})
		}()
		return nil
	}
}

// dbRawKeywordRe matches input that opens with a SQL statement keyword. Combined
// with a leading backslash (psql meta-command), this is the third "native psql"
// entry: such input is routed deterministically to the connected `sql` tool
// (read-only; writes are rejected there with a clear message) instead of starting
// an LLM turn. The CJK natural-language the user normally types never matches.
var dbRawKeywordRe = regexp.MustCompile(`(?i)^(SELECT|WITH|EXPLAIN|SHOW|VALUES|TABLE|INSERT|UPDATE|DELETE|MERGE|CREATE|ALTER|DROP|TRUNCATE|GRANT|REVOKE|CALL|COMMENT|VACUUM|ANALYZE|REINDEX|CLUSTER|COPY|LOCK|CHECKPOINT)\b`)

// matchDbRaw reports whether text is bare SQL (starts with a SQL keyword) or a
// psql backslash meta-command, AND a connected MCP tool named "sql" exists to run
// it. Returns the sql tool descriptor and the statement (the verbatim input).
func (m Model) matchDbRaw(text string) (appserverproto.McpToolDescriptor, string, bool) {
	t := strings.TrimSpace(text)
	if t == "" || strings.HasPrefix(t, "/") {
		return appserverproto.McpToolDescriptor{}, "", false
	}
	desc, ok := m.mcpTools["sql"]
	if !ok {
		return appserverproto.McpToolDescriptor{}, "", false
	}
	if strings.HasPrefix(t, `\`) || dbRawKeywordRe.MatchString(t) {
		return desc, t, true
	}
	return appserverproto.McpToolDescriptor{}, "", false
}

// runDbRaw dispatches bare SQL / a backslash meta-command to the `sql` tool with
// {"statement": <verbatim>}, bypassing the LLM and the @show gate (deterministic,
// like a slash). The tool enforces read-only and renders the result table.
func (m Model) runDbRaw(desc appserverproto.McpToolDescriptor, statement, rawText string) (tea.Model, tea.Cmd) {
	m.transcript = m.transcript.AppendUserMessage(strings.TrimSpace(rawText))
	argsJSON, _ := json.Marshal(map[string]string{"statement": statement})
	return m, m.callMcpCmd(desc.Tool, desc.QualifiedName, json.RawMessage(argsJSON))
}

// prettyJSON indents s when it is a JSON document, otherwise returns it as-is.
// MCP tools here return structured JSON, so indenting it makes the deterministic
// result readable in the transcript.
func prettyJSON(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" || !(strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")) {
		return s
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(trimmed), "", "  "); err != nil {
		return s
	}
	return buf.String()
}
