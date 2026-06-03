// Package realtimeconv ports codex-rs's realtime_conversation loop. It drives a
// realtime/voice session — audio in/out, transcript deltas, and background-agent
// turn handoff — over a transport (WebSocket or the WebRTC sideband from
// internal/realtimewebrtc) and maps server activity to internal/protocol
// realtime EventMsg variants.
//
// The reference Rust crate layers this loop on top of codex_api's realtime
// websocket endpoint (RealtimeWebsocketClient / RealtimeWebsocketWriter /
// RealtimeWebsocketEvents). Those transport types are not part of the Go port's
// internal/api surface, so this package depends on small, locally-defined
// transport interfaces (see transport.go). Callers wire any transport that
// satisfies them; tests use an in-memory fake.
package realtimeconv

import (
	"github.com/sqlrush/codexgo/internal/protocol"
)

// EventKind discriminates the variants of Event. It mirrors the Rust
// codex_protocol::protocol::RealtimeEvent enum.
type EventKind string

const (
	// EventKindSessionUpdated reports that the realtime session was (re)configured.
	EventKindSessionUpdated EventKind = "session_updated"
	// EventKindInputAudioSpeechStarted reports detected user speech onset.
	EventKindInputAudioSpeechStarted EventKind = "input_audio_speech_started"
	// EventKindInputTranscriptDelta carries an incremental user transcript.
	EventKindInputTranscriptDelta EventKind = "input_transcript_delta"
	// EventKindInputTranscriptDone carries a finalized user transcript.
	EventKindInputTranscriptDone EventKind = "input_transcript_done"
	// EventKindOutputTranscriptDelta carries an incremental model transcript.
	EventKindOutputTranscriptDelta EventKind = "output_transcript_delta"
	// EventKindOutputTranscriptDone carries a finalized model transcript.
	EventKindOutputTranscriptDone EventKind = "output_transcript_done"
	// EventKindAudioOut carries a chunk of model audio output.
	EventKindAudioOut EventKind = "audio_out"
	// EventKindResponseCreated reports that a response has begun.
	EventKindResponseCreated EventKind = "response_created"
	// EventKindResponseCancelled reports that a response was cancelled.
	EventKindResponseCancelled EventKind = "response_cancelled"
	// EventKindResponseDone reports that a response finished.
	EventKindResponseDone EventKind = "response_done"
	// EventKindConversationItemAdded reports a new conversation item (raw JSON).
	EventKindConversationItemAdded EventKind = "conversation_item_added"
	// EventKindConversationItemDone reports a finalized conversation item.
	EventKindConversationItemDone EventKind = "conversation_item_done"
	// EventKindHandoffRequested reports a background-agent delegation request.
	EventKindHandoffRequested EventKind = "handoff_requested"
	// EventKindNoopRequested reports a no-op function call the server expects to
	// be acknowledged.
	EventKindNoopRequested EventKind = "noop_requested"
	// EventKindError carries a realtime stream error message.
	EventKindError EventKind = "error"
)

// TranscriptDelta carries an incremental transcript chunk. Mirrors the Rust
// RealtimeTranscriptDelta.
type TranscriptDelta struct {
	Delta string
}

// TranscriptDone carries a finalized transcript. Mirrors the Rust
// RealtimeTranscriptDone.
type TranscriptDone struct {
	Text string
}

// TranscriptEntry is a single (role, text) line of conversation transcript.
// Mirrors the Rust RealtimeTranscriptEntry.
type TranscriptEntry struct {
	Role string
	Text string
}

// HandoffRequested carries a background-agent delegation request. Mirrors the
// Rust RealtimeHandoffRequested.
type HandoffRequested struct {
	HandoffID        string
	ItemID           string
	InputTranscript  string
	ActiveTranscript []TranscriptEntry
}

// NoopRequested carries a no-op function call that must be acknowledged. Mirrors
// the Rust RealtimeNoopRequested.
type NoopRequested struct {
	CallID string
	ItemID string
}

// InputAudioSpeechStarted reports user speech onset. Mirrors the Rust
// RealtimeInputAudioSpeechStarted. ItemID is optional (nil when absent).
type InputAudioSpeechStarted struct {
	ItemID *string
}

// ResponseLifecycle carries the optional response id shared by the
// ResponseCreated, ResponseCancelled and ResponseDone variants. Mirrors the
// equivalent Rust structs which each wrap an Option<String> response_id.
type ResponseLifecycle struct {
	ResponseID *string
}

// SessionUpdated reports a (re)configured realtime session. Mirrors the Rust
// RealtimeEvent::SessionUpdated variant fields.
type SessionUpdated struct {
	RealtimeSessionID string
	Instructions      *string
}

// Event is the Go analogue of the Rust codex_protocol RealtimeEvent enum. The
// active payload field depends on Kind. Exactly one payload field is populated
// for each Kind (none for ConversationItemDone, which only carries ItemID).
type Event struct {
	Kind EventKind

	// SessionUpdated is set for EventKindSessionUpdated.
	SessionUpdated *SessionUpdated
	// InputAudioSpeechStarted is set for EventKindInputAudioSpeechStarted.
	InputAudioSpeechStarted *InputAudioSpeechStarted
	// TranscriptDelta is set for the Input/Output TranscriptDelta kinds.
	TranscriptDelta *TranscriptDelta
	// TranscriptDone is set for the Input/Output TranscriptDone kinds.
	TranscriptDone *TranscriptDone
	// AudioOut is set for EventKindAudioOut.
	AudioOut *protocol.RealtimeAudioFrame
	// Response is set for the ResponseCreated/Cancelled/Done kinds.
	Response *ResponseLifecycle
	// ConversationItem is the raw item JSON for EventKindConversationItemAdded.
	ConversationItem []byte
	// ItemID is set for EventKindConversationItemDone.
	ItemID string
	// Handoff is set for EventKindHandoffRequested.
	Handoff *HandoffRequested
	// Noop is set for EventKindNoopRequested.
	Noop *NoopRequested
	// ErrorMessage is set for EventKindError.
	ErrorMessage string
}

// NewError returns an Error event carrying message. It mirrors the Rust
// RealtimeEvent::Error(String) constructor used throughout the loop.
func NewError(message string) Event {
	return Event{Kind: EventKindError, ErrorMessage: message}
}

// IsError reports whether the event is an Error variant.
func (e Event) IsError() bool { return e.Kind == EventKindError }
