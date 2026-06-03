package mcpserver

import (
	"encoding/json"
	"sync"

	"github.com/sqlrush/codexgo/internal/appserverproto"
)

// bridgeSink adapts the MCP [frameWriter] to the app-server [appserver.OutgoingSink]
// interface. The app-server emits messages in its own dialect (no "jsonrpc"
// field); the bridge re-encodes them into MCP frames (with "jsonrpc": "2.0") so
// the delegated v2/v1 responses, errors, and notifications travel over the same
// transport as the MCP-native messages.
//
// It also supports suppressing responses/errors for specific request ids: the
// MCP initialize handshake is advanced through the app-server processor, but its
// app-server-shaped response must be dropped because the MCP server sends its own
// MCP-shaped initialize response.
type bridgeSink struct {
	writer frameWriter

	mu       sync.Mutex
	suppress map[string]struct{}
}

// newBridgeSink wraps writer in a bridge sink.
func newBridgeSink(writer frameWriter) *bridgeSink {
	return &bridgeSink{writer: writer, suppress: make(map[string]struct{})}
}

// suppressResponse marks the response/error for id to be dropped instead of
// written. It is used so the app-server initialize reply is not duplicated.
func (s *bridgeSink) suppressResponse(id appserverproto.RequestId) {
	s.mu.Lock()
	s.suppress[id.String()] = struct{}{}
	s.mu.Unlock()
}

// isSuppressed reports whether id's response should be dropped, consuming the
// mark so it applies only once.
func (s *bridgeSink) isSuppressed(id appserverproto.RequestId) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.suppress[id.String()]
	if ok {
		delete(s.suppress, id.String())
	}
	return ok
}

// Send re-encodes an app-server JSON-RPC message into an MCP frame and writes it.
func (s *bridgeSink) Send(msg appserverproto.JSONRPCMessage) error {
	switch msg.Kind {
	case appserverproto.MessageKindResponse:
		if s.isSuppressed(msg.Response.ID) {
			return nil
		}
		return s.writer.WriteFrame(responseFrame{
			JSONRPC: jsonRPCVersion,
			ID:      toMCPRequestID(msg.Response.ID),
			Result:  msg.Response.Result,
		})
	case appserverproto.MessageKindError:
		if s.isSuppressed(msg.Error.ID) {
			return nil
		}
		id := toMCPRequestID(msg.Error.ID)
		return s.writer.WriteFrame(errorFrame{
			JSONRPC: jsonRPCVersion,
			ID:      &id,
			Error: errorBody{
				Code:    msg.Error.Error.Code,
				Message: msg.Error.Error.Message,
				Data:    msg.Error.Error.Data,
			},
		})
	case appserverproto.MessageKindNotification:
		return s.writer.WriteFrame(notificationFrame{
			JSONRPC: jsonRPCVersion,
			Method:  msg.Notification.Method,
			Params:  msg.Notification.Params,
		})
	case appserverproto.MessageKindRequest:
		return s.writer.WriteFrame(requestFrame{
			JSONRPC: jsonRPCVersion,
			ID:      toMCPRequestID(msg.Request.ID),
			Method:  msg.Request.Method,
			Params:  msg.Request.Params,
		})
	default:
		return nil
	}
}

// toMCPRequestID converts an app-server request id to the MCP request id type.
func toMCPRequestID(id appserverproto.RequestId) RequestID {
	if n, ok := id.Integer(); ok {
		return NewIntRequestID(n)
	}
	s, _ := id.StringValue()
	return NewStringRequestID(s)
}

// toAppServerRequestID converts an MCP request id to the app-server request id
// type, preserving the integer/string distinction.
func toAppServerRequestID(id RequestID) appserverproto.RequestId {
	if n, ok := id.Integer(); ok {
		return appserverproto.NewIntegerRequestId(n)
	}
	s, _ := id.StringValue()
	return appserverproto.NewStringRequestId(s)
}

// appserverRequest builds an app-server JSONRPCRequest from an MCP request id,
// method, and raw params.
func appserverRequest(id RequestID, method string, params json.RawMessage) appserverproto.JSONRPCRequest {
	return appserverproto.JSONRPCRequest{
		ID:     toAppServerRequestID(id),
		Method: method,
		Params: params,
	}
}
