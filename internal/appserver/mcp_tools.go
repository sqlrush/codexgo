package appserver

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/sqlrush/codexgo/internal/appserverproto"
)

// handleMcpListTools answers mcp/listTools: the connected MCP tools, sorted by
// canonical name. Returns an empty list (not an error) when no MCP gateway is
// wired, so the client can simply offer no dynamic commands.
func (p *Processor) handleMcpListTools(_ *appserverproto.McpListToolsParams) (any, *RPCError) {
	resp := appserverproto.McpListToolsResponse{Tools: []appserverproto.McpToolDescriptor{}}
	if p.assembly == nil || p.assembly.McpGateway == nil {
		return resp, nil
	}
	for _, info := range p.assembly.McpGateway.ListAllToolInfos() {
		d := appserverproto.McpToolDescriptor{
			QualifiedName: info.CanonicalToolName().String(),
			Server:        info.ServerName,
			Tool:          info.CallableName,
			InputSchema:   info.Tool.InputSchema,
		}
		if info.Tool.Description != nil {
			d.Description = *info.Tool.Description
		}
		resp.Tools = append(resp.Tools, d)
	}
	sort.Slice(resp.Tools, func(i, j int) bool {
		return resp.Tools[i].QualifiedName < resp.Tools[j].QualifiedName
	})
	return resp, nil
}

// handleMcpCallTool answers mcp/callTool: invoke one MCP tool deterministically
// (no LLM turn) and return its flattened text result.
func (p *Processor) handleMcpCallTool(ctx context.Context, params *appserverproto.McpCallToolParams) (any, *RPCError) {
	if p.assembly == nil || p.assembly.McpGateway == nil {
		return nil, invalidRequest("no MCP servers are connected")
	}
	if params.QualifiedName == "" {
		return nil, invalidParams("qualified_name is required")
	}
	args := params.Arguments
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	result, err := p.assembly.McpGateway.CallQualifiedTool(ctx, params.QualifiedName, args, nil)
	if err != nil {
		return nil, internalError("mcp call %q failed: %v", params.QualifiedName, err)
	}
	resp := appserverproto.McpCallToolResponse{Text: flattenMcpTextBlocks(result.Content)}
	if result.IsError != nil && *result.IsError {
		resp.IsError = true
	}
	return resp, nil
}

// flattenMcpTextBlocks concatenates the text content blocks of an MCP tool
// result, ignoring non-text blocks.
func flattenMcpTextBlocks(blocks []json.RawMessage) string {
	var b strings.Builder
	for _, raw := range blocks {
		var item struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(raw, &item); err != nil || item.Type != "text" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(item.Text)
	}
	return b.String()
}
