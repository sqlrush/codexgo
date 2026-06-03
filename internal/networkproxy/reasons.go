// Package networkproxy is a native Go port of OpenAI codex's
// `codex-network-proxy` crate. It runs a per-turn HTTP(S) forward proxy and a
// SOCKS5 proxy on loopback that give sandboxed commands policy-controlled
// network access.
//
// The package is faithful to codex 0.136.0 for the two drop-in-critical
// surfaces: the policy decisions (allow/deny by host and unix-socket path under
// a default-deny posture) and the environment-variable contract emitted to
// child processes. The transport is hand-rolled on the Go standard library
// (net, net/http, net/http/httputil, crypto/tls) rather than the Rust `rama`
// framework.
package networkproxy

// Policy reason codes. These match the Rust `reasons` module exactly because
// they are surfaced verbatim in audit events, blocked-request telemetry, and
// the `x-proxy-error` response header mapping.
const (
	reasonDenied                = "denied"
	reasonMethodNotAllowed      = "method_not_allowed"
	reasonMitmHookDenied        = "mitm_hook_denied"
	reasonMitmRequired          = "mitm_required"
	reasonNotAllowed            = "not_allowed"
	reasonNotAllowedLocal       = "not_allowed_local"
	reasonPolicyDenied          = "policy_denied"
	reasonProxyDisabled         = "proxy_disabled"
	reasonUnixSocketUnsupported = "unix_socket_unsupported"
)
