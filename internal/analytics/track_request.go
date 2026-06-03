package analytics

import (
	"encoding/json"
	"fmt"
)

// trackKind enumerates the supported untagged TrackEventRequest variants.
type trackKind int

const (
	trackSkillInvocation trackKind = iota
	trackHookRun
	trackTurnTokenUsage
	trackGuardianReview
	trackAppMentioned
	trackAppUsed
	trackAcceptedLines
)

// CodexTurnTokenUsageEventParams is the upload payload for turn token usage.
type CodexTurnTokenUsageEventParams struct {
	ThreadID              string `json:"thread_id"`
	TurnID                string `json:"turn_id"`
	InputTokens           *int64 `json:"input_tokens"`
	CachedInputTokens     *int64 `json:"cached_input_tokens"`
	OutputTokens          *int64 `json:"output_tokens"`
	ReasoningOutputTokens *int64 `json:"reasoning_output_tokens"`
	TotalTokens           *int64 `json:"total_tokens"`
}

// CodexTurnTokenUsageEventRequest wraps the turn-token-usage payload.
type CodexTurnTokenUsageEventRequest struct {
	EventType   string                         `json:"event_type"`
	EventParams CodexTurnTokenUsageEventParams `json:"event_params"`
}

// TrackEventRequest is the untagged union of upload request variants. Mirrors
// Rust `TrackEventRequest` (serde untagged). Exactly one inner pointer is set.
type TrackEventRequest struct {
	kind            trackKind
	skillInvocation *SkillInvocationEventRequest
	hookRun         *CodexHookRunEventRequest
	turnTokenUsage  *CodexTurnTokenUsageEventRequest
	guardianReview  *GuardianReviewEventRequest
	appMentioned    *CodexAppMentionedEventRequest
	appUsed         *CodexAppUsedEventRequest
	acceptedLines   *CodexAcceptedLineFingerprintsEventRequest
}

// MarshalJSON serializes the active variant directly (untagged). Mirrors serde
// untagged enum serialization.
func (t TrackEventRequest) MarshalJSON() ([]byte, error) {
	switch t.kind {
	case trackSkillInvocation:
		return json.Marshal(t.skillInvocation)
	case trackHookRun:
		return json.Marshal(t.hookRun)
	case trackTurnTokenUsage:
		return json.Marshal(t.turnTokenUsage)
	case trackGuardianReview:
		return json.Marshal(t.guardianReview)
	case trackAppMentioned:
		return json.Marshal(t.appMentioned)
	case trackAppUsed:
		return json.Marshal(t.appUsed)
	case trackAcceptedLines:
		return json.Marshal(t.acceptedLines)
	default:
		return nil, fmt.Errorf("analytics: unknown TrackEventRequest kind %d", t.kind)
	}
}

// ShouldSendInIsolatedRequest reports whether this event must be uploaded on its
// own. Mirrors Rust `TrackEventRequest::should_send_in_isolated_request`.
func (t TrackEventRequest) ShouldSendInIsolatedRequest() bool {
	return t.kind == trackAcceptedLines
}

// TrackEventsRequest is the upload envelope. Mirrors Rust `TrackEventsRequest`.
type TrackEventsRequest struct {
	Events []TrackEventRequest `json:"events"`
}

// trackEventRequestBatches splits events into upload batches, isolating any
// event that requires its own request. Mirrors Rust
// `track_event_request_batches`.
func trackEventRequestBatches(events []TrackEventRequest) [][]TrackEventRequest {
	var batches [][]TrackEventRequest
	var current []TrackEventRequest

	for _, event := range events {
		if event.ShouldSendInIsolatedRequest() {
			if len(current) > 0 {
				batches = append(batches, current)
				current = nil
			}
			batches = append(batches, []TrackEventRequest{event})
		} else {
			current = append(current, event)
		}
	}
	if len(current) > 0 {
		batches = append(batches, current)
	}
	return batches
}
