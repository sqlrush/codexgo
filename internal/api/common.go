package api

import (
	"encoding/json"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// WebSocket request client-metadata keys, mirroring the Rust constants.
const (
	WSRequestHeaderTraceparentClientMetadataKey = "ws_request_header_traceparent"
	WSRequestHeaderTracestateClientMetadataKey  = "ws_request_header_tracestate"
)

// Reasoning controls the `reasoning` field of a Responses request. It mirrors the
// Rust `Reasoning` struct (effort/summary, each skipped when nil).
type Reasoning struct {
	Effort  *protocol.ReasoningEffort  `json:"effort,omitempty"`
	Summary *protocol.ReasoningSummary `json:"summary,omitempty"`
}

// OpenAiVerbosity mirrors the Rust `OpenAiVerbosity` enum (lowercase).
type OpenAiVerbosity string

const (
	// OpenAiVerbosityLow is low verbosity.
	OpenAiVerbosityLow OpenAiVerbosity = "low"
	// OpenAiVerbosityMedium is medium verbosity (the default).
	OpenAiVerbosityMedium OpenAiVerbosity = "medium"
	// OpenAiVerbosityHigh is high verbosity.
	OpenAiVerbosityHigh OpenAiVerbosity = "high"
)

// OpenAiVerbosityFromConfig maps a protocol.Verbosity to an OpenAiVerbosity,
// mirroring the Rust `From<VerbosityConfig>`.
func OpenAiVerbosityFromConfig(v protocol.Verbosity) OpenAiVerbosity {
	switch v {
	case protocol.VerbosityLow:
		return OpenAiVerbosityLow
	case protocol.VerbosityHigh:
		return OpenAiVerbosityHigh
	default:
		return OpenAiVerbosityMedium
	}
}

// TextFormatType mirrors the Rust `TextFormatType` enum (snake_case). The only
// variant is json_schema.
type TextFormatType string

// TextFormatTypeJSONSchema is the json_schema format type.
const TextFormatTypeJSONSchema TextFormatType = "json_schema"

// TextFormat mirrors the Rust `TextFormat` struct.
type TextFormat struct {
	Type   TextFormatType  `json:"type"`
	Strict bool            `json:"strict"`
	Schema json.RawMessage `json:"schema"`
	Name   string          `json:"name"`
}

// TextControls controls the `text` field for the Responses API. It mirrors the
// Rust `TextControls` struct.
type TextControls struct {
	Verbosity *OpenAiVerbosity `json:"verbosity,omitempty"`
	Format    *TextFormat      `json:"format,omitempty"`
}

// ResponsesApiRequest is the canonical Responses API request body. Its JSON shape
// must match the Rust `ResponsesApiRequest` exactly, including field order and
// the skip_serializing_if rules.
type ResponsesApiRequest struct {
	Model             string                  `json:"model"`
	Instructions      string                  `json:"instructions"`
	Input             []protocol.ResponseItem `json:"input"`
	Tools             []json.RawMessage       `json:"tools"`
	ToolChoice        string                  `json:"tool_choice"`
	ParallelToolCalls bool                    `json:"parallel_tool_calls"`
	Reasoning         *Reasoning              `json:"reasoning"`
	Store             bool                    `json:"store"`
	Stream            bool                    `json:"stream"`
	Include           []string                `json:"include"`
	ServiceTier       *string                 `json:"service_tier,omitempty"`
	PromptCacheKey    *string                 `json:"prompt_cache_key,omitempty"`
	Text              *TextControls           `json:"text,omitempty"`
	ClientMetadata    map[string]string       `json:"client_metadata,omitempty"`
}

// MarshalJSON encodes the request, applying the Rust skip_serializing_if rules
// that Go struct tags cannot express: `instructions` is omitted when empty,
// `reasoning` is always present (may be null), and the slices are always present.
func (r ResponsesApiRequest) MarshalJSON() ([]byte, error) {
	m := make(map[string]json.RawMessage)
	if err := putString(m, "model", r.Model); err != nil {
		return nil, err
	}
	// instructions: skip when empty (String::is_empty).
	if r.Instructions != "" {
		if err := putString(m, "instructions", r.Instructions); err != nil {
			return nil, err
		}
	}
	if err := putValue(m, "input", ensureItems(r.Input)); err != nil {
		return nil, err
	}
	if err := putValue(m, "tools", ensureRawSlice(r.Tools)); err != nil {
		return nil, err
	}
	if err := putString(m, "tool_choice", r.ToolChoice); err != nil {
		return nil, err
	}
	if err := putValue(m, "parallel_tool_calls", r.ParallelToolCalls); err != nil {
		return nil, err
	}
	// reasoning: Option<Reasoning> without skip => always present (may be null).
	if err := putValue(m, "reasoning", r.Reasoning); err != nil {
		return nil, err
	}
	if err := putValue(m, "store", r.Store); err != nil {
		return nil, err
	}
	if err := putValue(m, "stream", r.Stream); err != nil {
		return nil, err
	}
	if err := putValue(m, "include", ensureStrings(r.Include)); err != nil {
		return nil, err
	}
	if r.ServiceTier != nil {
		if err := putValue(m, "service_tier", r.ServiceTier); err != nil {
			return nil, err
		}
	}
	if r.PromptCacheKey != nil {
		if err := putValue(m, "prompt_cache_key", r.PromptCacheKey); err != nil {
			return nil, err
		}
	}
	if r.Text != nil {
		if err := putValue(m, "text", r.Text); err != nil {
			return nil, err
		}
	}
	if r.ClientMetadata != nil {
		if err := putValue(m, "client_metadata", r.ClientMetadata); err != nil {
			return nil, err
		}
	}

	order := []string{
		"model", "instructions", "input", "tools", "tool_choice",
		"parallel_tool_calls", "reasoning", "store", "stream", "include",
		"service_tier", "prompt_cache_key", "text", "client_metadata",
	}
	return encodeOrdered(m, order)
}

// ResponseCreateWsRequest mirrors the Rust `ResponseCreateWsRequest`. It is the
// payload of a "response.create" WebSocket frame.
type ResponseCreateWsRequest struct {
	Model              string                  `json:"model"`
	Instructions       string                  `json:"instructions"`
	PreviousResponseID *string                 `json:"previous_response_id,omitempty"`
	Input              []protocol.ResponseItem `json:"input"`
	Tools              []json.RawMessage       `json:"tools"`
	ToolChoice         string                  `json:"tool_choice"`
	ParallelToolCalls  bool                    `json:"parallel_tool_calls"`
	Reasoning          *Reasoning              `json:"reasoning"`
	Store              bool                    `json:"store"`
	Stream             bool                    `json:"stream"`
	Include            []string                `json:"include"`
	ServiceTier        *string                 `json:"service_tier,omitempty"`
	PromptCacheKey     *string                 `json:"prompt_cache_key,omitempty"`
	Text               *TextControls           `json:"text,omitempty"`
	Generate           *bool                   `json:"generate,omitempty"`
	ClientMetadata     map[string]string       `json:"client_metadata,omitempty"`
}

// MarshalJSON encodes the WS create request with the Rust skip_serializing_if
// rules: instructions skipped when empty, optional pointers skipped when nil,
// and the required fields (reasoning, store, stream, slices) always present.
func (r ResponseCreateWsRequest) MarshalJSON() ([]byte, error) {
	m := make(map[string]json.RawMessage)
	if err := putString(m, "model", r.Model); err != nil {
		return nil, err
	}
	if r.Instructions != "" {
		if err := putString(m, "instructions", r.Instructions); err != nil {
			return nil, err
		}
	}
	if r.PreviousResponseID != nil {
		if err := putValue(m, "previous_response_id", r.PreviousResponseID); err != nil {
			return nil, err
		}
	}
	if err := putValue(m, "input", ensureItems(r.Input)); err != nil {
		return nil, err
	}
	if err := putValue(m, "tools", ensureRawSlice(r.Tools)); err != nil {
		return nil, err
	}
	if err := putString(m, "tool_choice", r.ToolChoice); err != nil {
		return nil, err
	}
	if err := putValue(m, "parallel_tool_calls", r.ParallelToolCalls); err != nil {
		return nil, err
	}
	if err := putValue(m, "reasoning", r.Reasoning); err != nil {
		return nil, err
	}
	if err := putValue(m, "store", r.Store); err != nil {
		return nil, err
	}
	if err := putValue(m, "stream", r.Stream); err != nil {
		return nil, err
	}
	if err := putValue(m, "include", ensureStrings(r.Include)); err != nil {
		return nil, err
	}
	if r.ServiceTier != nil {
		if err := putValue(m, "service_tier", r.ServiceTier); err != nil {
			return nil, err
		}
	}
	if r.PromptCacheKey != nil {
		if err := putValue(m, "prompt_cache_key", r.PromptCacheKey); err != nil {
			return nil, err
		}
	}
	if r.Text != nil {
		if err := putValue(m, "text", r.Text); err != nil {
			return nil, err
		}
	}
	if r.Generate != nil {
		if err := putValue(m, "generate", r.Generate); err != nil {
			return nil, err
		}
	}
	if r.ClientMetadata != nil {
		if err := putValue(m, "client_metadata", r.ClientMetadata); err != nil {
			return nil, err
		}
	}
	order := []string{
		"model", "instructions", "previous_response_id", "input", "tools",
		"tool_choice", "parallel_tool_calls", "reasoning", "store", "stream",
		"include", "service_tier", "prompt_cache_key", "text", "generate",
		"client_metadata",
	}
	return encodeOrdered(m, order)
}

// ResponseCreateWsRequestFromAPI builds a ResponseCreateWsRequest from a
// ResponsesApiRequest, mirroring the Rust `From<&ResponsesApiRequest>`.
func ResponseCreateWsRequestFromAPI(request ResponsesApiRequest) ResponseCreateWsRequest {
	return ResponseCreateWsRequest{
		Model:              request.Model,
		Instructions:       request.Instructions,
		PreviousResponseID: nil,
		Input:              request.Input,
		Tools:              request.Tools,
		ToolChoice:         request.ToolChoice,
		ParallelToolCalls:  request.ParallelToolCalls,
		Reasoning:          request.Reasoning,
		Store:              request.Store,
		Stream:             request.Stream,
		Include:            request.Include,
		ServiceTier:        request.ServiceTier,
		PromptCacheKey:     request.PromptCacheKey,
		Text:               request.Text,
		Generate:           nil,
		ClientMetadata:     request.ClientMetadata,
	}
}

// ResponseProcessedWsRequest mirrors the Rust `ResponseProcessedWsRequest`.
type ResponseProcessedWsRequest struct {
	ResponseID string `json:"response_id"`
}

// ResponsesWsRequestKind discriminates the variants of ResponsesWsRequest.
type ResponsesWsRequestKind int

const (
	// ResponsesWsRequestCreate is a "response.create" frame.
	ResponsesWsRequestCreate ResponsesWsRequestKind = iota
	// ResponsesWsRequestProcessed is a "response.processed" frame.
	ResponsesWsRequestProcessed
)

// ResponsesWsRequest is the internally-tagged ("type") WebSocket request enum. It
// mirrors the Rust `ResponsesWsRequest`.
type ResponsesWsRequest struct {
	Kind      ResponsesWsRequestKind
	Create    *ResponseCreateWsRequest
	Processed *ResponseProcessedWsRequest
}

// MarshalJSON encodes the tagged WS request, inserting the "type" tag.
func (r ResponsesWsRequest) MarshalJSON() ([]byte, error) {
	switch r.Kind {
	case ResponsesWsRequestCreate:
		return marshalTagged("response.create", r.Create)
	case ResponsesWsRequestProcessed:
		return marshalTagged("response.processed", r.Processed)
	default:
		return nil, &APIError{Kind: APIErrorStream, Message: "unknown websocket request kind"}
	}
}

// CreateTextParamForRequest builds the optional TextControls for a request from
// verbosity and an optional output schema. It mirrors the Rust
// `create_text_param_for_request`.
func CreateTextParamForRequest(verbosity *protocol.Verbosity, outputSchema *json.RawMessage, outputSchemaStrict bool) *TextControls {
	if verbosity == nil && outputSchema == nil {
		return nil
	}
	tc := &TextControls{}
	if verbosity != nil {
		v := OpenAiVerbosityFromConfig(*verbosity)
		tc.Verbosity = &v
	}
	if outputSchema != nil {
		tc.Format = &TextFormat{
			Type:   TextFormatTypeJSONSchema,
			Strict: outputSchemaStrict,
			Schema: *outputSchema,
			Name:   "codex_output_schema",
		}
	}
	return tc
}

// ResponseCreateClientMetadata merges trace context into client metadata,
// mirroring the Rust `response_create_client_metadata`. It returns nil when the
// resulting map would be empty. The input map is never mutated.
func ResponseCreateClientMetadata(clientMetadata map[string]string, trace *protocol.W3cTraceContext) map[string]string {
	out := make(map[string]string, len(clientMetadata)+2)
	for k, v := range clientMetadata {
		out[k] = v
	}
	if trace != nil {
		if trace.Traceparent != nil {
			out[WSRequestHeaderTraceparentClientMetadataKey] = *trace.Traceparent
		}
		if trace.Tracestate != nil {
			out[WSRequestHeaderTracestateClientMetadataKey] = *trace.Tracestate
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
