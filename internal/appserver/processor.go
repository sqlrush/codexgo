package appserver

import (
	"context"
	"encoding/json"
	"runtime"
	"sync"

	"github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/pkg/core"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// InitializeMethod is the v1 handshake method name. It is handled specially,
// before any registered-method dispatch, mirroring the Rust
// ClientRequest::Initialize fast-path.
const InitializeMethod = "initialize"

// Defaults carries the per-session default settings the processor applies when a
// request does not override them: the model, provider, cwd, and base
// instructions. They seed every spawned thread's [core.SessionConfiguration].
type Defaults struct {
	// Model is the default model slug.
	Model string
	// ProviderID is the default model provider id.
	ProviderID string
	// Cwd is the default working directory for spawned threads.
	Cwd string
	// BaseInstructions is the default base instruction text.
	BaseInstructions string
	// UserAgent is reported in the initialize handshake response.
	UserAgent string

	// IncludeEnvironmentContext seeds the codex `<permissions instructions>` +
	// `<environment_context>` messages into a new thread's history at session start.
	IncludeEnvironmentContext bool
	// SandboxMode is the effective filesystem sandbox mode for those messages.
	SandboxMode protocol.SandboxMode
	// NetworkAccessEnabled toggles the rendered network-access word.
	NetworkAccessEnabled bool
	// ApprovalPolicy is the default approval policy for spawned threads. The zero
	// value (empty Kind) falls back to on-request; `codex exec` sets it to never.
	ApprovalPolicy protocol.AskForApproval
}

// Processor handles client JSON-RPC requests against a [core] engine and streams
// engine events back as server notifications. It is the reduced, faithful port
// of the Rust MessageProcessor, focused on the turn-driving path.
//
// A Processor is bound to a single logical connection (one client). Concurrent
// requests are dispatched on their own goroutines, but each request's response
// and the thread event stream are serialized onto the [OutgoingSink], which must
// itself be safe for concurrent use.
type Processor struct {
	assembly *Assembly
	defaults Defaults
	sink     OutgoingSink
	fs       *fsHandler

	// mu guards forwarders.
	mu sync.Mutex
	// forwarders maps a thread id to its running event forwarder so interrupts
	// can register against it and shutdown can stop them.
	forwarders map[string]*eventForwarder
}

// ProcessorConfig bundles the dependencies for a [Processor].
type ProcessorConfig struct {
	// Assembly is the constructed engine; required.
	Assembly *Assembly
	// Defaults are the per-session default settings; required.
	Defaults Defaults
	// Sink delivers server-to-client messages; required.
	Sink OutgoingSink
}

// NewProcessor constructs a [Processor]. The caller owns the sink lifecycle.
func NewProcessor(cfg ProcessorConfig) *Processor {
	return &Processor{
		assembly:   cfg.Assembly,
		defaults:   cfg.Defaults,
		sink:       cfg.Sink,
		fs:         newFsHandler(),
		forwarders: make(map[string]*eventForwarder),
	}
}

// HandleRequest processes one client request. It decodes the typed params,
// dispatches to the matching handler, and writes the response or error to the
// sink. The initialize handshake is handled first; all other methods require the
// connection to be initialized.
//
// HandleRequest never returns an error to the caller: every failure is rendered
// as a JSON-RPC error response on the sink, matching the Rust behavior where
// handlers report through send_error.
func (p *Processor) HandleRequest(ctx context.Context, conn *Conn, req appserverproto.JSONRPCRequest) {
	if req.Method == InitializeMethod {
		if err := p.handleInitialize(conn, req); err != nil {
			_ = sendError(p.sink, req.ID, err)
		}
		return
	}

	if !conn.isInitialized() {
		_ = sendError(p.sink, req.ID, invalidRequest("Not initialized"))
		return
	}

	result, rpcErr := p.dispatch(ctx, conn, req)
	if rpcErr != nil {
		_ = sendError(p.sink, req.ID, rpcErr)
		return
	}
	if result == nil {
		// Handlers that defer their response (e.g. turn/interrupt resolved on
		// abort) return a nil result and answer later through the sink.
		return
	}
	// If the response fails to marshal or send, surface it as a JSON-RPC error so
	// the client is never left waiting for a reply that will not arrive.
	if err := sendResponse(p.sink, req.ID, result); err != nil {
		_ = sendError(p.sink, req.ID, internalError("failed to encode response: %v", err))
	}
}

// dispatch routes an initialized request to its handler, returning the response
// value (nil when the handler defers its response) or an RPCError.
func (p *Processor) dispatch(ctx context.Context, conn *Conn, req appserverproto.JSONRPCRequest) (any, *RPCError) {
	params, err := appserverproto.DecodeClientRequestParams(req)
	if err != nil {
		// Unknown method or undecodable params. Distinguish the two so the wire
		// error code matches the reference.
		if _, ok := appserverproto.Lookup(req.Method); !ok {
			return nil, methodNotFound("unknown method %q", req.Method)
		}
		return nil, invalidParams("invalid params for %q: %v", req.Method, err)
	}

	switch req.Method {
	case "thread/start":
		return p.handleThreadStart(ctx, conn, params.(*appserverproto.ThreadStartParams))
	case "thread/resume":
		return p.handleThreadResume(ctx, conn, params.(*appserverproto.ThreadResumeParams))
	case "thread/fork":
		return p.handleThreadFork(ctx, conn, params.(*appserverproto.ThreadForkParams))
	case "turn/start":
		return p.handleTurnStart(ctx, params.(*appserverproto.TurnStartParams))
	case "turn/interrupt":
		return p.handleTurnInterrupt(ctx, req.ID, params.(*appserverproto.TurnInterruptParams))
	case "fs/readFile":
		return p.fs.readFile(ctx, params.(*appserverproto.FsReadFileParams))
	case "fs/writeFile":
		return p.fs.writeFile(ctx, params.(*appserverproto.FsWriteFileParams))
	case "fs/createDirectory":
		return p.fs.createDirectory(ctx, params.(*appserverproto.FsCreateDirectoryParams))
	case "fs/getMetadata":
		return p.fs.getMetadata(ctx, params.(*appserverproto.FsGetMetadataParams))
	case "fs/readDirectory":
		return p.fs.readDirectory(ctx, params.(*appserverproto.FsReadDirectoryParams))
	case "fs/remove":
		return p.fs.remove(ctx, params.(*appserverproto.FsRemoveParams))
	case "fs/copy":
		return p.fs.copy(ctx, params.(*appserverproto.FsCopyParams))
	case "mcp/listTools":
		return p.handleMcpListTools(params.(*appserverproto.McpListToolsParams))
	case "mcp/callTool":
		return p.handleMcpCallTool(ctx, params.(*appserverproto.McpCallToolParams))
	default:
		// The method is registered but not implemented in this turn-driving subset.
		return nil, methodNotFound("method %q is not implemented", req.Method)
	}
}

// handleInitialize processes the v1 initialize handshake. It records the
// negotiated capabilities on the connection and replies with the platform/user
// agent metadata.
func (p *Processor) handleInitialize(conn *Conn, req appserverproto.JSONRPCRequest) *RPCError {
	var params appserverproto.InitializeParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return invalidParams("invalid initialize params: %v", err)
		}
	}

	experimentalAPI := false
	if params.Capabilities != nil {
		experimentalAPI = params.Capabilities.ExperimentalApi
	}
	if !conn.markInitialized(params.ClientInfo.Name, params.ClientInfo.Version, experimentalAPI) {
		return invalidRequest("Already initialized")
	}

	resp := appserverproto.InitializeResponse{
		UserAgent:      p.defaults.UserAgent,
		CodexHome:      protocol.AbsolutePath(p.assembly.CodexHome),
		PlatformFamily: platformFamily(),
		PlatformOs:     runtime.GOOS,
	}
	if err := sendResponse(p.sink, req.ID, resp); err != nil {
		return internalError("send initialize response: %v", err)
	}
	return nil
}

