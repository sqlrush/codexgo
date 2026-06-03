// Package appserver implements the Codex app-server: the JSON-RPC request
// processor that drives a [core.Codex]/[core.ThreadManager] engine and streams
// agent events back to clients as server notifications.
//
// It is a reduced, faithful port of codex-rs/app-server, prioritizing the
// turn-driving path:
//
//   - Initialize     (v1 handshake)
//   - thread/start   (spawn a new thread)
//   - thread/resume  (resume from history)
//   - thread/fork    (fork an existing thread)
//   - turn/start     (submit user input, driving a model turn)
//   - turn/interrupt (cancel the active turn)
//   - fs/*           (filesystem access via internal/filesystem)
//
// Core engine events are forwarded to the client as `codex/event`
// notifications carrying the serialized [protocol.Event] (the same wire format
// the MCP/in-process paths use), so real clients that consume the event stream
// keep working unchanged.
//
// The package is transport-agnostic: a [Processor] consumes typed
// [appserverproto.JSONRPCRequest] values and emits [appserverproto.JSONRPCMessage]
// values through an [OutgoingSink]. The stdio, UDS, and WebSocket transports
// adapt byte streams onto that interface.
//
// # Assembly
//
// [Assemble] builds the manager set (ToolRouter, ModelsManager, ThreadManager,
// and the optional MCP/Skills/Plugins/Hooks managers) from a resolved
// configuration plus a model-client factory. It follows the codex init order:
// validate inputs, build the per-thread services factory, then the thread
// manager.
package appserver
