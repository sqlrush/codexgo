package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/sqlrush/codexgo/internal/appserverclient"
	"github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// engineClient is the subset of the in-process app-server client the TUI spine
// needs. Defining it here (where it is used) keeps the dependency surface small
// and makes the engine pump unit-testable against a fake. It mirrors the seam
// the exec session uses.
type engineClient interface {
	RequestTyped(ctx context.Context, method string, params any, out any) error
	NextEvent(ctx context.Context) (appserverclient.ServerEvent, bool)
	Shutdown(ctx context.Context)
}

// compile-time assertion that the real client satisfies the seam.
var _ engineClient = (*appserverclient.InProcessAppServerClient)(nil)

// Engine wires the TUI [Model] to an [appserverclient.InProcessAppServerClient].
// It performs the initialize handshake and thread start, submits user turns, and
// pumps the engine's `codex/event` notification stream into the model as
// [CoreEventMsg] values via the [AppEventSender].
//
// Engine is the foundation analogue of the parts of codex-rs/tui/src/app.rs that
// own the conversation manager and the event loop's engine-stream branch.
type Engine struct {
	client   engineClient
	sender   *AppEventSender
	clientNm string
	clientVr string

	mu       sync.Mutex
	threadID string
	pumping  bool
}

// EngineConfig parameterizes an [Engine].
type EngineConfig struct {
	// Client drives the engine; required.
	Client engineClient
	// Sender delivers decoded events into the model; required for the pump.
	Sender *AppEventSender
	// ClientName / ClientVersion identify this client in the initialize handshake.
	// Defaults are applied when empty.
	ClientName    string
	ClientVersion string
}

// NewEngine constructs an [Engine] from the supplied configuration.
func NewEngine(cfg EngineConfig) *Engine {
	name := cfg.ClientName
	if name == "" {
		name = defaultClientName
	}
	version := cfg.ClientVersion
	if version == "" {
		version = defaultClientVersion
	}
	return &Engine{
		client:   cfg.Client,
		sender:   cfg.Sender,
		clientNm: name,
		clientVr: version,
	}
}

// CodexVersion is the upstream codex CLI version codexgo impersonates as a
// drop-in (the parity target). It is the version rendered in the session-header
// card's "(v…)" title and reported in the initialize handshake — codex derives
// both from its own CARGO_PKG_VERSION (0.136.0). codexgo's build identity
// (cli.Version, e.g. "0.0.0-dev") is intentionally distinct: the TUI surfaces
// the impersonated version so the first frame matches real codex byte-for-byte.
const CodexVersion = "0.136.0"

const (
	defaultClientName    = "codex_tui"
	defaultClientVersion = CodexVersion
)

// ThreadID returns the active thread id, or "" before [Start] completes.
func (e *Engine) ThreadID() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.threadID
}

// Start performs the initialize handshake and starts (or resumes) a thread. On
// success the active thread id is recorded and returned. resumeThreadID resumes
// an existing thread when non-empty; otherwise a fresh thread is started.
func (e *Engine) Start(ctx context.Context, resumeThreadID string) (string, error) {
	if err := e.initialize(ctx); err != nil {
		return "", err
	}
	threadID, err := e.startOrResume(ctx, resumeThreadID)
	if err != nil {
		return "", err
	}
	e.mu.Lock()
	e.threadID = threadID
	e.mu.Unlock()
	return threadID, nil
}

// initialize performs the required initialize handshake.
func (e *Engine) initialize(ctx context.Context) error {
	var resp appserverproto.InitializeResponse
	err := e.client.RequestTyped(ctx, "initialize", appserverproto.InitializeParams{
		ClientInfo: appserverproto.ClientInfo{Name: e.clientNm, Version: e.clientVr},
	}, &resp)
	if err != nil {
		return fmt.Errorf("tui: initialize: %w", err)
	}
	return nil
}

// startOrResume starts a fresh thread or resumes an existing one, returning the
// thread id extracted from the thread aggregate.
func (e *Engine) startOrResume(ctx context.Context, resumeID string) (string, error) {
	var threadAgg json.RawMessage
	if resumeID != "" {
		var resp appserverproto.ThreadResumeResponse
		if err := e.client.RequestTyped(ctx, "thread/resume", appserverproto.ThreadResumeParams{
			ThreadID: resumeID,
		}, &resp); err != nil {
			return "", fmt.Errorf("tui: thread/resume: %w", err)
		}
		threadAgg = resp.Thread
	} else {
		var resp appserverproto.ThreadStartResponse
		if err := e.client.RequestTyped(ctx, "thread/start", appserverproto.ThreadStartParams{}, &resp); err != nil {
			return "", fmt.Errorf("tui: thread/start: %w", err)
		}
		threadAgg = resp.Thread
	}
	return threadIDFromAggregate(threadAgg)
}

// StartNewThread spawns a fresh thread, optionally overriding the model slug
// (thread/start params.Model). The new thread becomes the active thread. It
// backs /new (empty model keeps the configured default) and the /model picker
// (the override makes the model→provider routing take effect immediately).
func (e *Engine) StartNewThread(ctx context.Context, model string) (string, error) {
	params := appserverproto.ThreadStartParams{}
	if model != "" {
		params.Model = &model
	}
	var resp appserverproto.ThreadStartResponse
	if err := e.client.RequestTyped(ctx, "thread/start", params, &resp); err != nil {
		return "", fmt.Errorf("tui: thread/start: %w", err)
	}
	threadID, err := threadIDFromAggregate(resp.Thread)
	if err != nil {
		return "", err
	}
	e.mu.Lock()
	e.threadID = threadID
	e.mu.Unlock()
	return threadID, nil
}

