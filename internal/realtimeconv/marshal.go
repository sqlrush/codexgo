package realtimeconv

import (
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// wireTranscriptDelta is the JSON shape of RealtimeTranscriptDelta.
type wireTranscriptDelta struct {
	Delta string `json:"delta"`
}

// wireTranscriptDone is the JSON shape of RealtimeTranscriptDone.
type wireTranscriptDone struct {
	Text string `json:"text"`
}

// wireTranscriptEntry is the JSON shape of RealtimeTranscriptEntry.
type wireTranscriptEntry struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// wireSpeechStarted is the JSON shape of RealtimeInputAudioSpeechStarted.
type wireSpeechStarted struct {
	ItemID *string `json:"item_id"`
}

// wireResponseLifecycle is the JSON shape shared by the response lifecycle
// structs (RealtimeResponse{Created,Cancelled,Done}).
type wireResponseLifecycle struct {
	ResponseID *string `json:"response_id"`
}

// wireSessionUpdated is the JSON shape of the SessionUpdated struct variant.
type wireSessionUpdated struct {
	RealtimeSessionID string  `json:"realtime_session_id"`
	Instructions      *string `json:"instructions"`
}

// wireHandoffRequested is the JSON shape of RealtimeHandoffRequested.
type wireHandoffRequested struct {
	HandoffID        string                `json:"handoff_id"`
	ItemID           string                `json:"item_id"`
	InputTranscript  string                `json:"input_transcript"`
	ActiveTranscript []wireTranscriptEntry `json:"active_transcript"`
}

// wireNoopRequested is the JSON shape of RealtimeNoopRequested.
type wireNoopRequested struct {
	CallID string `json:"call_id"`
	ItemID string `json:"item_id"`
}

// wireConversationItemDone is the JSON shape of the ConversationItemDone struct
// variant.
type wireConversationItemDone struct {
	ItemID string `json:"item_id"`
}

// variantName returns the externally-tagged serde variant key for the event,
// matching the default Serialize representation of the Rust RealtimeEvent enum.
func variantName(kind EventKind) (string, error) {
	switch kind {
	case EventKindSessionUpdated:
		return "SessionUpdated", nil
	case EventKindInputAudioSpeechStarted:
		return "InputAudioSpeechStarted", nil
	case EventKindInputTranscriptDelta:
		return "InputTranscriptDelta", nil
	case EventKindInputTranscriptDone:
		return "InputTranscriptDone", nil
	case EventKindOutputTranscriptDelta:
		return "OutputTranscriptDelta", nil
	case EventKindOutputTranscriptDone:
		return "OutputTranscriptDone", nil
	case EventKindAudioOut:
		return "AudioOut", nil
	case EventKindResponseCreated:
		return "ResponseCreated", nil
	case EventKindResponseCancelled:
		return "ResponseCancelled", nil
	case EventKindResponseDone:
		return "ResponseDone", nil
	case EventKindConversationItemAdded:
		return "ConversationItemAdded", nil
	case EventKindConversationItemDone:
		return "ConversationItemDone", nil
	case EventKindHandoffRequested:
		return "HandoffRequested", nil
	case EventKindNoopRequested:
		return "NoopRequested", nil
	case EventKindError:
		return "Error", nil
	default:
		return "", fmt.Errorf("realtimeconv: unknown event kind %q", kind)
	}
}

// eventPayload returns the inner JSON payload for an event variant, ready to be
// nested under its variant key.
func (e Event) eventPayload() (json.RawMessage, error) {
	switch e.Kind {
	case EventKindSessionUpdated:
		su := e.SessionUpdated
		if su == nil {
			su = &SessionUpdated{}
		}
		return marshalRaw(wireSessionUpdated{
			RealtimeSessionID: su.RealtimeSessionID,
			Instructions:      su.Instructions,
		})
	case EventKindInputAudioSpeechStarted:
		var itemID *string
		if e.InputAudioSpeechStarted != nil {
			itemID = e.InputAudioSpeechStarted.ItemID
		}
		return marshalRaw(wireSpeechStarted{ItemID: itemID})
	case EventKindInputTranscriptDelta, EventKindOutputTranscriptDelta:
		var delta string
		if e.TranscriptDelta != nil {
			delta = e.TranscriptDelta.Delta
		}
		return marshalRaw(wireTranscriptDelta{Delta: delta})
	case EventKindInputTranscriptDone, EventKindOutputTranscriptDone:
		var text string
		if e.TranscriptDone != nil {
			text = e.TranscriptDone.Text
		}
		return marshalRaw(wireTranscriptDone{Text: text})
	case EventKindAudioOut:
		frame := protocol.RealtimeAudioFrame{}
		if e.AudioOut != nil {
			frame = *e.AudioOut
		}
		return marshalRaw(frame)
	case EventKindResponseCreated, EventKindResponseCancelled, EventKindResponseDone:
		var responseID *string
		if e.Response != nil {
			responseID = e.Response.ResponseID
		}
		return marshalRaw(wireResponseLifecycle{ResponseID: responseID})
	case EventKindConversationItemAdded:
		if len(e.ConversationItem) == 0 {
			return json.RawMessage("null"), nil
		}
		return json.RawMessage(e.ConversationItem), nil
	case EventKindConversationItemDone:
		return marshalRaw(wireConversationItemDone{ItemID: e.ItemID})
	case EventKindHandoffRequested:
		return marshalRaw(handoffToWire(e.Handoff))
	case EventKindNoopRequested:
		noop := wireNoopRequested{}
		if e.Noop != nil {
			noop = wireNoopRequested{CallID: e.Noop.CallID, ItemID: e.Noop.ItemID}
		}
		return marshalRaw(noop)
	case EventKindError:
		return marshalRaw(e.ErrorMessage)
	default:
		return nil, fmt.Errorf("realtimeconv: unknown event kind %q", e.Kind)
	}
}

// MarshalJSON encodes the event using the externally-tagged representation of
// the Rust RealtimeEvent enum: {"VariantName": <payload>}.
func (e Event) MarshalJSON() ([]byte, error) {
	name, err := variantName(e.Kind)
	if err != nil {
		return nil, err
	}
	payload, err := e.eventPayload()
	if err != nil {
		return nil, err
	}
	wrapper := map[string]json.RawMessage{name: payload}
	out, err := json.Marshal(wrapper)
	if err != nil {
		return nil, fmt.Errorf("realtimeconv: marshal event: %w", err)
	}
	return out, nil
}

// UnmarshalJSON decodes an externally-tagged RealtimeEvent value, mirroring the
// default Deserialize representation of the Rust enum. It is the inverse of
// MarshalJSON and lets a transport that already parsed wire frames hand back
// canonical events (or callers round-trip a serialized payload).
func (e *Event) UnmarshalJSON(data []byte) error {
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return fmt.Errorf("realtimeconv: decode event: %w", err)
	}
	if len(wrapper) != 1 {
		return fmt.Errorf("realtimeconv: expected exactly one event variant, got %d", len(wrapper))
	}

	var (
		name    string
		payload json.RawMessage
	)
	for k, v := range wrapper {
		name, payload = k, v
	}

	out, err := decodeEventVariant(name, payload)
	if err != nil {
		return err
	}
	*e = out
	return nil
}

