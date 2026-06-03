package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/internal/appserver"
	"github.com/sqlrush/codexgo/internal/core"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// MessageProcessor handles JSON-RPC frames arriving over the MCP transport. It is
// the faithful Go port of the Rust MessageProcessor: it owns the MCP protocol
// handshake, the codex/codex-reply tools, server->client approval correlation,
// and the live event stream. The v2/v1 app-server methods documented by the MCP
// interface are routed to a shared [appserver.Processor] driving the same engine.
//
// One MessageProcessor is created per logical connection (one client). Requests
// are processed sequentially by the serve loop; long-running tool calls run on
// their own goroutines so the loop is never blocked.
type MessageProcessor struct {
	sender   *outgoingSender
	manager  *core.ThreadManager
	defaults toolDefaults
	running  *runningRequests

	// app is the shared app-server processor used to service the v2/v1 methods.
	app *appserver.Processor
	// appConn carries the app-server connection state (initialize handshake).
	appConn *appserver.Conn
	// appSink is the bridge sink used to suppress the app-server initialize reply.
	appSink *bridgeSink

	info        serverInfo
	initialized bool
}

// ProcessorConfig bundles the dependencies for a [MessageProcessor].
type ProcessorConfig struct {
	// Assembly is the constructed engine shared with the app-server; required.
	Assembly *appserver.Assembly
	// Defaults are the per-session default settings; required.
	Defaults appserver.Defaults
	// Writer delivers serialized server-to-client frames; required.
	Writer frameWriter
	// ServerName and ServerVersion are reported in the initialize response.
	ServerName    string
	ServerVersion string
	// UserAgent is reported as serverInfo.user_agent in the initialize response.
	UserAgent string
}

// NewMessageProcessor constructs a [MessageProcessor]. The shared app-server
// processor is built over the same assembly and a bridge sink so its responses,
// errors, and notifications are written through the MCP transport.
func NewMessageProcessor(cfg ProcessorConfig) *MessageProcessor {
	sender := newOutgoingSender(cfg.Writer)
	appSink := newBridgeSink(cfg.Writer)
	app := appserver.NewProcessor(appserver.ProcessorConfig{
		Assembly: cfg.Assembly,
		Defaults: cfg.Defaults,
		Sink:     appSink,
	})
	return &MessageProcessor{
		sender:  sender,
		manager: cfg.Assembly.ThreadManager,
		defaults: toolDefaults{
			Model:            cfg.Defaults.Model,
			ProviderID:       cfg.Defaults.ProviderID,
			Cwd:              cfg.Defaults.Cwd,
			CodexHome:        cfg.Assembly.CodexHome,
			BaseInstructions: cfg.Defaults.BaseInstructions,
		},
		running: newRunningRequests(),
		app:     app,
		appConn: appserver.NewConn(),
		appSink: appSink,
		info: serverInfo{
			name:      orDefault(cfg.ServerName, "codex-mcp-server"),
			version:   orDefault(cfg.ServerVersion, "0.0.0-dev"),
			userAgent: cfg.UserAgent,
		},
	}
}

// serverInfo holds the implementation metadata returned by initialize.
type serverInfo struct {
	name      string
	version   string
	userAgent string
}

// processFrame dispatches one decoded incoming frame to the appropriate handler.
// It is the faithful port of the lib.rs match over JsonRpcMessage variants.
func (p *MessageProcessor) processFrame(ctx context.Context, msg incomingMessage) {
	switch msg.classify() {
	case incomingRequest:
		p.processRequest(ctx, *msg.ID, msg.Method, msg.Params)
	case incomingResponse:
		p.sender.notifyClientResponse(*msg.ID, msg.Result)
	case incomingNotification:
		p.processNotification(msg.Method, msg.Params)
	case incomingError:
		// Errors to server-initiated requests are logged-and-dropped in the
		// reference; resolve the callback with a null result so denials apply.
		if msg.ID != nil {
			p.sender.notifyClientResponse(*msg.ID, json.RawMessage("null"))
		}
	default:
		// Unclassifiable frame: nothing to do.
	}
}

