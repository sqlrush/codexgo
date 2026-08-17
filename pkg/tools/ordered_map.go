package tools

import (
	"bytes"
	"encoding/json"
)

// orderedMap is a minimal helper that serializes JSON object keys in insertion
// order. It exists because Rust serde emits struct fields in declaration order,
// and Go's encoding/json sorts map keys alphabetically. Several tool wire shapes
// (function/namespace tools, ToolSpec variants) depend on a specific key order.
type orderedMap struct {
	keys   []string
	values map[string]any
}

// set appends or overwrites a key/value pair, preserving first-insertion order.
func (m *orderedMap) set(key string, value any) {
	if m.values == nil {
		m.values = make(map[string]any)
	}
	if _, exists := m.values[key]; !exists {
		m.keys = append(m.keys, key)
	}
	m.values[key] = value
}

// MarshalJSON emits the object with keys in insertion order.
func (m orderedMap) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, key := range m.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		keyJSON, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		buf.Write(keyJSON)
		buf.WriteByte(':')
		valJSON, err := json.Marshal(m.values[key])
		if err != nil {
			return nil, err
		}
		buf.Write(valJSON)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}
