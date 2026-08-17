package rollout

import (
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// CompactedItem records a compaction summary plus the optional replacement
// history that re-establishes context after compaction.
//
// Mirrors the Rust `CompactedItem`. `replacement_history` is omitted when nil
// (`#[serde(default, skip_serializing_if = "Option::is_none")]`).
type CompactedItem struct {
	Message            string                   `json:"message"`
	ReplacementHistory *[]protocol.ResponseItem `json:"replacement_history,omitempty"`
	// Context-window chain metadata (0.147): the monotonic window number and
	// the UUIDv7 identities of the first / previous / this window.
	WindowNumber     *uint64 `json:"window_number,omitempty"`
	FirstWindowID    *string `json:"first_window_id,omitempty"`
	PreviousWindowID *string `json:"previous_window_id,omitempty"`
	WindowID         *string `json:"window_id,omitempty"`
}

// TurnContextItem captures the model-visible context for a turn. It is persisted
// once per real user turn (and again after mid-turn compaction) so resume/fork
// replay can recover the latest durable baseline.
//
// Mirrors the Rust `TurnContextItem`. To preserve byte-for-byte round-trip
// fidelity for this large, frequently-evolving struct without redefining the
// many nested policy types it references, the full object is retained verbatim
// in `Raw` and re-emitted unchanged. The `Cwd` and `TurnID` fields are extracted
// for the rollout logic that reads them (resume cwd matching).
type TurnContextItem struct {
	// TurnID is the optional turn identifier.
	TurnID *string
	// Cwd is the working directory for the turn. The rollout resume logic reads
	// this to match a candidate session against a requested cwd.
	Cwd string

	// Raw retains the entire original object so it can be re-serialized
	// byte-for-byte.
	Raw json.RawMessage
}

// MarshalJSON re-emits the retained raw object verbatim.
func (t TurnContextItem) MarshalJSON() ([]byte, error) {
	if len(t.Raw) == 0 {
		return nil, fmt.Errorf("turn context item has no raw payload")
	}
	return t.Raw, nil
}

// UnmarshalJSON retains the raw object and extracts the `cwd` and `turn_id`
// fields used by the rollout logic.
func (t *TurnContextItem) UnmarshalJSON(data []byte) error {
	var fields struct {
		TurnID *string `json:"turn_id"`
		Cwd    string  `json:"cwd"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		return fmt.Errorf("decode turn context item: %w", err)
	}
	raw := make(json.RawMessage, len(data))
	copy(raw, data)
	*t = TurnContextItem{
		TurnID: fields.TurnID,
		Cwd:    fields.Cwd,
		Raw:    raw,
	}
	return nil
}
