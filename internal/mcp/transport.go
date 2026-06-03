package mcp

import "context"

// Transport is the byte-level boundary between the MCP client and a server. A
// transport carries JSON-RPC messages in both directions: the client writes
// requests and notifications, and reads responses, server-initiated requests,
// and notifications.
//
// Implementations must be safe for a single concurrent reader (Receive) and a
// single concurrent writer (Send); the [Client] serializes access on each side.
type Transport interface {
	// Send delivers a single JSON-RPC message (already framed by the caller as
	// a complete JSON object) to the server. It must not block indefinitely;
	// callers pass a context with the operation deadline.
	Send(ctx context.Context, message []byte) error

	// Receive blocks until the next JSON-RPC message arrives from the server,
	// returning the raw message bytes. It returns a non-nil error when the
	// transport is closed or the context is cancelled. A clean end-of-stream is
	// reported as [ErrTransportClosed].
	Receive(ctx context.Context) ([]byte, error)

	// Close shuts the transport down, terminating any owned child process and
	// releasing resources. Close is idempotent.
	Close() error
}
