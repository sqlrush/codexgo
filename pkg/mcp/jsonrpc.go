// Package mcp implements a hand-rolled Model Context Protocol (MCP) client that
// lets Codex connect to external MCP servers over stdio child processes and
// streamable HTTP. It is a focused Go port of codex's rmcp-client and
// codex-mcp connection-manager surface, built on the standard library plus the
// project's own internal packages.
//
// The protocol is JSON-RPC 2.0 (https://www.jsonrpc.org/specification) carried
// over a transport (see [Transport]). Requests carry a unique id and expect a
// matching response; notifications carry no id and expect no response. The MCP
// lifecycle and method shapes mirror the 2025-06-18 specification.
package mcp

import (
	"encoding/json"
	"fmt"
)

// JSONRPCVersion is the protocol version string carried on every JSON-RPC
// message. It matches the JSON-RPC 2.0 specification.
const JSONRPCVersion = "2.0"

// Request is a JSON-RPC 2.0 request object. A request always carries an id; a
// message without an id is a [Notification].
type Request struct {
	// ID identifies the request so the matching response can be correlated.
	ID json.RawMessage `json:"id"`
	// Method is the RPC method name (for example "tools/list").
	Method string `json:"method"`
	// Params is the optional method parameters object. It is omitted from the
	// wire form when nil.
	Params json.RawMessage `json:"params,omitempty"`
}

// MarshalJSON emits the request with the leading "jsonrpc" field, matching the
// canonical JSON-RPC 2.0 envelope.
func (r Request) MarshalJSON() ([]byte, error) {
	out := map[string]any{
		"jsonrpc": JSONRPCVersion,
		"id":      r.ID,
		"method":  r.Method,
	}
	if r.Params != nil {
		out["params"] = r.Params
	}
	return json.Marshal(out)
}

// Notification is a JSON-RPC 2.0 notification: a request with no id and no
// response.
type Notification struct {
	// Method is the notification method name (for example
	// "notifications/initialized").
	Method string `json:"method"`
	// Params is the optional notification parameters, omitted when nil.
	Params json.RawMessage `json:"params,omitempty"`
}

// MarshalJSON emits the notification envelope with the leading "jsonrpc" field
// and no id.
func (n Notification) MarshalJSON() ([]byte, error) {
	out := map[string]any{
		"jsonrpc": JSONRPCVersion,
		"method":  n.Method,
	}
	if n.Params != nil {
		out["params"] = n.Params
	}
	return json.Marshal(out)
}

// Response is a JSON-RPC 2.0 response object. Exactly one of Result or Error is
// populated by a well-formed server.
type Response struct {
	// ID echoes the originating request id. Server-initiated requests also use
	// this field; a nil id denotes a notification.
	ID json.RawMessage `json:"id"`
	// Method is set only for server-initiated requests and notifications
	// flowing back to the client (for example "elicitation/create").
	Method string `json:"method"`
	// Params carries the parameters for a server-initiated request or
	// notification.
	Params json.RawMessage `json:"params"`
	// Result is the successful response payload.
	Result json.RawMessage `json:"result"`
	// Error is the failure payload.
	Error *RPCError `json:"error"`
}

// IsResponse reports whether the message is a response to a client request
// (carries a result or error and no method).
func (r Response) IsResponse() bool {
	return r.Method == "" && (r.Result != nil || r.Error != nil)
}

// IsServerRequest reports whether the message is a server-initiated request
// (carries a method and an id).
func (r Response) IsServerRequest() bool {
	return r.Method != "" && len(r.ID) > 0 && string(r.ID) != "null"
}

// IsServerNotification reports whether the message is a server-initiated
// notification (carries a method and no id).
func (r Response) IsServerNotification() bool {
	return r.Method != "" && (len(r.ID) == 0 || string(r.ID) == "null")
}

// RPCError is the JSON-RPC 2.0 error object returned in a failed response.
type RPCError struct {
	// Code is the numeric error code.
	Code int `json:"code"`
	// Message is the short human-readable error description.
	Message string `json:"message"`
	// Data carries optional structured error details.
	Data json.RawMessage `json:"data,omitempty"`
}

// Error implements the error interface so an RPCError can be returned directly
// from client operations.
func (e *RPCError) Error() string {
	if e == nil {
		return "<nil rpc error>"
	}
	if len(e.Data) > 0 {
		return fmt.Sprintf("jsonrpc error %d: %s (%s)", e.Code, e.Message, string(e.Data))
	}
	return fmt.Sprintf("jsonrpc error %d: %s", e.Code, e.Message)
}

// newRequest builds a Request with the given numeric id, method, and params.
// Params may be nil. The id is encoded as a bare JSON number, matching the
// integer RequestId form used by codex.
func newRequest(id int64, method string, params any) (Request, error) {
	rawParams, err := marshalParams(params)
	if err != nil {
		return Request{}, err
	}
	idRaw, err := json.Marshal(id)
	if err != nil {
		return Request{}, fmt.Errorf("marshal request id: %w", err)
	}
	return Request{ID: idRaw, Method: method, Params: rawParams}, nil
}

// newNotification builds a Notification for the given method and params. Params
// may be nil.
func newNotification(method string, params any) (Notification, error) {
	rawParams, err := marshalParams(params)
	if err != nil {
		return Notification{}, err
	}
	return Notification{Method: method, Params: rawParams}, nil
}

// marshalParams encodes the params value to raw JSON, returning nil for a nil
// value so the "params" key is omitted entirely.
func marshalParams(params any) (json.RawMessage, error) {
	if params == nil {
		return nil, nil
	}
	if raw, ok := params.(json.RawMessage); ok {
		if len(raw) == 0 {
			return nil, nil
		}
		return raw, nil
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshal params: %w", err)
	}
	return encoded, nil
}
