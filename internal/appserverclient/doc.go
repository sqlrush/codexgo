// Package appserverclient provides clients for driving a Codex app-server.
//
// Two flavors are offered:
//
//   - [InProcessAppServerClient] embeds the engine directly: it constructs an
//     [appserver.Processor] in the same process and drives it without any
//     serialization round-trip. This is the client used by exec/TUI embedders.
//
//   - [RemoteAppServerClient] talks to an out-of-process app-server over a
//     newline-delimited JSON-RPC byte stream (stdio, UDS, or a piped child
//     process). It issues requests and surfaces server notifications.
//
// Both clients share the same request/response and event-stream surface so
// callers can be written against either. Requests are correlated by JSON-RPC
// id; server notifications (including the `codex/event` agent event stream) are
// delivered through the client's event channel.
package appserverclient
