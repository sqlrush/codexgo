package core

import (
	"net/http"

	"github.com/sqlrush/codexgo/internal/api"
)

// buildResponsesOptions assembles the per-request [api.ResponsesOptions],
// including the routing / turn-metadata headers and the session source used for
// the subagent header. It is the port of the Rust
// `ModelClientSession::build_responses_options` (HTTP path).
//
// STUB: request compression negotiation, attestation headers, and the
// inference-trace request headers are deferred. Compression is always None.
func (c *ResponsesModelClient) buildResponsesOptions() api.ResponsesOptions {
	sessionID := c.cfg.SessionID.String()
	threadID := c.cfg.ThreadID.String()

	headers := c.buildResponsesHeaders()
	c.extendIdentityHeaders(headers)

	return api.ResponsesOptions{
		SessionID:     &sessionID,
		ThreadID:      &threadID,
		SessionSource: c.cfg.SessionSource,
		ExtraHeaders:  headers,
		Compression:   api.CompressionNone,
		SetTurnState:  c.SetTurnState,
	}
}

// buildResponsesHeaders builds the Codex-specific request headers (beta
// features, sticky turn-state, turn metadata, beta protocol, timing metrics),
// mirroring the Rust `build_responses_headers` plus the per-request additions in
// `build_responses_options` / `build_websocket_headers`.
func (c *ResponsesModelClient) buildResponsesHeaders() http.Header {
	headers := http.Header{}

	if c.cfg.BetaFeaturesHeader != "" {
		headers.Set(headerCodexBetaFeatures, c.cfg.BetaFeaturesHeader)
	}
	if state := c.currentTurnState(); state != "" {
		headers.Set(headerCodexTurnState, state)
	}
	if md := c.turnMetadataHeader(); md != "" {
		headers.Set(headerCodexTurnMetadata, md)
	}

	// Protocol beta marker is always present, mirroring the Rust
	// build_websocket_headers / responses identity wiring.
	headers.Set(headerOpenAIBeta, responsesWebsocketsV2Beta)
	if c.cfg.IncludeTimingMetrics {
		headers.Set(headerResponsesAPITiming, "true")
	}
	headers.Set(headerCodexInstallationID, c.cfg.InstallationID)
	return headers
}

// extendIdentityHeaders adds the subagent / parent-thread / window-id routing
// headers, mirroring `build_responses_identity_headers` + `build_subagent_headers`.
func (c *ResponsesModelClient) extendIdentityHeaders(headers http.Header) {
	if sub, ok := c.subagentHeaderValue(); ok && sub != "" {
		headers.Set(headerOpenAISubagent, sub)
	}
	if parent := c.parentThreadIDHeaderValue(); parent != "" {
		headers.Set(headerCodexParentThreadID, parent)
	}
	headers.Set(headerCodexWindowID, c.currentWindowID())
}

// turnMetadataHeader returns the per-turn metadata header value. The
// turn-running subset does not yet thread per-turn metadata from the turn
// runner, so this returns the empty string.
//
// STUB: per-turn metadata propagation (parse_turn_metadata_header) is deferred
// until the turn runner supplies it; the wiring point is preserved here.
func (c *ResponsesModelClient) turnMetadataHeader() string { return "" }

// subagentHeaderValue computes the x-openai-subagent value from the session
// source, mirroring the Rust `subagent_header_value`. It reuses the api layer's
// subagent classification so the values stay in lockstep.
func (c *ResponsesModelClient) subagentHeaderValue() (string, bool) {
	src := c.cfg.SessionSource
	if src == nil || !src.IsSubAgent {
		return "", false
	}
	switch src.SubAgent.Kind {
	case api.SubAgentReview:
		return "review", true
	case api.SubAgentCompact:
		return "compact", true
	case api.SubAgentMemoryConsolidation:
		return "memory_consolidation", true
	case api.SubAgentThreadSpawn:
		return "collab_spawn", true
	case api.SubAgentOther:
		return src.SubAgent.Label, true
	default:
		return "", false
	}
}

// parentThreadIDHeaderValue returns the parent-thread id routing header value.
// The api SessionSource subset does not model the parent-thread id, so this
// returns the empty string.
//
// STUB: parent-thread id propagation (parent_thread_id_header_value) is deferred
// until the SessionSource carries the parent thread id.
func (c *ResponsesModelClient) parentThreadIDHeaderValue() string { return "" }
