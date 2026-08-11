package mcp

import "fmt"

// Protocol version strings advertised during the initialize handshake.
// Port of the reference `rmcp::model::ProtocolVersion` values codexgo speaks.
const (
	// ProtocolVersion20250618 is the legacy revision codexgo has always used.
	ProtocolVersion20250618 = "2025-06-18"
	// ProtocolVersion20260728 is the MCP 2026-07-28 revision (spec 49 need 1:
	// paginated discovery, multi-round requests, non-blocking startup).
	ProtocolVersion20260728 = "2026-07-28"

	// StdioProtocolVersionEnv opts a stdio MCP server into the modern revision
	// (brand-isolated rename of upstream CODEX_MCP_PROTOCOL_VERSION; see
	// internal/brand). Accepts only "2026-07-28".
	StdioProtocolVersionEnv = "CODEXGO_MCP_PROTOCOL_VERSION"
)

// ProtocolMode is the MCP compatibility policy selected once per session.
// Port of upstream `McpProtocolMode` (rmcp-client/src/protocol_mode.rs).
// The zero value is Legacy, matching upstream's Default.
type ProtocolMode int

const (
	// ProtocolModeLegacy preserves the existing 2025-06-18 initialization.
	ProtocolModeLegacy ProtocolMode = iota
	// ProtocolModeV20260728 enables the MCP 2026-07-28 discovery/request lifecycle,
	// negotiating down to 2025-06-18 when the server does not support it.
	ProtocolModeV20260728
)

// PreferredProtocolVersion returns the newest version this policy will offer.
func (m ProtocolMode) PreferredProtocolVersion() string {
	if m == ProtocolModeV20260728 {
		return ProtocolVersion20260728
	}
	return ProtocolVersion20250618
}

// supportedVersions is the set this policy accepts in an initialize response,
// newest first. Legacy accepts only the legacy revision; the modern policy
// accepts the modern revision and negotiates down to legacy.
func (m ProtocolMode) supportedVersions() []string {
	if m == ProtocolModeV20260728 {
		return []string{ProtocolVersion20260728, ProtocolVersion20250618}
	}
	return []string{ProtocolVersion20250618}
}

// NegotiateVersion validates the server's chosen protocolVersion against this
// policy. An empty server version means the server omitted it (older behavior);
// codexgo falls back to the policy's legacy revision. A server-selected version
// outside the supported set is a fatal handshake mismatch.
//
// Mirrors the rmcp SDK's ClientLifecycleMode::Auto negotiation: offer preferred,
// accept legacy fallback, reject anything else.
func (m ProtocolMode) NegotiateVersion(serverVersion string) (string, error) {
	if serverVersion == "" {
		return ProtocolVersion20250618, nil
	}
	for _, v := range m.supportedVersions() {
		if v == serverVersion {
			return serverVersion, nil
		}
	}
	return "", fmt.Errorf("mcp: server selected unsupported protocol version %q (supported: %v)",
		serverVersion, m.supportedVersions())
}

// StdioMode resolves the protocol mode for a stdio MCP server from the
// requested version env value. Port of upstream `stdio_mode`:
//   - Legacy policy stays Legacy regardless of the request.
//   - No request → Legacy.
//   - Modern policy + "2026-07-28" → V20260728.
//   - Any other explicit request → error.
func (m ProtocolMode) StdioMode(requestedVersion string) (ProtocolMode, error) {
	switch {
	case m == ProtocolModeLegacy:
		return ProtocolModeLegacy, nil
	case requestedVersion == "":
		return ProtocolModeLegacy, nil
	case requestedVersion == ProtocolVersion20260728:
		return ProtocolModeV20260728, nil
	default:
		return ProtocolModeLegacy, fmt.Errorf(
			"unsupported %s %q for stdio MCP server; expected %q",
			StdioProtocolVersionEnv, requestedVersion, ProtocolVersion20260728)
	}
}

// ServerSupportedVersions is the set codexgo (acting as an MCP server) accepts
// from clients; it echoes the client's requested version when supported and
// falls back to the legacy default otherwise.
func ServerSupportedVersions() []string {
	return []string{ProtocolVersion20260728, ProtocolVersion20250618}
}

// NegotiateServerVersion picks the version codexgo-as-server will echo: the
// client's request when supported, else the legacy default.
func NegotiateServerVersion(clientRequested string) string {
	for _, v := range ServerSupportedVersions() {
		if v == clientRequested {
			return clientRequested
		}
	}
	return ProtocolVersion20250618
}
