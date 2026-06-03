package execserver

import (
	"encoding/json"
	"fmt"
)

// ProcessId is a client-chosen logical process handle scoped to one
// connection/session. It is a protocol key, not an OS pid.
//
// Rust: a transparent newtype wrapper over String
// (`#[serde(transparent)] pub struct ProcessId(String)`). On the wire it is a
// bare JSON string, so MarshalJSON/UnmarshalJSON round-trip it transparently.
type ProcessId struct {
	value string
}

// NewProcessId builds a ProcessId from a string.
func NewProcessId(value string) ProcessId {
	return ProcessId{value: value}
}

// String returns the id as a string. Mirrors the Rust Display impl.
func (p ProcessId) String() string {
	return p.value
}

// MarshalJSON encodes the id as a bare JSON string (serde transparent).
func (p ProcessId) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.value)
}

// UnmarshalJSON decodes a bare JSON string into the id.
func (p *ProcessId) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("execserver: ProcessId must be a string: %w", err)
	}
	p.value = s
	return nil
}