// decodeEventVariant decodes a single externally-tagged variant payload into an
// Event. It is the inverse mapping of eventPayload / variantName.
func decodeEventVariant(name string, payload json.RawMessage) (Event, error) {
	switch name {
	case "SessionUpdated":
		var w wireSessionUpdated
		if err := json.Unmarshal(payload, &w); err != nil {
			return Event{}, decodeErr(name, err)
		}
		return Event{Kind: EventKindSessionUpdated, SessionUpdated: &SessionUpdated{
			RealtimeSessionID: w.RealtimeSessionID,
			Instructions:      w.Instructions,
		}}, nil
	case "InputAudioSpeechStarted":
		var w wireSpeechStarted
		if err := json.Unmarshal(payload, &w); err != nil {
			return Event{}, decodeErr(name, err)
		}
		return Event{Kind: EventKindInputAudioSpeechStarted, InputAudioSpeechStarted: &InputAudioSpeechStarted{ItemID: w.ItemID}}, nil
	case "InputTranscriptDelta", "OutputTranscriptDelta":
		var w wireTranscriptDelta
		if err := json.Unmarshal(payload, &w); err != nil {
			return Event{}, decodeErr(name, err)
		}
		kind := EventKindInputTranscriptDelta
		if name == "OutputTranscriptDelta" {
			kind = EventKindOutputTranscriptDelta
		}
		return Event{Kind: kind, TranscriptDelta: &TranscriptDelta{Delta: w.Delta}}, nil
	case "InputTranscriptDone", "OutputTranscriptDone":
		var w wireTranscriptDone
		if err := json.Unmarshal(payload, &w); err != nil {
			return Event{}, decodeErr(name, err)
		}
		kind := EventKindInputTranscriptDone
		if name == "OutputTranscriptDone" {
			kind = EventKindOutputTranscriptDone
		}
		return Event{Kind: kind, TranscriptDone: &TranscriptDone{Text: w.Text}}, nil
	case "AudioOut":
		var frame protocol.RealtimeAudioFrame
		if err := json.Unmarshal(payload, &frame); err != nil {
			return Event{}, decodeErr(name, err)
		}
		return Event{Kind: EventKindAudioOut, AudioOut: &frame}, nil
	case "ResponseCreated", "ResponseCancelled", "ResponseDone":
		var w wireResponseLifecycle
		if err := json.Unmarshal(payload, &w); err != nil {
			return Event{}, decodeErr(name, err)
		}
		kind := EventKindResponseCreated
		switch name {
		case "ResponseCancelled":
			kind = EventKindResponseCancelled
		case "ResponseDone":
			kind = EventKindResponseDone
		}
		return Event{Kind: kind, Response: &ResponseLifecycle{ResponseID: w.ResponseID}}, nil
	case "ConversationItemAdded":
		return Event{Kind: EventKindConversationItemAdded, ConversationItem: append(json.RawMessage(nil), payload...)}, nil
	case "ConversationItemDone":
		var w wireConversationItemDone
		if err := json.Unmarshal(payload, &w); err != nil {
			return Event{}, decodeErr(name, err)
		}
		return Event{Kind: EventKindConversationItemDone, ItemID: w.ItemID}, nil
	case "HandoffRequested":
		var w wireHandoffRequested
		if err := json.Unmarshal(payload, &w); err != nil {
			return Event{}, decodeErr(name, err)
		}
		return Event{Kind: EventKindHandoffRequested, Handoff: wireToHandoff(w)}, nil
	case "NoopRequested":
		var w wireNoopRequested
		if err := json.Unmarshal(payload, &w); err != nil {
			return Event{}, decodeErr(name, err)
		}
		return Event{Kind: EventKindNoopRequested, Noop: &NoopRequested{CallID: w.CallID, ItemID: w.ItemID}}, nil
	case "Error":
		var msg string
		if err := json.Unmarshal(payload, &msg); err != nil {
			return Event{}, decodeErr(name, err)
		}
		return Event{Kind: EventKindError, ErrorMessage: msg}, nil
	default:
		return Event{}, fmt.Errorf("realtimeconv: unknown event variant %q", name)
	}
}