// processRequest routes a client request to its handler. MCP-native methods are
// handled here; everything else is delegated to the shared app-server processor.
func (p *MessageProcessor) processRequest(ctx context.Context, id RequestID, method string, params json.RawMessage) {
	switch method {
	case "initialize":
		p.handleInitialize(id, params)
	case "ping":
		p.sender.sendResponse(id, map[string]any{})
	case "tools/list":
		p.handleListTools(id)
	case "tools/call":
		p.handleCallTool(ctx, id, params)
	case "resources/list", "resources/templates/list", "prompts/list":
		// Read-only MCP discovery requests: the reference logs and returns an
		// empty result. Return the conventional empty shape.
		p.sender.sendResponse(id, emptyListResult(method))
	default:
		p.delegateToAppServer(ctx, id, method, params)
	}
}

// processNotification handles a client notification. The reference logs them;
// cancellation is the only one with side effects.
func (p *MessageProcessor) processNotification(method string, params json.RawMessage) {
	if method == "notifications/cancelled" {
		p.handleCancelled(params)
	}
}

// handleInitialize processes the MCP initialize handshake. It records the client
// info, advances the app-server handshake, and replies with the negotiated
// capabilities plus the codex serverInfo (including the non-spec user_agent).
func (p *MessageProcessor) handleInitialize(id RequestID, params json.RawMessage) {
	if p.initialized {
		p.sender.sendError(id, newErrorBody(codeInvalidRequest, "initialize called more than once", nil))
		return
	}

	var req mcpInitializeParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			p.sender.sendError(id, newErrorBody(codeInvalidParams, fmt.Sprintf("invalid initialize params: %v", err), nil))
			return
		}
	}

	// Advance the shared app-server handshake so delegated v2/v1 methods are
	// accepted. The app-server emits its own initialize response; suppress it so
	// the MCP-shaped response below is the only one written.
	p.advanceAppServerHandshake(req.ClientInfo.Name, req.ClientInfo.Version)

	protocolVersion := req.ProtocolVersion
	if protocolVersion == "" {
		protocolVersion = defaultProtocolVersion
	}

	si := map[string]any{
		"name":       p.info.name,
		"version":    p.info.version,
		"title":      "Codex",
		"user_agent": p.info.userAgent,
	}
	result := map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities": map[string]any{
			"tools": map[string]any{"listChanged": true},
		},
		"serverInfo": si,
	}

	p.initialized = true
	p.sender.sendResponse(id, result)
}

// handleListTools returns the codex and codex-reply tool descriptors. Faithful
// port of handle_list_tools.
func (p *MessageProcessor) handleListTools(id RequestID) {
	p.sender.sendResponse(id, map[string]any{
		"tools": []toolDescriptor{codexTool(), codexReplyTool()},
	})
}

// handleCallTool dispatches a tools/call to the codex or codex-reply tool.
// Faithful port of handle_call_tool.
func (p *MessageProcessor) handleCallTool(ctx context.Context, id RequestID, params json.RawMessage) {
	var call callToolParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &call); err != nil {
			p.sender.sendResponse(id, errorCallToolResult(fmt.Sprintf("Failed to parse tool call params: %v", err)))
			return
		}
	}

	switch call.Name {
	case toolNameCodex:
		p.handleCallCodex(ctx, id, call.Arguments)
	case toolNameCodexReply:
		p.handleCallCodexReply(ctx, id, call.Arguments)
	default:
		p.sender.sendResponse(id, errorCallToolResult(fmt.Sprintf("Unknown tool '%s'", call.Name)))
	}
}

// handleCallCodex parses the codex tool params and runs a fresh session on a new
// goroutine so the serve loop is never blocked. Faithful port of
// handle_tool_call_codex.
func (p *MessageProcessor) handleCallCodex(ctx context.Context, id RequestID, args json.RawMessage) {
	param, err := parseCodexToolCallParam(args)
	if err != nil {
		p.sender.sendResponse(id, errorCallToolResult(fmt.Sprintf("Failed to parse configuration for Codex tool: %v", err)))
		return
	}
	prompt, cfg, err := param.toSessionConfig(p.defaults)
	if err != nil {
		p.sender.sendResponse(id, errorCallToolResult(fmt.Sprintf("Failed to load Codex configuration from overrides: %v", err)))
		return
	}
	go runCodexToolSession(ctx, p.manager, p.sender, p.running, id, prompt, cfg)
}

