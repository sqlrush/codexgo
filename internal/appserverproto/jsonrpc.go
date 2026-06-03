package appserverproto

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// JSONRPCVersion is the nominal protocol version. NOTE: this dialect neither
// sends nor expects the "jsonrpc" field on the wire; the constant exists only
// for parity with the Rust reference (jsonrpc_lite::JSONRPC_VERSION).
const JSONRPCVersion = "2.0"

// RequestId is a JSON-RPC request identifier. On the wire it is either a string
// or an integer (Rust: untagged enum RequestId { String(String), Integer(i64) }).
//
// We model it as a tagged scalar with custom JSON so the untagged round-trip is
// exact: integers serialize as bare numbers, strings as bare strings.
type RequestId struct {
	isInt bool
	str   string
	num   int64
}

// NewStringRequestId builds a string-valued RequestId.
func NewStringRequestId(s string) RequestId { return RequestId{isInt: false, str: s} }

// NewIntegerRequestId builds an integer-valued RequestId.
func NewIntegerRequestId(n int64) RequestId { return RequestId{isInt: true, num: n} }

// IsInteger reports whether the id holds an integer.
func (r RequestId) IsInteger() bool { return r.isInt }

// Integer returns the integer value and whether the id is integer-valued.
func (r RequestId) Integer() (int64, bool) { return r.num, r.isInt }

// StringValue returns the string value and whether the id is string-valued.
func (r RequestId) StringValue() (string, bool) { return r.str, !r.isInt }

// String renders the id for logging/correlation, matching the Rust Display impl
// (the integer formatted in base 10, the string verbatim).
func (r RequestId) String() string {
	if r.isInt {
		return strconv.FormatInt(r.num, 10)
	}
	return r.str
}

// MarshalJSON encodes the id as a bare number or bare string (untagged).
func (r RequestId) MarshalJSON() ([]byte, error) {
	if r.isInt {
		return json.Marshal(r.num)
	}
	return json.Marshal(r.str)
}

// UnmarshalJSON decodes the untagged id from a number or string. String is tried
// first to mirror serde's untagged variant order (String before Integer).
func (r *RequestId) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*r = RequestId{isInt: false, str: s}
		return nil
	}
	var n int64
	if err := json.Unmarshal(data, &n); err == nil {
		*r = RequestId{isInt: true, num: n}
		return nil
	}
	return fmt.Errorf("appserverproto: RequestId must be a string or integer, got %s", string(data))
}

// Result is the JSON value carried in a successful response. Mirrors the Rust
// type alias `pub type Result = serde_json::Value`.
type Result = json.RawMessage

// JSONRPCRequest is a request that expects a response. Mirrors the Rust struct;
// "params" and "trace" are omitted when absent (serde skip_serializing_if).
type JSONRPCRequest struct {
	ID     RequestId                 `json:"id"`
	Method string                    `json:"method"`
	Params json.RawMessage           `json:"params,omitempty"`
	Trace  *protocol.W3cTraceContext `json:"trace,omitempty"`
}

// JSONRPCNotification is a notification that does not expect a response.
type JSONRPCNotification struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse is a successful (non-error) response to a request.
type JSONRPCResponse struct {
	ID     RequestId       `json:"id"`
	Result json.RawMessage `json:"result"`
}

// JSONRPCError is a response indicating an error occurred. The field order
// (error before id) matches the Rust struct declaration order.
type JSONRPCError struct {
	Error JSONRPCErrorBody `json:"error"`
	ID    RequestId        `json:"id"`
}

// JSONRPCErrorBody is the error object inside a JSONRPCError. "data" is omitted
// when absent (serde skip_serializing_if).
type JSONRPCErrorBody struct {
	Code    int64           `json:"code"`
	Data    json.RawMessage `json:"data,omitempty"`
	Message string          `json:"message"`
}
