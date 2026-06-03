package mcpserver

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// strictUnmarshal decodes data into v while rejecting any object key that does
// not map to a struct field. It is the Go analogue of serde's
// deny_unknown_fields, surfacing the offending key in the error so the message
// matches the reference closely (e.g. the removed "profile" field).
func strictUnmarshal(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("decode params: %w", err)
	}
	return nil
}