// decodeErr wraps a variant-decode failure with the variant name for context.
func decodeErr(name string, err error) error {
	return fmt.Errorf("realtimeconv: decode %s payload: %w", name, err)
}

// wireToHandoff projects the JSON shape back onto a HandoffRequested.
func wireToHandoff(w wireHandoffRequested) *HandoffRequested {
	entries := make([]TranscriptEntry, len(w.ActiveTranscript))
	for i, entry := range w.ActiveTranscript {
		entries[i] = TranscriptEntry{Role: entry.Role, Text: entry.Text}
	}
	return &HandoffRequested{
		HandoffID:        w.HandoffID,
		ItemID:           w.ItemID,
		InputTranscript:  w.InputTranscript,
		ActiveTranscript: entries,
	}
}

// handoffToWire projects a HandoffRequested onto its JSON shape.
func handoffToWire(h *HandoffRequested) wireHandoffRequested {
	if h == nil {
		return wireHandoffRequested{ActiveTranscript: []wireTranscriptEntry{}}
	}
	entries := make([]wireTranscriptEntry, len(h.ActiveTranscript))
	for i, entry := range h.ActiveTranscript {
		entries[i] = wireTranscriptEntry{Role: entry.Role, Text: entry.Text}
	}
	return wireHandoffRequested{
		HandoffID:        h.HandoffID,
		ItemID:           h.ItemID,
		InputTranscript:  h.InputTranscript,
		ActiveTranscript: entries,
	}
}

// marshalRaw is a small helper that marshals v to a json.RawMessage.
func marshalRaw(v any) (json.RawMessage, error) {
	out, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("realtimeconv: marshal payload: %w", err)
	}
	return out, nil
}
