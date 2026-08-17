package protocol

import (
	"encoding/json"
	"fmt"
)

// ThreadHistoryMode selects how a thread's durable history is laid out. Legacy
// threads replay a single rollout; paginated threads hydrate through turn and
// item lists (0.147). Mirrors Rust `ThreadHistoryMode` (`rename_all = "lowercase"`).
type ThreadHistoryMode string

const (
	// ThreadHistoryModeLegacy is the default single-rollout layout.
	ThreadHistoryModeLegacy ThreadHistoryMode = "legacy"
	// ThreadHistoryModePaginated hydrates history through turn/item pages.
	ThreadHistoryModePaginated ThreadHistoryMode = "paginated"
)

// String returns the wire spelling.
func (m ThreadHistoryMode) String() string { return string(m) }

// IsValid reports whether m is one of the known modes (the zero value is not).
func (m ThreadHistoryMode) IsValid() bool {
	return m == ThreadHistoryModeLegacy || m == ThreadHistoryModePaginated
}

// ParseThreadHistoryMode parses the wire spelling, mirroring the Rust `FromStr`.
func ParseThreadHistoryMode(s string) (ThreadHistoryMode, error) {
	m := ThreadHistoryMode(s)
	if !m.IsValid() {
		return "", fmt.Errorf("protocol: unknown thread history mode %q", s)
	}
	return m, nil
}

// UnmarshalJSON rejects unknown modes at the boundary.
func (m *ThreadHistoryMode) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	parsed, err := ParseThreadHistoryMode(s)
	if err != nil {
		return err
	}
	*m = parsed
	return nil
}

// HistoryPosition is an exclusive position in another thread's paginated
// rollout history: the prefix of that thread's history a fork or subagent
// inherits. Mirrors Rust `HistoryPosition`.
type HistoryPosition struct {
	ThreadID ThreadID `json:"thread_id"`
	// EndOrdinalExclusive is the first rollout ordinal not included from the prefix.
	EndOrdinalExclusive uint64 `json:"end_ordinal_exclusive"`
	// EndByteOffset is the byte offset immediately after the last included JSONL record.
	EndByteOffset uint64 `json:"end_byte_offset"`
}
