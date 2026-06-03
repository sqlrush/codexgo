package appserver

import (
	"fmt"

	"github.com/sqlrush/codexgo/internal/appserverproto"
)

// JSON-RPC error codes, mirroring codex-rs/app-server/src/error_code.rs.
const (
	// InvalidRequestCode is the JSON-RPC "invalid request" code.
	InvalidRequestCode int64 = -32600
	// MethodNotFoundCode is the JSON-RPC "method not found" code.
	MethodNotFoundCode int64 = -32601
	// InvalidParamsCode is the JSON-RPC "invalid params" code.
	InvalidParamsCode int64 = -32602
	// InternalErrorCode is the JSON-RPC "internal error" code.
	InternalErrorCode int64 = -32603
	// OverloadedErrorCode signals the server is overloaded.
	OverloadedErrorCode int64 = -32001
)

// RPCError is a typed JSON-RPC error carrying a code and message. It is the Go
// analogue of the Rust JSONRPCErrorError and is returned by request handlers so
// the dispatcher can render a JSONRPCError envelope.
type RPCError struct {
	// Code is the JSON-RPC error code.
	Code int64
	// Message is the human-readable error message.
	Message string
}

// Error implements the error interface.
func (e *RPCError) Error() string { return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message) }

// Body converts the error into the wire error body.
func (e *RPCError) Body() appserverproto.JSONRPCErrorBody {
	return appserverproto.JSONRPCErrorBody{Code: e.Code, Message: e.Message}
}

// invalidRequest builds an invalid-request RPCError.
func invalidRequest(format string, args ...any) *RPCError {
	return &RPCError{Code: InvalidRequestCode, Message: fmt.Sprintf(format, args...)}
}

// methodNotFound builds a method-not-found RPCError.
func methodNotFound(format string, args ...any) *RPCError {
	return &RPCError{Code: MethodNotFoundCode, Message: fmt.Sprintf(format, args...)}
}

// invalidParams builds an invalid-params RPCError.
func invalidParams(format string, args ...any) *RPCError {
	return &RPCError{Code: InvalidParamsCode, Message: fmt.Sprintf(format, args...)}
}

// internalError builds an internal-error RPCError.
func internalError(format string, args ...any) *RPCError {
	return &RPCError{Code: InternalErrorCode, Message: fmt.Sprintf(format, args...)}
}
