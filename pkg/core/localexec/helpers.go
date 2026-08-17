package localexec

import "encoding/json"

// boolPtr returns a pointer to b (tool output success flags are *bool).
func boolPtr(b bool) *bool { return &b }

// mustJSON marshals v, panicking on failure; only used for shapes that cannot
// fail to encode (maps of strings, plain structs).
func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
