package appserver

import (
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// CodexEventMethod is the JSON-RPC notification method used to stream raw core
// engine events to clients. It carries the serialized [protocol.Event] payload,
// matching the wire format the MCP/in-process paths emit
// (codex-rs/mcp-server/src/outgoing_message.rs).
const CodexEventMethod = "codex/event"

// OutgoingSink receives server-to-client JSON-RPC messages produced while a
// request is processed: responses, errors, and notifications. Transports
// implement it to serialize messages onto their byte stream; the in-process
// client implements it to deliver typed messages directly.
//
// Send must be safe for concurrent use: the event-forwarding goroutine and the
// request handlers can both write through the same sink.
type OutgoingSink interface {
	// Send delivers one server-to-client message to the peer.
	Send(msg appserverproto.JSONRPCMessage) error
}

// sendResponse marshals result and delivers a JSON-RPC response for id.
func sendResponse(sink OutgoingSink, id appserverproto.RequestId, result any) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("appserver: marshal response result: %w", err)
	}
	return sink.Send(appserverproto.NewResponseMessage(appserverproto.JSONRPCResponse{
		ID:     id,
		Result: raw,
	}))
}

// sendError delivers a JSON-RPC error for id.
func sendError(sink OutgoingSink, id appserverproto.RequestId, rpcErr *RPCError) error {
	return sink.Send(appserverproto.NewErrorMessage(appserverproto.JSONRPCError{
		ID:    id,
		Error: rpcErr.Body(),
	}))
}

// sendNotification delivers a JSON-RPC notification with the given method and
// already-marshaled params.
func sendNotification(sink OutgoingSink, method string, params json.RawMessage) error {
	return sink.Send(appserverproto.NewNotificationMessage(appserverproto.JSONRPCNotification{
		Method: method,
		Params: params,
	}))
}

// sendCodexEvent forwards a core engine [protocol.Event] to the client as a
// `codex/event` notification. The serialized params is the Event itself
// (id + msg), matching the reference wire format clients consume.
func sendCodexEvent(sink OutgoingSink, ev protocol.Event) error {
	raw, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("appserver: marshal codex event: %w", err)
	}
	return sendNotification(sink, CodexEventMethod, raw)
}
