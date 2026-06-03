package mcpserver

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// codexEventMethod is the JSON-RPC notification method used to stream raw core
// engine events to clients (codex-rs/mcp-server/src/outgoing_message.rs).
const codexEventMethod = "codex/event"

// frameWriter delivers one serialized JSON-RPC frame to the client. It must be
// safe for concurrent use: approval tasks, event forwarders, and request
// handlers all write through the same writer.
type frameWriter interface {
	// WriteFrame serializes one JSON object and writes it as a single line.
	WriteFrame(v any) error
}

// outgoingSender sends server-to-client messages and tracks callbacks for
// server-initiated requests. It is the faithful port of the Rust
// OutgoingMessageSender: send_request registers a oneshot channel keyed by a
// monotonically allocated request id, and notify_client_response resolves it.
type outgoingSender struct {
	writer frameWriter

	nextID atomic.Int64

	mu        sync.Mutex
	callbacks map[string]chan json.RawMessage
}

// newOutgoingSender wraps writer in a sender with an empty callback table.
func newOutgoingSender(writer frameWriter) *outgoingSender {
	return &outgoingSender{
		writer:    writer,
		callbacks: make(map[string]chan json.RawMessage),
	}
}

// notificationFrame is a server->client notification on the wire.
type notificationFrame struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// requestFrame is a server->client request on the wire.
type requestFrame struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      RequestID       `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// responseFrame is a server->client response on the wire.
type responseFrame struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      RequestID       `json:"id"`
	Result  json.RawMessage `json:"result"`
}

// errorFrame is a server->client error on the wire.
type errorFrame struct {
	JSONRPC string     `json:"jsonrpc"`
	ID      *RequestID `json:"id"`
	Error   errorBody  `json:"error"`
}

// sendResponse marshals result and writes a JSON-RPC response for id. When the
// result fails to marshal, an internal error is written instead so the client is
// never left waiting.
func (s *outgoingSender) sendResponse(id RequestID, result any) {
	raw, err := json.Marshal(result)
	if err != nil {
		s.sendError(id, newErrorBody(codeInternalError, fmt.Sprintf("failed to serialize response: %v", err), nil))
		return
	}
	_ = s.writer.WriteFrame(responseFrame{JSONRPC: jsonRPCVersion, ID: id, Result: raw})
}

// sendError writes a JSON-RPC error for id.
func (s *outgoingSender) sendError(id RequestID, body errorBody) {
	idCopy := id
	_ = s.writer.WriteFrame(errorFrame{JSONRPC: jsonRPCVersion, ID: &idCopy, Error: body})
}

// sendNotification writes a JSON-RPC notification with already-marshaled params.
func (s *outgoingSender) sendNotification(method string, params json.RawMessage) {
	_ = s.writer.WriteFrame(notificationFrame{JSONRPC: jsonRPCVersion, Method: method, Params: params})
}

// sendRequest writes a server->client request and returns a channel that
// receives the client's response result. The id is allocated monotonically.
// Mirrors OutgoingMessageSender::send_request.
func (s *outgoingSender) sendRequest(method string, params json.RawMessage) <-chan json.RawMessage {
	id := NewIntRequestID(s.nextID.Add(1) - 1)
	ch := make(chan json.RawMessage, 1)

	s.mu.Lock()
	s.callbacks[id.String()] = ch
	s.mu.Unlock()

	_ = s.writer.WriteFrame(requestFrame{JSONRPC: jsonRPCVersion, ID: id, Method: method, Params: params})
	return ch
}

// notifyClientResponse resolves the callback registered for id with result.
// Mirrors OutgoingMessageSender::notify_client_response. Unknown ids are dropped.
func (s *outgoingSender) notifyClientResponse(id RequestID, result json.RawMessage) {
	s.mu.Lock()
	ch, ok := s.callbacks[id.String()]
	if ok {
		delete(s.callbacks, id.String())
	}
	s.mu.Unlock()
	if !ok {
		return
	}
	// The channel is buffered with capacity 1, so this never blocks.
	ch <- result
	close(ch)
}

// outgoingMeta is the _meta object attached to codex/event notifications so a
// client can correlate streamed events with the originating tool call and
// thread. It mirrors the Rust OutgoingNotificationMeta (camelCase fields).
type outgoingMeta struct {
	// RequestID is the originating MCP request id, when one applies.
	RequestID *RequestID `json:"requestId,omitempty"`
	// ThreadID is the thread the event belongs to, when known.
	ThreadID *string `json:"threadId,omitempty"`
}

// sendEventAsNotification serializes a core engine event into the codex/event
// notification params, flattening the event (id + msg) and attaching the _meta
// object under "_meta". It is the faithful port of
// OutgoingMessageSender::send_event_as_notification.
func (s *outgoingSender) sendEventAsNotification(ev protocol.Event, meta *outgoingMeta) {
	params, err := buildEventParams(ev, meta)
	if err != nil {
		// Fall back to the bare event so the stream still carries the payload.
		if raw, e := json.Marshal(ev); e == nil {
			s.sendNotification(codexEventMethod, raw)
		}
		return
	}
	s.sendNotification(codexEventMethod, params)
}

// buildEventParams flattens the event object and (when present) injects the
// _meta key, matching the Rust OutgoingNotificationParams (flatten event + meta).
func buildEventParams(ev protocol.Event, meta *outgoingMeta) (json.RawMessage, error) {
	eventJSON, err := json.Marshal(ev)
	if err != nil {
		return nil, fmt.Errorf("mcpserver: marshal event: %w", err)
	}
	if meta == nil {
		return eventJSON, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(eventJSON, &obj); err != nil {
		return nil, fmt.Errorf("mcpserver: decode event object: %w", err)
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("mcpserver: marshal meta: %w", err)
	}
	obj["_meta"] = metaJSON
	merged, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("mcpserver: re-encode event params: %w", err)
	}
	return merged, nil
}