// registerForwarder starts an event forwarder for a freshly spawned thread and
// records it so interrupts and shutdown can reach it.
//
// The thread manager consumes the SessionConfigured ack during spawn (it is
// returned on [core.NewThread]), so the forwarder's NextEvent loop would never
// see it. To keep the client's event stream faithful — SessionConfigured is the
// first codex/event — the captured ack is re-emitted here before the loop
// starts.
func (p *Processor) registerForwarder(ctx context.Context, newThread core.NewThread) *eventForwarder {
	thread := newThread.Thread
	f := newEventForwarder(ctx, thread, p.sink)
	p.mu.Lock()
	p.forwarders[thread.ThreadID().String()] = f
	p.mu.Unlock()

	// Re-emit the captured SessionConfigured ack as the first codex/event.
	configured := newThread.SessionConfigured
	_ = sendCodexEvent(p.sink, protocol.Event{
		ID: core.InitialSubmitID,
		Msg: protocol.EventMsg{
			Type:              protocol.EventMsgKindSessionConfigured,
			SessionConfigured: &configured,
		},
	})

	go f.Run()
	return f
}

// forwarderFor returns the running forwarder for threadID, if any.
func (p *Processor) forwarderFor(threadID string) (*eventForwarder, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	f, ok := p.forwarders[threadID]
	return f, ok
}

// Shutdown stops every running event forwarder and shuts down their threads. It
// is best-effort and bounded by ctx.
func (p *Processor) Shutdown(ctx context.Context) {
	p.mu.Lock()
	forwarders := make([]*eventForwarder, 0, len(p.forwarders))
	for _, f := range p.forwarders {
		forwarders = append(forwarders, f)
	}
	p.forwarders = make(map[string]*eventForwarder)
	p.mu.Unlock()

	for _, f := range forwarders {
		_ = f.thread.ShutdownAndWait(ctx)
		f.Stop()
	}
}

// platformFamily reports the OS family ("unix" or "windows"), mirroring the Rust
// std::env::consts::FAMILY value.
func platformFamily() string {
	if runtime.GOOS == "windows" {
		return "windows"
	}
	return "unix"
}
