package ollama

import "encoding/json"

// pullUpdate is the wire shape of a single JSON object in the Ollama /api/pull
// streaming response. Numeric fields use json.Number so values are interpreted
// with the same "as_u64" semantics as the Rust reference (only non-negative
// integers count; anything else is treated as absent).
type pullUpdate struct {
	Status    *string      `json:"status"`
	Digest    *string      `json:"digest"`
	Total     *json.Number `json:"total"`
	Completed *json.Number `json:"completed"`
	Error     *string      `json:"error"`
}

// pullEventsFromValue converts a single decoded JSON pull update into zero or
// more events. It mirrors the Rust pull_events_from_value: a status produces a
// Status event (plus a Success event when status == "success"), and any total
// or completed value produces a ChunkProgress event carrying the digest (empty
// string when absent).
func pullEventsFromValue(update pullUpdate) []PullEvent {
	var events []PullEvent

	if update.Status != nil {
		events = append(events, NewPullStatus(*update.Status))
		if *update.Status == "success" {
			events = append(events, NewPullSuccess())
		}
	}

	digest := ""
	if update.Digest != nil {
		digest = *update.Digest
	}
	total := asU64(update.Total)
	completed := asU64(update.Completed)
	if total != nil || completed != nil {
		events = append(events, NewPullChunkProgress(digest, total, completed))
	}

	return events
}

// asU64 mirrors serde_json::Value::as_u64: it yields the value only when it is a
// non-negative integer, and nil otherwise.
func asU64(n *json.Number) *uint64 {
	if n == nil {
		return nil
	}
	v, err := uint64FromNumber(*n)
	if err != nil {
		return nil
	}
	return &v
}

// uint64FromNumber parses a json.Number as a u64, rejecting negative or
// fractional values just as serde_json's as_u64 returns None for them.
func uint64FromNumber(n json.Number) (uint64, error) {
	// json.Number.Int64 rejects fractional/overflowing input; for the byte
	// counts Ollama reports this is sufficient and matches as_u64's integer
	// requirement.
	i, err := n.Int64()
	if err != nil {
		return 0, err
	}
	if i < 0 {
		return 0, errNegativeNumber
	}
	return uint64(i), nil
}