// SubmitTurn submits user input as a new turn on the active thread.
func (e *Engine) SubmitTurn(ctx context.Context, items []appserverproto.UserInput, outputSchema json.RawMessage) error {
	threadID := e.ThreadID()
	if threadID == "" {
		return fmt.Errorf("tui: submit turn: no active thread")
	}
	return e.submitTurnTo(ctx, threadID, items, outputSchema)
}

// submitTurnTo submits user input to the named thread.
func (e *Engine) submitTurnTo(ctx context.Context, threadID string, items []appserverproto.UserInput, outputSchema json.RawMessage) error {
	var resp appserverproto.TurnStartResponse
	err := e.client.RequestTyped(ctx, "turn/start", appserverproto.TurnStartParams{
		ThreadID:     threadID,
		Input:        items,
		OutputSchema: outputSchema,
	}, &resp)
	if err != nil {
		return fmt.Errorf("tui: turn/start: %w", err)
	}
	return nil
}

// SubmitCommand routes a typed [AppCommand] to the engine. User turns become
// turn/start submissions; other commands map onto their engine RPCs. The
// targetThreadID overrides the active thread when non-empty (mirroring
// SubmitThreadOp).
func (e *Engine) SubmitCommand(ctx context.Context, cmd AppCommand, targetThreadID string) error {
	threadID := targetThreadID
	if threadID == "" {
		threadID = e.ThreadID()
	}
	switch cmd.Kind {
	case AppCommandUserTurn:
		if threadID == "" {
			return fmt.Errorf("tui: submit user turn: no active thread")
		}
		return e.submitTurnTo(ctx, threadID, cmd.Items, cmd.OutputSchema)
	case AppCommandInterrupt:
		if threadID == "" {
			return fmt.Errorf("tui: interrupt: no active thread")
		}
		var resp appserverproto.TurnInterruptResponse
		if err := e.client.RequestTyped(ctx, "turn/interrupt", appserverproto.TurnInterruptParams{
			ThreadID: threadID,
			TurnID:   cmd.TurnID,
		}, &resp); err != nil {
			return fmt.Errorf("tui: turn/interrupt: %w", err)
		}
		return nil
	case AppCommandShutdown:
		e.client.Shutdown(context.WithoutCancel(ctx))
		return nil
	default:
		// Area-owned commands (approvals, elicitations, review, rollback, ...)
		// are submitted by their owning area via the typed client directly. The
		// foundation acknowledges them without erroring so unrouted commands do
		// not crash the loop.
		return nil
	}
}

// ListMcpTools fetches the connected MCP tools (codexgo extension method) for
// the deterministic slash→tool-call entry. Returns nil with no error when no
// MCP servers are connected.
func (e *Engine) ListMcpTools(ctx context.Context) ([]appserverproto.McpToolDescriptor, error) {
	var resp appserverproto.McpListToolsResponse
	if err := e.client.RequestTyped(ctx, "mcp/listTools", appserverproto.McpListToolsParams{}, &resp); err != nil {
		return nil, fmt.Errorf("tui: mcp/listTools: %w", err)
	}
	return resp.Tools, nil
}

// CallMcpTool invokes one MCP tool deterministically (no LLM turn) by its
// canonical "mcp__<server>__<tool>" name and returns the flattened result.
func (e *Engine) CallMcpTool(ctx context.Context, qualifiedName string, arguments json.RawMessage) (appserverproto.McpCallToolResponse, error) {
	var resp appserverproto.McpCallToolResponse
	if err := e.client.RequestTyped(ctx, "mcp/callTool", appserverproto.McpCallToolParams{
		QualifiedName: qualifiedName,
		Arguments:     arguments,
	}, &resp); err != nil {
		return appserverproto.McpCallToolResponse{}, fmt.Errorf("tui: mcp/callTool %s: %w", qualifiedName, err)
	}
	return resp, nil
}

// Pump drains the engine event stream, decoding each `codex/event` notification
// into a [CoreEventMsg] and delivering it to the model via the sender. It runs
// until ctx is cancelled or the stream closes, then sends [EngineClosedMsg].
//
// Pump blocks; run it in its own goroutine. It is safe to start once.
func (e *Engine) Pump(ctx context.Context) {
	e.mu.Lock()
	if e.pumping {
		e.mu.Unlock()
		return
	}
	e.pumping = true
	e.mu.Unlock()

	for {
		ev, ok := e.client.NextEvent(ctx)
		if !ok {
			e.sender.SendMsg(EngineClosedMsg{})
			return
		}
		if ev.Error != nil {
			e.sender.SendMsg(EngineErrorMsg{Err: fmt.Errorf("tui: engine error: code=%d message=%s", ev.Error.Error.Code, ev.Error.Error.Message)})
			continue
		}
		if !ev.IsCodexEvent() || ev.Notification == nil {
			continue
		}
		var coreEvent protocol.Event
		if err := json.Unmarshal(ev.Notification.Params, &coreEvent); err != nil {
			e.sender.SendMsg(EngineErrorMsg{Err: fmt.Errorf("tui: decode engine event: %w", err)})
			continue
		}
		e.sender.SendMsg(CoreEventMsg{Event: coreEvent})
	}
}

// Shutdown stops the engine client.
func (e *Engine) Shutdown(ctx context.Context) {
	e.client.Shutdown(ctx)
}

// threadIDFromAggregate extracts the "id" field from a thread aggregate payload.
func threadIDFromAggregate(raw json.RawMessage) (string, error) {
	var agg struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &agg); err != nil {
		return "", fmt.Errorf("tui: decode thread aggregate: %w", err)
	}
	if agg.ID == "" {
		return "", fmt.Errorf("tui: thread aggregate has empty id")
	}
	return agg.ID, nil
}
