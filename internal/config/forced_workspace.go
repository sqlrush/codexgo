package config

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ForcedChatgptWorkspaceIds is an untagged enum accepting either a single
// workspace ID string or a list of strings. A comma-separated single string is
// rejected, matching the Rust custom Deserialize.
type ForcedChatgptWorkspaceIds struct {
	// Single is set when the value was a single (comma-free) string.
	Single *string
	// Multiple is set when the value was a list of strings.
	Multiple *[]string
}

// IntoVec returns the workspace IDs as a flat slice.
func (f ForcedChatgptWorkspaceIds) IntoVec() []string {
	if f.Single != nil {
		return []string{*f.Single}
	}
	if f.Multiple != nil {
		return *f.Multiple
	}
	return nil
}

const commaSeparatedWorkspaceErr = "forced_chatgpt_workspace_id must be a single workspace ID string or a TOML list " +
	"of strings; comma-separated strings are not supported. Use " +
	"`forced_chatgpt_workspace_id = [\"123e4567-e89b-42d3-a456-426614174000\", " +
	"\"123e4567-e89b-42d3-a456-426614174001\"]` instead."

// MarshalJSON emits the string or array form.
func (f ForcedChatgptWorkspaceIds) MarshalJSON() ([]byte, error) {
	if f.Multiple != nil {
		return json.Marshal(*f.Multiple)
	}
	if f.Single != nil {
		return json.Marshal(*f.Single)
	}
	return json.Marshal(nil)
}

// UnmarshalJSON decodes the string or array form, rejecting comma-separated
// single strings.
func (f *ForcedChatgptWorkspaceIds) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		if strings.Contains(s, ",") {
			return fmt.Errorf("%s", commaSeparatedWorkspaceErr)
		}
		f.Single = &s
		f.Multiple = nil
		return nil
	}
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		f.Multiple = &arr
		f.Single = nil
		return nil
	}
	return fmt.Errorf("forced_chatgpt_workspace_id must be a string or a list of strings")
}
