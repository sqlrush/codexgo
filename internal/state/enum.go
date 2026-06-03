package state

import "encoding/json"

// enumToString serializes value the same way as the Rust `enum_to_string`
// helper: JSON-encode it, and if the result is a JSON string return the
// unquoted contents, otherwise return the raw JSON text. Errors yield "".
//
// This is used to canonicalize protocol enums (SessionSource, SandboxPolicy,
// AskForApproval, ReasoningEffort) into the textual form stored in SQLite.
func enumToString(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	if len(data) > 0 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err == nil {
			return s
		}
	}
	return string(data)
}
