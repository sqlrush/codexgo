package networkproxy

import "github.com/sqlrush/codexgo/pkg/protocol"

// NetworkProtocol identifies the transport that a policy decision applies to.
// It mirrors the Rust `NetworkProtocol` enum and is distinct from
// protocol.NetworkApprovalProtocol (the approval-facing wire type) which is
// reused for cross-package interop via ApprovalProtocol.
type NetworkProtocol int

const (
	// ProtocolHTTP is a plain (non-CONNECT) HTTP forward-proxy request.
	ProtocolHTTP NetworkProtocol = iota
	// ProtocolHTTPSConnect is an HTTPS CONNECT tunnel request.
	ProtocolHTTPSConnect
	// ProtocolSocks5TCP is a SOCKS5 TCP CONNECT request.
	ProtocolSocks5TCP
	// ProtocolSocks5UDP is a SOCKS5 UDP ASSOCIATE request.
	ProtocolSocks5UDP
)

// PolicyProtocol returns the canonical snake_case string used in audit events,
// matching the Rust `as_policy_protocol` mapping.
func (p NetworkProtocol) PolicyProtocol() string {
	switch p {
	case ProtocolHTTP:
		return "http"
	case ProtocolHTTPSConnect:
		return "https_connect"
	case ProtocolSocks5TCP:
		return "socks5_tcp"
	case ProtocolSocks5UDP:
		return "socks5_udp"
	default:
		return "http"
	}
}

// ApprovalProtocol maps the transport protocol onto the shared protocol package
// approval type, reusing existing wire types instead of redefining them.
func (p NetworkProtocol) ApprovalProtocol() protocol.NetworkApprovalProtocol {
	switch p {
	case ProtocolHTTP:
		return protocol.NetworkApprovalProtocolHTTP
	case ProtocolHTTPSConnect:
		return protocol.NetworkApprovalProtocolHTTPS
	case ProtocolSocks5TCP:
		return protocol.NetworkApprovalProtocolSocks5TCP
	case ProtocolSocks5UDP:
		return protocol.NetworkApprovalProtocolSocks5UDP
	default:
		return protocol.NetworkApprovalProtocolHTTP
	}
}