// handleCallCodexReply parses the reply params, resolves the thread, and
// continues the session on a new goroutine. Faithful port of
// handle_tool_call_codex_session_reply.
func (p *MessageProcessor) handleCallCodexReply(ctx context.Context, id RequestID, args json.RawMessage) {
	param, err := parseCodexReplyParam(args)
	if err != nil {
		p.sender.sendResponse(id, errorCallToolResult(fmt.Sprintf("Failed to parse configuration for Codex tool: %v", err)))
		return
	}
	threadID, err := param.resolveThreadID()
	if err != nil {
		p.sender.sendResponse(id, errorCallToolResult(fmt.Sprintf("Failed to parse thread_id: %v", err)))
		return
	}
	thread, err := p.manager.GetThread(protocol.NewThreadID(threadID))
	if err != nil {
		p.sender.sendResponse(id, callToolResultWithThreadID(threadID, fmt.Sprintf("Session not found for thread_id: %s", threadID), boolTrue()))
		return
	}
	go runCodexToolSessionReply(ctx, thread, p.sender, p.running, id, threadID, param.Prompt)
}

// handleCancelled interrupts the thread servicing the cancelled request id.
// Faithful port of handle_cancelled_notification.
func (p *MessageProcessor) handleCancelled(params json.RawMessage) {
	var note cancelledParams
	if err := json.Unmarshal(params, &note); err != nil {
		return
	}
	if note.RequestID == nil {
		return
	}
	reqIDStr := note.RequestID.String()
	threadID, ok := p.running.get(reqIDStr)
	if !ok {
		return
	}
	thread, err := p.manager.GetThread(protocol.NewThreadID(threadID))
	if err != nil {
		return
	}
	_ = thread.SubmitWithID(protocol.Submission{ID: reqIDStr, Op: protocol.Op{Type: protocol.OpInterrupt}})
	p.running.remove(reqIDStr)
}

// delegateToAppServer routes a v2/v1 method to the shared app-server processor,
// re-using its handler logic and registry. Unknown methods produce a
// method-not-found error.
func (p *MessageProcessor) delegateToAppServer(ctx context.Context, id RequestID, method string, params json.RawMessage) {
	req := appserverRequest(id, method, params)
	p.app.HandleRequest(ctx, p.appConn, req)
}

// advanceAppServerHandshake drives the app-server initialize handshake so its
// connection becomes initialized through the normal handler path. The
// app-server's initialize response is suppressed because the MCP server sends its
// own MCP-shaped response.
func (p *MessageProcessor) advanceAppServerHandshake(name, version string) {
	id := NewStringRequestID("__mcp_init__")
	appID := toAppServerRequestID(id)
	p.appSink.suppressResponse(appID)

	params, _ := json.Marshal(appserverInitializeParams(name, version))
	p.app.HandleRequest(context.Background(), p.appConn, appserverRequest(id, "initialize", params))
}

// Shutdown stops the shared app-server processor's thread forwarders.
func (p *MessageProcessor) Shutdown(ctx context.Context) {
	p.app.Shutdown(ctx)
}

// orDefault returns v when non-empty, else def.
func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// defaultProtocolVersion is echoed when the client omits protocolVersion.
const defaultProtocolVersion = "2025-06-18"

// emptyListResult builds the conventional empty result for a read-only MCP
// discovery request.
func emptyListResult(method string) map[string]any {
	switch method {
	case "resources/list":
		return map[string]any{"resources": []any{}}
	case "resources/templates/list":
		return map[string]any{"resourceTemplates": []any{}}
	case "prompts/list":
		return map[string]any{"prompts": []any{}}
	default:
		return map[string]any{}
	}
}
