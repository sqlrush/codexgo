package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// Initialize performs the MCP initialization handshake: it sends the initialize
// request, then the notifications/initialized notification, and records that the
// session is ready. It returns the server's InitializeResult. Initialize may be
// called at most once per client.
func (c *Client) Initialize(ctx context.Context, params InitializeParams, timeout time.Duration) (InitializeResult, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return InitializeResult{}, ErrClientShutDown
	}
	if c.initOK {
		c.mu.Unlock()
		return InitializeResult{}, errInitTwice
	}
	c.mu.Unlock()

	if params.ProtocolVersion == "" {
		params.ProtocolVersion = MCPProtocolVersion
	}

	raw, err := c.call(ctx, MethodInitialize, params, timeout)
	if err != nil {
		return InitializeResult{}, err
	}
	var result InitializeResult
	if err := decodeResult(raw, &result); err != nil {
		return InitializeResult{}, err
	}

	if err := c.notify(ctx, MethodInitializedNotify, nil); err != nil {
		return InitializeResult{}, fmt.Errorf("mcp: send initialized notification: %w", err)
	}

	c.mu.Lock()
	c.initOK = true
	c.mu.Unlock()
	return result, nil
}

// ListTools fetches one page of tools from the server. cursor may be nil for the
// first page; the returned NextCursor (when set) feeds the next call.
func (c *Client) ListTools(ctx context.Context, cursor *string, timeout time.Duration) (ListToolsResult, error) {
	if err := c.requireInit(); err != nil {
		return ListToolsResult{}, err
	}
	params := paginatedParams(cursor)
	raw, err := c.call(ctx, MethodToolsList, params, timeout)
	if err != nil {
		return ListToolsResult{}, err
	}
	var result ListToolsResult
	if err := decodeResult(raw, &result); err != nil {
		return ListToolsResult{}, err
	}
	return result, nil
}

// ListAllTools pages through tools/list until exhaustion, guarding against a
// server that repeats a cursor. The aggregated tool list is returned.
func (c *Client) ListAllTools(ctx context.Context, timeout time.Duration) ([]protocol.Tool, error) {
	var (
		tools  []protocol.Tool
		cursor *string
	)
	for {
		page, err := c.ListTools(ctx, cursor, timeout)
		if err != nil {
			return nil, err
		}
		tools = append(tools, page.Tools...)
		if page.NextCursor == nil {
			return tools, nil
		}
		if cursor != nil && *cursor == *page.NextCursor {
			return nil, fmt.Errorf("mcp: tools/list returned duplicate cursor")
		}
		cursor = page.NextCursor
	}
}

// CallTool invokes a tool by its raw MCP name with the given arguments and
// optional request metadata. arguments and meta must each be a JSON object or
// nil; any other shape is rejected, matching the reference client.
func (c *Client) CallTool(ctx context.Context, name string, arguments, meta json.RawMessage, timeout time.Duration) (protocol.CallToolResult, error) {
	if err := c.requireInit(); err != nil {
		return protocol.CallToolResult{}, err
	}
	if err := requireObjectOrNil("arguments", arguments); err != nil {
		return protocol.CallToolResult{}, err
	}
	if err := requireObjectOrNil("_meta", meta); err != nil {
		return protocol.CallToolResult{}, err
	}
	params := CallToolParams{Name: name, Arguments: arguments, Meta: meta}
	raw, err := c.call(ctx, MethodToolsCall, params, timeout)
	if err != nil {
		return protocol.CallToolResult{}, err
	}
	var result protocol.CallToolResult
	if err := decodeResult(raw, &result); err != nil {
		return protocol.CallToolResult{}, err
	}
	return result, nil
}

// ListResources fetches one page of resources.
func (c *Client) ListResources(ctx context.Context, cursor *string, timeout time.Duration) (ListResourcesResult, error) {
	if err := c.requireInit(); err != nil {
		return ListResourcesResult{}, err
	}
	raw, err := c.call(ctx, MethodResourcesList, paginatedParams(cursor), timeout)
	if err != nil {
		return ListResourcesResult{}, err
	}
	var result ListResourcesResult
	if err := decodeResult(raw, &result); err != nil {
		return ListResourcesResult{}, err
	}
	return result, nil
}

