package mcpserver

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// jsonRPCVersion is the protocol version emitted on every MCP frame. The MCP
// transport, unlike the in-process app-server dialect, always carries the
// "jsonrpc" field (rmcp's JsonRpcVersion2_0).
const jsonRPCVersion = "2.0"

// RequestID is a JSON-RPC request identifier: either a string or an integer, on
// the wire as a bare scalar (rmcp RequestId { String, Number }). It is modeled
// as a tagged scalar with custom JSON so the untagged round-trip is exact.
type RequestID struct {
	isInt bool
	str   string
	num   int64
}

// NewStringRequestID builds a string-valued id.
func NewStringRequestID(s string) RequestID { return RequestID{str: s} }

// NewIntRequestID builds an integer-valued id.
func NewIntRequestID(n int64) RequestID { return RequestID{isInt: true, num: n} }

// IsInteger reports whether the id holds an integer.
func (r RequestID) IsInteger() bool { return r.isInt }

// Integer returns the integer value and whether the id is integer-valued.
func (r RequestID) Integer() (int64, bool) { return r.num, r.isInt }

// StringValue returns the string value and whether the id is string-valued.
func (r RequestID) StringValue() (string, bool) { return r.str, !r.isInt }

// String renders the id for correlation, matching the rmcp Display impl: the
// integer formatted in base 10, the string verbatim.
func (r RequestID) String() string {
	if r.isInt {
		return strconv.FormatInt(r.num, 10)
	}
	return r.str
}

// MarshalJSON encodes the id as a bare number or bare string (untagged).
func (r RequestID) MarshalJSON() ([]byte, error) {
	if r.isInt {
		return json.Marshal(r.num)
	}
	return json.Marshal(r.str)
}

// UnmarshalJSON decodes the untagged id from a number or string. String is tried
// first to mirror serde's untagged variant order (String before Number).
func (r *RequestID) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*r = RequestID{str: s}
		return nil
	}
	var n int64
	if err := json.Unmarshal(data, &n); err == nil {
		*r = RequestID{isInt: true, num: n}
		return nil
	}
	return fmt.Errorf("mcpserver: RequestID must be a string or integer, got %s", string(data))
}

// incomingMessage is a frame received from the client. The MCP transport carries
// requests, responses (to server-initiated requests), notifications, and errors;
// they are distinguished structurally by which keys are present.
type incomingMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *RequestID      `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	Result  json.RawMessage `json:"result"`
	Error   json.RawMessage `json:"error"`
}

// incomingKind classifies a decoded frame.
type incomingKind int

const (
	incomingUnknown incomingKind = iota
	// incomingRequest is a client request (id + method present).
	incomingRequest
	// incomingNotification is a client notification (method present, no id).
	incomingNotification
	// incomingResponse is a response to a server-initiated request (id + result).
	incomingResponse
	// incomingError is an error to a server-initiated request (id + error).
	incomingError
)

// classify reports which JSON-RPC frame shape was received.
func (m incomingMessage) classify() incomingKind {
	hasMethod := m.Method != ""
	hasID := m.ID != nil
	switch {
	case hasMethod && hasID:
		return incomingRequest
	case hasMethod:
		return incomingNotification
	case len(m.Error) > 0:
		return incomingError
	case hasID && len(m.Result) > 0:
		return incomingResponse
	case hasID:
		// A bare {id, result: null} response.
		return incomingResponse
	default:
		return incomingUnknown
	}
}

// errorBody is the JSON-RPC error object.
type errorBody struct {
	Code    int64           `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// JSON-RPC error codes used by the MCP server, mirroring rmcp's ErrorCode.
const (
	codeInvalidRequest int64 = -32600
	codeMethodNotFound int64 = -32601
	codeInvalidParams  int64 = -32602
	codeInternalError  int64 = -32603
)

func newErrorBody(code int64, message string, data json.RawMessage) errorBody {
	return errorBody{Code: code, Message: message, Data: data}
}
