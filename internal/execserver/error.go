package execserver

import "fmt"

// ExecServerError is an error surfaced by the [ExecProcess] / [ExecBackend]
// abstractions.
//
// Rust: the `ExecServerError` enum. This port models the subset produced by the
// in-process backend: a server-rejection carrying a JSON-RPC code and message
// (`ExecServerError::Server { code, message }`). The full client-side variants
// (transport, websocket, environment-registry) belong to the out-of-scope client
// and remote modules.
type ExecServerError struct {
	// Code is the JSON-RPC error code from the rejected request.
	Code int64
	// Message is the human-readable rejection message.
	Message string
}

// Error implements the error interface, matching the Rust Display for the
// Server variant: "exec-server rejected request ({code}): {message}".
func (e *ExecServerError) Error() string {
	return fmt.Sprintf("exec-server rejected request (%d): %s", e.Code, e.Message)
}