// ListAllResources pages through resources/list, guarding against repeated
// cursors.
func (c *Client) ListAllResources(ctx context.Context, timeout time.Duration) ([]protocol.Resource, error) {
	var (
		out    []protocol.Resource
		cursor *string
	)
	for {
		page, err := c.ListResources(ctx, cursor, timeout)
		if err != nil {
			return nil, err
		}
		out = append(out, page.Resources...)
		if page.NextCursor == nil {
			return out, nil
		}
		if cursor != nil && *cursor == *page.NextCursor {
			return nil, fmt.Errorf("mcp: resources/list returned duplicate cursor")
		}
		cursor = page.NextCursor
	}
}

// ListResourceTemplates fetches one page of resource templates.
func (c *Client) ListResourceTemplates(ctx context.Context, cursor *string, timeout time.Duration) (ListResourceTemplatesResult, error) {
	if err := c.requireInit(); err != nil {
		return ListResourceTemplatesResult{}, err
	}
	raw, err := c.call(ctx, MethodResourceTemplatesList, paginatedParams(cursor), timeout)
	if err != nil {
		return ListResourceTemplatesResult{}, err
	}
	var result ListResourceTemplatesResult
	if err := decodeResult(raw, &result); err != nil {
		return ListResourceTemplatesResult{}, err
	}
	return result, nil
}

// ListAllResourceTemplates pages through resources/templates/list, guarding
// against repeated cursors.
func (c *Client) ListAllResourceTemplates(ctx context.Context, timeout time.Duration) ([]protocol.ResourceTemplate, error) {
	var (
		out    []protocol.ResourceTemplate
		cursor *string
	)
	for {
		page, err := c.ListResourceTemplates(ctx, cursor, timeout)
		if err != nil {
			return nil, err
		}
		out = append(out, page.ResourceTemplates...)
		if page.NextCursor == nil {
			return out, nil
		}
		if cursor != nil && *cursor == *page.NextCursor {
			return nil, fmt.Errorf("mcp: resources/templates/list returned duplicate cursor")
		}
		cursor = page.NextCursor
	}
}

// ReadResource reads a single resource by URI.
func (c *Client) ReadResource(ctx context.Context, uri string, timeout time.Duration) (ReadResourceResult, error) {
	if err := c.requireInit(); err != nil {
		return ReadResourceResult{}, err
	}
	raw, err := c.call(ctx, MethodResourcesRead, ReadResourceParams{URI: uri}, timeout)
	if err != nil {
		return ReadResourceResult{}, err
	}
	var result ReadResourceResult
	if err := decodeResult(raw, &result); err != nil {
		return ReadResourceResult{}, err
	}
	return result, nil
}

// Ping issues a ping request, used as a lightweight liveness probe.
func (c *Client) Ping(ctx context.Context, timeout time.Duration) error {
	if err := c.requireInit(); err != nil {
		return err
	}
	_, err := c.call(ctx, MethodPing, nil, timeout)
	return err
}

// paginatedParams returns a PaginatedParams pointer when a cursor is present, or
// nil so the request omits params entirely (matching rmcp's None default).
func paginatedParams(cursor *string) any {
	if cursor == nil {
		return nil
	}
	return PaginatedParams{Cursor: cursor}
}

// requireObjectOrNil validates that raw is nil/empty or a JSON object, returning
// a descriptive error otherwise. This mirrors the reference client's rejection
// of non-object tool arguments and _meta.
func requireObjectOrNil(field string, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var probe any
	if err := json.Unmarshal(raw, &probe); err != nil {
		return fmt.Errorf("mcp: %s is not valid JSON: %w", field, err)
	}
	if _, ok := probe.(map[string]any); !ok {
		return fmt.Errorf("mcp: %s must be a JSON object", field)
	}
	return nil
}
