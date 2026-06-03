// Package appserverproto is a faithful, drop-in-compatible Go port of the Codex
// 0.136.0 app-server-protocol crate (JSON-RPC 2.0 dialect). Method-name strings
// and payload JSON match the Rust reference byte-for-byte after key-order
// canonicalization, because real IDE and CLI clients depend on it.
//
// # JSON-RPC dialect
//
// The protocol is "JSON-RPC 2.0"-shaped but does NOT carry the "jsonrpc": "2.0"
// field on the wire (matching the Rust jsonrpc_lite module). The envelope types
// (JSONRPCRequest, JSONRPCNotification, JSONRPCResponse, JSONRPCError) and the
// RequestId scalar live in this package.
//
// # Registry instead of a giant enum
//
// The Rust crate generates a single ClientRequest enum (with per-variant params
// and response types) via the client_request_definitions! macro. Go has no such
// macro, so this package replaces the enum with a runtime registry:
//
//   - Method params/result constructors register via Register(MethodSpec{...}).
//   - ServerNotification payload constructors register via
//     RegisterNotification(NotificationSpec{...}).
//   - ServerRequest params/result constructors register via
//     RegisterServerRequest(ServerRequestSpec{...}).
//
// Area agents call the Register* functions from init() in their own files, so
// the dispatch tables are assembled at package-load time. Lookup* functions
// resolve a wire method name back to its constructors for decoding.
package appserverproto
