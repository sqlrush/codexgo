package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// responder produces a response for a decoded client request. Returning a nil
// slice means "no response" (used to drop notifications).
type responder func(req Response) []json.RawMessage

// fakeTransport is an in-memory [Transport] that runs a scripted MCP server. The
// client writes requests via Send; the transport decodes them, invokes the
// responder, and queues any resulting frames for Receive. It is safe for one
// concurrent reader and one concurrent writer, matching the Transport contract.
type fakeTransport struct {
	respond responder

	mu       sync.Mutex
	queue    [][]byte
	waiters  []chan []byte
	closed   bool
	closedCh chan struct{}

	// sent records every raw frame written by the client, for assertions.
	sentMu sync.Mutex
	sent   [][]byte
}

func newFakeTransport(respond responder) *fakeTransport {
	return &fakeTransport{
		respond:  respond,
		closedCh: make(chan struct{}),
	}
}

// pushServerMessage injects a server-initiated frame (request or notification)
// into the receive queue, simulating an unsolicited server message.
func (t *fakeTransport) pushServerMessage(raw json.RawMessage) {
	t.enqueue(raw)
}

func (t *fakeTransport) Send(_ context.Context, message []byte) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return ErrTransportClosed
	}
	t.mu.Unlock()

	cp := make([]byte, len(message))
	copy(cp, message)
	t.sentMu.Lock()
	t.sent = append(t.sent, cp)
	t.sentMu.Unlock()

	var req Response
	if err := json.Unmarshal(message, &req); err != nil {
		return fmt.Errorf("fakeTransport: decode client message: %w", err)
	}
	if t.respond != nil {
		for _, frame := range t.respond(req) {
			t.enqueue(frame)
		}
	}
	return nil
}

func (t *fakeTransport) enqueue(frame []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return
	}
	if len(t.waiters) > 0 {
		w := t.waiters[0]
		t.waiters = t.waiters[1:]
		w <- frame
		return
	}
	t.queue = append(t.queue, frame)
}

func (t *fakeTransport) Receive(ctx context.Context) ([]byte, error) {
	t.mu.Lock()
	if len(t.queue) > 0 {
		frame := t.queue[0]
		t.queue = t.queue[1:]
		t.mu.Unlock()
		return frame, nil
	}
	if t.closed {
		t.mu.Unlock()
		return nil, ErrTransportClosed
	}
	w := make(chan []byte, 1)
	t.waiters = append(t.waiters, w)
	t.mu.Unlock()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-t.closedCh:
		return nil, ErrTransportClosed
	case frame := <-w:
		return frame, nil
	}
}

func (t *fakeTransport) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	close(t.closedCh)
	t.mu.Unlock()
	return nil
}

// reqID extracts the integer id from a decoded request frame for use in replies.
func reqID(req Response) json.RawMessage {
	return req.ID
}

// resultFrame builds a JSON-RPC success response for the given request id and
// result payload.
func resultFrame(id json.RawMessage, result string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":%s}`, id, result))
}

// errorFrame builds a JSON-RPC error response for the given request id.
func errorFrame(id json.RawMessage, code int, message string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"error":{"code":%d,"message":%q}}`, id, code, message))
}

// scriptedServer returns a responder implementing a minimal MCP server: it
// answers initialize, tools/list (single page), tools/call, and resources/read,
// and drops the initialized notification.
func scriptedServer() responder {
	return func(req Response) []json.RawMessage {
		switch req.Method {
		case MethodInitialize:
			return []json.RawMessage{resultFrame(reqID(req), `{
				"protocolVersion":"2025-06-18",
				"capabilities":{},
				"serverInfo":{"name":"test-server","version":"1.2.3"},
				"instructions":"hello"
			}`)}
		case MethodToolsList:
			return []json.RawMessage{resultFrame(reqID(req), `{
				"tools":[
					{"name":"echo","inputSchema":{"type":"object"}},
					{"name":"secret","inputSchema":{"type":"object"}}
				]
			}`)}
		case MethodToolsCall:
			return []json.RawMessage{resultFrame(reqID(req), `{
				"content":[{"type":"text","text":"ok"}],
				"isError":false
			}`)}
		case MethodResourcesRead:
			return []json.RawMessage{resultFrame(reqID(req), `{
				"contents":[{"uri":"file:///x","text":"data"}]
			}`)}
		case MethodPing:
			return []json.RawMessage{resultFrame(reqID(req), `{}`)}
		case MethodInitializedNotify:
			return nil // notification: no reply
		default:
			return []json.RawMessage{errorFrame(reqID(req), -32601, "method not found: "+req.Method)}
		}
	}
}
