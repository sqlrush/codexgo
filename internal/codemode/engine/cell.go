package engine

import (
	"encoding/json"
	"fmt"
)

// CellID mirrors codex's `CellId`. It is a newtype wrapper over a string that
// scopes a single code-mode invocation within a session. CellIDs are allocated
// monotonically by a session and reused as map keys, so it is a comparable
// value type.
type CellID struct {
	value string
}

// NewCellID constructs a CellID from a raw string, mirroring codex's
// `CellId::new`.
func NewCellID(value string) CellID {
	return CellID{value: value}
}

// String renders the underlying identifier, mirroring codex's Display impl.
func (c CellID) String() string {
	return c.value
}

// AsStr returns the underlying identifier, mirroring codex's `as_str`.
func (c CellID) AsStr() string {
	return c.value
}

// MarshalJSON emits the bare string, mirroring serde's newtype-struct encoding.
func (c CellID) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.value)
}

// UnmarshalJSON parses a bare string into the CellID.
func (c *CellID) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("decode cell id: %w", err)
	}
	c.value = raw
	return nil
}
