package tools

import "encoding/json"

// ToolDefinition holds tool metadata and schemas that adapters lower into
// higher-level tool specs. Mirrors Rust `ToolDefinition`.
//
// OutputSchema is an arbitrary decoded JSON value (or nil when absent), matching
// Rust's `Option<serde_json::Value>`.
type ToolDefinition struct {
	Name         string
	Description  string
	InputSchema  JsonSchema
	OutputSchema json.RawMessage
	DeferLoading bool
}

// Renamed returns a copy with the name replaced. Mirrors Rust
// `ToolDefinition::renamed`. The original is not mutated.
func (t ToolDefinition) Renamed(name string) ToolDefinition {
	t.Name = name
	return t
}

// IntoDeferred returns a copy with the output schema dropped and deferred
// loading enabled. Mirrors Rust `ToolDefinition::into_deferred`. The original is
// not mutated.
func (t ToolDefinition) IntoDeferred() ToolDefinition {
	t.OutputSchema = nil
	t.DeferLoading = true
	return t
}
