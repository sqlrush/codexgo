package rollout

import (
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// RolloutItemKind is the discriminator for a RolloutItem.
//
// Mirrors the Rust `RolloutItem` adjacently tagged enum
// (`#[serde(tag = "type", content = "payload", rename_all = "snake_case")]`).
type RolloutItemKind string

const (
	// RolloutItemKindSessionMeta tags a session-metadata line.
	RolloutItemKindSessionMeta RolloutItemKind = "session_meta"
	// RolloutItemKindResponseItem tags a Responses-API history item.
	RolloutItemKindResponseItem RolloutItemKind = "response_item"
	// RolloutItemKindCompacted tags a compaction record.
	RolloutItemKindCompacted RolloutItemKind = "compacted"
	// RolloutItemKindTurnContext tags a turn-context record.
	RolloutItemKindTurnContext RolloutItemKind = "turn_context"
	// RolloutItemKindEventMsg tags an event message.
	RolloutItemKindEventMsg RolloutItemKind = "event_msg"
)

// RolloutItem is one tagged item in a rollout JSONL line. Exactly one of the
// payload pointers is populated for the corresponding Kind.
type RolloutItem struct {
	Kind RolloutItemKind

	SessionMeta  *SessionMetaLine
	ResponseItem *protocol.ResponseItem
	Compacted    *CompactedItem
	TurnContext  *TurnContextItem
	EventMsg     *protocol.EventMsg
}

// NewSessionMetaItem wraps a SessionMetaLine in a RolloutItem.
func NewSessionMetaItem(line SessionMetaLine) RolloutItem {
	return RolloutItem{Kind: RolloutItemKindSessionMeta, SessionMeta: &line}
}

// NewResponseItem wraps a ResponseItem in a RolloutItem.
func NewResponseItem(item protocol.ResponseItem) RolloutItem {
	return RolloutItem{Kind: RolloutItemKindResponseItem, ResponseItem: &item}
}

// NewCompactedItem wraps a CompactedItem in a RolloutItem.
func NewCompactedItem(item CompactedItem) RolloutItem {
	return RolloutItem{Kind: RolloutItemKindCompacted, Compacted: &item}
}

// NewTurnContextItem wraps a TurnContextItem in a RolloutItem.
func NewTurnContextItem(item TurnContextItem) RolloutItem {
	return RolloutItem{Kind: RolloutItemKindTurnContext, TurnContext: &item}
}

// NewEventMsgItem wraps an EventMsg in a RolloutItem.
func NewEventMsgItem(ev protocol.EventMsg) RolloutItem {
	return RolloutItem{Kind: RolloutItemKindEventMsg, EventMsg: &ev}
}

// rolloutItemEnvelope is the adjacently-tagged wire representation.
type rolloutItemEnvelope struct {
	Type    RolloutItemKind `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// MarshalJSON encodes the RolloutItem with the adjacent `type`/`payload` tagging.
func (r RolloutItem) MarshalJSON() ([]byte, error) {
	var payload any
	switch r.Kind {
	case RolloutItemKindSessionMeta:
		payload = r.SessionMeta
	case RolloutItemKindResponseItem:
		payload = r.ResponseItem
	case RolloutItemKindCompacted:
		payload = r.Compacted
	case RolloutItemKindTurnContext:
		payload = r.TurnContext
	case RolloutItemKindEventMsg:
		payload = r.EventMsg
	default:
		return nil, fmt.Errorf("unknown rollout item kind: %q", r.Kind)
	}
	if payload == nil {
		return nil, fmt.Errorf("rollout item %q has no payload", r.Kind)
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal rollout item payload: %w", err)
	}
	return json.Marshal(rolloutItemEnvelope{Type: r.Kind, Payload: payloadJSON})
}

// UnmarshalJSON decodes an adjacently-tagged RolloutItem.
func (r *RolloutItem) UnmarshalJSON(data []byte) error {
	var env rolloutItemEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("decode rollout item envelope: %w", err)
	}
	switch env.Type {
	case RolloutItemKindSessionMeta:
		var line SessionMetaLine
		if err := json.Unmarshal(env.Payload, &line); err != nil {
			return fmt.Errorf("decode session_meta payload: %w", err)
		}
		*r = RolloutItem{Kind: env.Type, SessionMeta: &line}
	case RolloutItemKindResponseItem:
		var item protocol.ResponseItem
		if err := json.Unmarshal(env.Payload, &item); err != nil {
			return fmt.Errorf("decode response_item payload: %w", err)
		}
		*r = RolloutItem{Kind: env.Type, ResponseItem: &item}
	case RolloutItemKindCompacted:
		var item CompactedItem
		if err := json.Unmarshal(env.Payload, &item); err != nil {
			return fmt.Errorf("decode compacted payload: %w", err)
		}
		*r = RolloutItem{Kind: env.Type, Compacted: &item}
	case RolloutItemKindTurnContext:
		var item TurnContextItem
		if err := json.Unmarshal(env.Payload, &item); err != nil {
			return fmt.Errorf("decode turn_context payload: %w", err)
		}
		*r = RolloutItem{Kind: env.Type, TurnContext: &item}
	case RolloutItemKindEventMsg:
		var ev protocol.EventMsg
		if err := json.Unmarshal(env.Payload, &ev); err != nil {
			return fmt.Errorf("decode event_msg payload: %w", err)
		}
		*r = RolloutItem{Kind: env.Type, EventMsg: &ev}
	default:
		return fmt.Errorf("unknown rollout item type: %q", env.Type)
	}
	return nil
}

// RolloutLine is one line of a rollout JSONL file: a timestamp plus a flattened
// RolloutItem.
//
// Mirrors the Rust `RolloutLine`, which flattens the `RolloutItem` fields next
// to `timestamp` (`#[serde(flatten)]`).
type RolloutLine struct {
	Timestamp string
	Item      RolloutItem
}

// MarshalJSON emits `{"timestamp":...,"type":...,"payload":...}` with the
// timestamp first, matching the Rust serialization order.
func (l RolloutLine) MarshalJSON() ([]byte, error) {
	itemJSON, err := json.Marshal(l.Item)
	if err != nil {
		return nil, fmt.Errorf("marshal rollout item: %w", err)
	}
	tsJSON, err := json.Marshal(l.Timestamp)
	if err != nil {
		return nil, fmt.Errorf("marshal timestamp: %w", err)
	}
	// itemJSON is "{...}" with at least the `type` key.
	if len(itemJSON) < 2 || itemJSON[0] != '{' {
		return nil, fmt.Errorf("rollout item did not marshal to a JSON object")
	}
	out := make([]byte, 0, len(itemJSON)+len(tsJSON)+16)
	out = append(out, '{')
	out = append(out, `"timestamp":`...)
	out = append(out, tsJSON...)
	out = append(out, ',')
	out = append(out, itemJSON[1:]...)
	return out, nil
}

// UnmarshalJSON decodes the flattened timestamp + RolloutItem.
func (l *RolloutLine) UnmarshalJSON(data []byte) error {
	var ts struct {
		Timestamp string `json:"timestamp"`
	}
	if err := json.Unmarshal(data, &ts); err != nil {
		return fmt.Errorf("decode rollout line timestamp: %w", err)
	}
	var item RolloutItem
	if err := json.Unmarshal(data, &item); err != nil {
		return fmt.Errorf("decode rollout line item: %w", err)
	}
	l.Timestamp = ts.Timestamp
	l.Item = item
	return nil
}
