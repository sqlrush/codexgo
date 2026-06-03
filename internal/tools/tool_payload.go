package tools

// SearchToolCallParams holds the arguments for a tool_search invocation.
//
// It mirrors Rust `codex_protocol::models::SearchToolCallParams`. Serialization:
// `query` is always present; `limit` uses `skip_serializing_if = "Option::is_none"`.
type SearchToolCallParams struct {
	Query string `json:"query"`
	Limit *int   `json:"limit,omitempty"`
}

// ToolPayloadKind discriminates the canonical payload shapes accepted by
// model-visible tool runtimes. Mirrors the Rust `ToolPayload` enum variants.
type ToolPayloadKind int

// ToolPayloadKind variants.
const (
	// ToolPayloadKindFunction carries raw JSON function-call arguments.
	ToolPayloadKindFunction ToolPayloadKind = iota
	// ToolPayloadKindToolSearch carries parsed tool_search arguments.
	ToolPayloadKindToolSearch
	// ToolPayloadKindCustom carries freeform custom-tool input.
	ToolPayloadKindCustom
)

// ToolPayload is a canonical payload accepted by a model-visible tool runtime.
// Mirrors Rust `ToolPayload`.
type ToolPayload struct {
	Kind ToolPayloadKind

	// Arguments holds the raw function-call arguments (Function variant).
	Arguments string
	// SearchArguments holds the parsed tool_search arguments (ToolSearch variant).
	SearchArguments SearchToolCallParams
	// Input holds the freeform custom-tool input (Custom variant).
	Input string
}

// FunctionPayload constructs a Function payload.
func FunctionPayload(arguments string) ToolPayload {
	return ToolPayload{Kind: ToolPayloadKindFunction, Arguments: arguments}
}

// ToolSearchPayload constructs a ToolSearch payload.
func ToolSearchPayload(arguments SearchToolCallParams) ToolPayload {
	return ToolPayload{Kind: ToolPayloadKindToolSearch, SearchArguments: arguments}
}

// CustomPayload constructs a Custom payload.
func CustomPayload(input string) ToolPayload {
	return ToolPayload{Kind: ToolPayloadKindCustom, Input: input}
}

// LogPayload returns the payload text suitable for logging. Mirrors Rust
// `ToolPayload::log_payload`: function arguments and custom input are returned
// verbatim, while a tool_search payload logs only its query.
func (p ToolPayload) LogPayload() string {
	switch p.Kind {
	case ToolPayloadKindToolSearch:
		return p.SearchArguments.Query
	case ToolPayloadKindCustom:
		return p.Input
	default:
		return p.Arguments
	}
}
