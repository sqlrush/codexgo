package execserver

import (
	"github.com/sqlrush/codexgo/internal/appserverproto"
)

// JSON-RPC error codes used by the executor process and filesystem services.
//
// Rust: the integer codes in the rpc.rs error constructors.
const (
	codeInvalidRequest int64 = -32600
	codeMethodNotFound int64 = -32601
	codeInvalidParams  int64 = -32602
	codeNotFound       int64 = -32004
	codeInternalError  int64 = -32603
)

// rpcError builds a JSON-RPC error body with no data, matching the Rust helpers
// which always set `data: None`.
func rpcError(code int64, message string) *appserverproto.JSONRPCErrorBody {
	return &appserverproto.JSONRPCErrorBody{Code: code, Message: message}
}

// invalidRequest mirrors the Rust `invalid_request` helper (code -32600).
func invalidRequest(message string) *appserverproto.JSONRPCErrorBody {
	return rpcError(codeInvalidRequest, message)
}

// methodNotFound mirrors the Rust `method_not_found` helper (code -32601).
func methodNotFound(message string) *appserverproto.JSONRPCErrorBody {
	return rpcError(codeMethodNotFound, message)
}

// invalidParams mirrors the Rust `invalid_params` helper (code -32602).
func invalidParams(message string) *appserverproto.JSONRPCErrorBody {
	return rpcError(codeInvalidParams, message)
}

// notFound mirrors the Rust `not_found` helper (code -32004).
func notFound(message string) *appserverproto.JSONRPCErrorBody {
	return rpcError(codeNotFound, message)
}

// internalError mirrors the Rust `internal_error` helper (code -32603).
func internalError(message string) *appserverproto.JSONRPCErrorBody {
	return rpcError(codeInternalError, message)
}
