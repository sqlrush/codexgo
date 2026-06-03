// Package mcpserver exposes Codex as a Model Context Protocol (MCP) server over
// stdio JSON-RPC. It is the faithful Go port of the Rust codex mcp-server crate
// (codex-rs/mcp-server/src) plus the v2/v1 app-server surface that the MCP
// interface documents (docs/codex_mcp_interface.md).
//
// The MCP transport is standard JSON-RPC 2.0, line-delimited, with the
// "jsonrpc" field present on every frame (unlike the in-process app-server
// dialect). The server:
//
//   - implements the MCP protocol handshake (initialize, ping, tools/list,
//     tools/call) and tolerates the remaining read-only MCP requests.
//   - exposes two tools, "codex" and "codex-reply", which start and continue a
//     Codex session. Each returns a CallToolResult whose structuredContent
//     mirrors the text content alongside the threadId.
//   - streams live agent events as codex/event notifications, attaching a
//     _meta object carrying the originating requestId and threadId.
//   - issues server->client elicitation/create requests to obtain exec-command
//     and apply-patch approvals.
//   - routes the v2 (thread/*, turn/*, account/*, config/*, model/list,
//     app/list, collaborationMode/list) and v1 compatibility methods
//     (getConversationSummary, getAuthStatus, gitDiffToRemote, fuzzyFileSearch)
//     to a shared app-server [appserver.Processor] driving the same engine.
//
// The engine is assembled exactly as the app-server assembles it
// ([appserver.Assemble]); the MessageProcessor drives the [core.ThreadManager]
// it exposes, so the MCP server and app-server share one wire-compatible engine.
package mcpserver
