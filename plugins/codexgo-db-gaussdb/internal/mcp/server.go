package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// ToolHandler executes a tool call. args is the raw JSON arguments object.
type ToolHandler func(ctx context.Context, args json.RawMessage) (CallToolResult, error)

// Server is a minimal stdio MCP server: it reads newline-delimited JSON-RPC
// requests, dispatches initialize / tools/list / tools/call, and writes
// responses. Tool registration and the protocol loop are all it owns; the
// actual tools live in the caller.
type Server struct {
	name         string
	version      string
	instructions string

	mu       sync.Mutex
	tools    []Tool
	handlers map[string]ToolHandler

	out *json.Encoder
}

// NewServer builds a server with the given identity and server-level
// instructions (the domain knowledge surfaced to the LLM via initialize).
func NewServer(name, version, instructions string) *Server {
	return &Server{
		name:         name,
		version:      version,
		instructions: instructions,
		handlers:     map[string]ToolHandler{},
	}
}

// Register adds a tool and its handler.
func (s *Server) Register(t Tool, h ToolHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools = append(s.tools, t)
	s.handlers[t.Name] = h
}

// Serve runs the read/dispatch/write loop until in is closed. Each line is one
// JSON-RPC message. Notifications (no id) get no response.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	s.out = json.NewEncoder(out)
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024) // allow large messages
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req jsonrpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			s.reply(nil, nil, &jsonrpcError{Code: codeParseError, Message: "parse error"})
			continue
		}
		s.dispatch(ctx, req)
	}
	return scanner.Err()
}

// dispatch routes one request.
func (s *Server) dispatch(ctx context.Context, req jsonrpcRequest) {
	isNotification := len(req.ID) == 0
	switch req.Method {
	case "initialize":
		s.reply(req.ID, initializeResult{
			ProtocolVersion: "2024-11-05",
			Capabilities:    map[string]any{"tools": map[string]any{}},
			ServerInfo:      serverInfo{Name: s.name, Version: s.version},
			Instructions:    s.instructions,
		}, nil)
	case "notifications/initialized", "notifications/cancelled":
		// no response for notifications
	case "tools/list":
		s.mu.Lock()
		result := listToolsResult{Tools: append([]Tool(nil), s.tools...)}
		s.mu.Unlock()
		s.reply(req.ID, result, nil)
	case "tools/call":
		s.handleToolCall(ctx, req)
	default:
		if !isNotification {
			s.reply(req.ID, nil, &jsonrpcError{Code: codeMethodNotFound, Message: fmt.Sprintf("method %q not found", req.Method)})
		}
	}
}

// handleToolCall invokes a registered tool. A handler error is reported as an
// MCP tool error (isError:true) rather than a JSON-RPC error, so the model/user
// sees the message in the tool result (MCP convention).
func (s *Server) handleToolCall(ctx context.Context, req jsonrpcRequest) {
	var params callToolParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.reply(req.ID, nil, &jsonrpcError{Code: codeInvalidRequest, Message: "invalid tools/call params"})
		return
	}
	s.mu.Lock()
	h, ok := s.handlers[params.Name]
	s.mu.Unlock()
	if !ok {
		s.reply(req.ID, CallToolResult{Content: []ContentItem{TextContent("unknown tool: " + params.Name)}, IsError: true}, nil)
		return
	}
	res, err := h(ctx, params.Arguments)
	if err != nil {
		s.reply(req.ID, CallToolResult{Content: []ContentItem{TextContent(err.Error())}, IsError: true}, nil)
		return
	}
	s.reply(req.ID, res, nil)
}

// reply writes a JSON-RPC response (skipped when id is nil — notifications).
func (s *Server) reply(id json.RawMessage, result any, rpcErr *jsonrpcError) {
	if len(id) == 0 && rpcErr == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.out.Encode(jsonrpcResponse{JSONRPC: "2.0", ID: id, Result: result, Error: rpcErr})
}
