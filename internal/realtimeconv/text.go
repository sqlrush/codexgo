package realtimeconv

import (
	"fmt"
	"strings"
)

// SessionKind discriminates the realtime protocol generation, mirroring the
// Rust RealtimeSessionKind enum. It governs V2-only behaviors such as text
// prefixing, audio truncation, response.create deduplication and handoff
// steering.
type SessionKind int

const (
	// SessionKindV1 is the original realtime protocol.
	SessionKindV1 SessionKind = iota
	// SessionKindV2 is the realtime v2 protocol with backend delegation.
	SessionKindV2
)

const (
	// UserTextPrefix tags user-typed text in V2 sessions. Mirrors the Rust
	// REALTIME_USER_TEXT_PREFIX.
	UserTextPrefix = "[USER] "
	// BackendTextPrefix tags background-agent output in V2 sessions. Mirrors the
	// Rust REALTIME_BACKEND_TEXT_PREFIX.
	BackendTextPrefix = "[BACKEND] "
)

// prefixText prepends prefix to text for V2 sessions only, and only when text is
// non-empty and not already prefixed. It mirrors the Rust prefix_realtime_text.
func prefixText(text, prefix string, kind SessionKind) string {
	if kind != SessionKindV2 || text == "" || strings.HasPrefix(text, prefix) {
		return text
	}
	return prefix + text
}

// PrefixV2Text prepends prefix to text using V2 semantics. It mirrors the Rust
// pub(crate) prefix_realtime_v2_text helper.
func PrefixV2Text(text, prefix string) string {
	return prefixText(text, prefix, SessionKindV2)
}

// escapeXMLText escapes the XML special characters &, < and >, matching the Rust
// escape_xml_text helper (which intentionally escapes only these three).
func escapeXMLText(input string) string {
	input = strings.ReplaceAll(input, "&", "&amp;")
	input = strings.ReplaceAll(input, "<", "&lt;")
	input = strings.ReplaceAll(input, ">", "&gt;")
	return input
}

// transcriptDeltaFromHandoff renders a handoff's active transcript as newline
// joined "role: text" lines, or returns ("", false) when empty. Mirrors the Rust
// realtime_transcript_delta_from_handoff.
func transcriptDeltaFromHandoff(h *HandoffRequested) (string, bool) {
	if h == nil || len(h.ActiveTranscript) == 0 {
		return "", false
	}
	lines := make([]string, 0, len(h.ActiveTranscript))
	for _, entry := range h.ActiveTranscript {
		lines = append(lines, fmt.Sprintf("%s: %s", entry.Role, entry.Text))
	}
	joined := strings.Join(lines, "\n")
	if joined == "" {
		return "", false
	}
	return joined, true
}

// textFromHandoffRequest prefers the explicit input transcript and falls back to
// the active transcript. Mirrors the Rust realtime_text_from_handoff_request.
func textFromHandoffRequest(h *HandoffRequested) (string, bool) {
	if h == nil {
		return "", false
	}
	if h.InputTranscript != "" {
		return h.InputTranscript, true
	}
	return transcriptDeltaFromHandoff(h)
}

// delegationFromHandoff builds the XML-wrapped delegation input routed to the
// background agent, or returns ("", false) when the handoff carries no usable
// text. Mirrors the Rust realtime_delegation_from_handoff.
func delegationFromHandoff(h *HandoffRequested) (string, bool) {
	input, ok := textFromHandoffRequest(h)
	if !ok {
		return "", false
	}
	transcriptDelta, _ := transcriptDeltaFromHandoff(h)
	return wrapDelegationInput(input, transcriptDelta), true
}

// wrapDelegationInput wraps input (and optional transcriptDelta) in the
// <realtime_delegation> XML envelope. Mirrors the Rust
// wrap_realtime_delegation_input.
func wrapDelegationInput(input, transcriptDelta string) string {
	input = escapeXMLText(input)
	if transcriptDelta != "" {
		transcriptDelta = escapeXMLText(transcriptDelta)
		return fmt.Sprintf(
			"<realtime_delegation>\n  <input>%s</input>\n  <transcript_delta>%s</transcript_delta>\n</realtime_delegation>",
			input, transcriptDelta,
		)
	}
	return fmt.Sprintf(
		"<realtime_delegation>\n  <input>%s</input>\n</realtime_delegation>",
		input,
	)
}

// DelegationFromHandoff exposes the routed delegation text for a handoff event,
// returning ok=false when the handoff carries no usable input. The fanout that
// routes realtime handoffs into a background agent uses this; it mirrors the
// Rust realtime_delegation_from_handoff call site in handle_start_inner.
func DelegationFromHandoff(h *HandoffRequested) (string, bool) {
	return delegationFromHandoff(h)
}
