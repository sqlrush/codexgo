package api

import (
	"bytes"
	"encoding/json"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// putString marshals a string value into the field map.
func putString(m map[string]json.RawMessage, key, value string) error {
	return putValue(m, key, value)
}

// putValue marshals any value into the field map under key.
func putValue(m map[string]json.RawMessage, key string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	m[key] = data
	return nil
}

// encodeOrdered serializes the field map as a JSON object with keys emitted in
// the given order. Only keys present in m are emitted; the order slice defines
// the canonical field order matching the Rust struct definition.
func encodeOrdered(m map[string]json.RawMessage, order []string) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	first := true
	for _, key := range order {
		raw, ok := m[key]
		if !ok {
			continue
		}
		if !first {
			buf.WriteByte(',')
		}
		first = false
		keyBytes, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		buf.Write(keyBytes)
		buf.WriteByte(':')
		buf.Write(raw)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// marshalTagged encodes payload as a JSON object and inserts a "type" tag with
// the given value as the first field, mirroring serde's internally-tagged enum
// representation (tag = "type").
func marshalTagged(tag string, payload any) ([]byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	// payload must serialize to a JSON object.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteByte('{')
	tagBytes, _ := json.Marshal(tag)
	buf.WriteString(`"type":`)
	buf.Write(tagBytes)
	for k, v := range fields {
		buf.WriteByte(',')
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		buf.Write(v)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// ensureItems returns a non-nil slice so it serializes as [] rather than null,
// matching the Rust Vec<ResponseItem> which always serializes as an array.
func ensureItems(items []protocol.ResponseItem) []protocol.ResponseItem {
	if items == nil {
		return []protocol.ResponseItem{}
	}
	return items
}

// ensureRawSlice returns a non-nil slice so it serializes as [].
func ensureRawSlice(items []json.RawMessage) []json.RawMessage {
	if items == nil {
		return []json.RawMessage{}
	}
	return items
}

// ensureStrings returns a non-nil slice so it serializes as [].
func ensureStrings(items []string) []string {
	if items == nil {
		return []string{}
	}
	return items
}
