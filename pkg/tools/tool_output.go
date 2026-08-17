package tools

import (
	"encoding/json"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

const (
	telemetryPreviewMaxBytes       = 2 * 1024
	telemetryPreviewMaxLines       = 64
	telemetryPreviewTruncationNote = "[... telemetry preview truncated ...]"
)

// ToolOutput is the model-facing output contract returned by executable tool
// runtimes. Mirrors the Rust `ToolOutput` trait.
//
// PostToolUse* methods carry the data exposed to PostToolUse hooks. The default
// implementations are provided by DefaultToolOutput, which concrete outputs may
// embed to inherit the same defaults the Rust trait offers.
type ToolOutput interface {
	// LogPreview returns a truncated preview for telemetry logging.
	LogPreview() string

	// SuccessForLogging reports whether the output represents a success.
	SuccessForLogging() bool

	// ToResponseItem converts the output into an input-side conversation item.
	ToResponseItem(callID string, payload ToolPayload) ResponseInputItem

	// PostToolUseID returns the tool call id exposed to PostToolUse hooks.
	PostToolUseID(callID string) string

	// PostToolUseInput returns the tool input exposed to PostToolUse hooks.
	PostToolUseInput(payload ToolPayload) (json.RawMessage, bool)

	// PostToolUseResponse returns the stable value exposed to PostToolUse hooks.
	PostToolUseResponse(callID string, payload ToolPayload) (json.RawMessage, bool)

	// CodeModeResult returns the value surfaced to a code-mode runtime.
	CodeModeResult(payload ToolPayload) json.RawMessage
}

// DefaultToolOutput supplies the default ToolOutput hook implementations,
// matching the Rust trait's provided methods. Concrete outputs may embed it.
type DefaultToolOutput struct{}

// PostToolUseID returns the call id unchanged.
func (DefaultToolOutput) PostToolUseID(callID string) string { return callID }

// PostToolUseInput returns no input by default.
func (DefaultToolOutput) PostToolUseInput(ToolPayload) (json.RawMessage, bool) {
	return nil, false
}

// PostToolUseResponse returns no response by default.
func (DefaultToolOutput) PostToolUseResponse(string, ToolPayload) (json.RawMessage, bool) {
	return nil, false
}

// JsonToolOutput is the canonical JSON-valued tool output. Mirrors Rust
// `JsonToolOutput`. A nil success defaults to true for logging.
type JsonToolOutput struct {
	DefaultToolOutput
	value   json.RawMessage
	success *bool
}

// NewJsonToolOutput constructs a successful JSON output. Mirrors Rust
// `JsonToolOutput::new`. The value is stored in compact form so the function
// output body matches serde_json's `Value::to_string`.
func NewJsonToolOutput(value json.RawMessage) JsonToolOutput {
	success := true
	return JsonToolOutput{value: compactJSON(value), success: &success}
}

// NewJsonToolOutputWithSuccess constructs a JSON output with an explicit success
// flag. Mirrors Rust `JsonToolOutput::with_success`.
func NewJsonToolOutputWithSuccess(value json.RawMessage, success *bool) JsonToolOutput {
	return JsonToolOutput{value: compactJSON(value), success: success}
}

// compactJSON normalizes a JSON value to its compact serialization, matching
// serde_json's `Value::to_string`. Invalid JSON is returned unchanged.
func compactJSON(value json.RawMessage) json.RawMessage {
	var v any
	if err := json.Unmarshal(value, &v); err != nil {
		return value
	}
	out, err := json.Marshal(v)
	if err != nil {
		return value
	}
	return out
}

// Value returns the underlying JSON value.
func (o JsonToolOutput) Value() json.RawMessage { return o.value }

// LogPreview returns a telemetry preview of the JSON value.
func (o JsonToolOutput) LogPreview() string {
	return telemetryPreview(string(o.value))
}

// SuccessForLogging reports success, defaulting to true when unset.
func (o JsonToolOutput) SuccessForLogging() bool {
	if o.success == nil {
		return true
	}
	return *o.success
}

// ToResponseItem converts the JSON output into a function (or custom) tool call
// output. Mirrors Rust `JsonToolOutput::to_response_item`: a Custom payload maps
// to a custom_tool_call_output, otherwise a function_call_output.
func (o JsonToolOutput) ToResponseItem(callID string, payload ToolPayload) ResponseInputItem {
	output := protocol.FunctionCallOutputPayload{
		Text:    strPtrFromBytes(o.value),
		Success: o.success,
	}
	if payload.Kind == ToolPayloadKindCustom {
		return CustomToolCallOutputInput(callID, nil, output)
	}
	return FunctionCallOutputInput(callID, output)
}

// PostToolUseResponse returns the JSON value. Mirrors Rust impl.
func (o JsonToolOutput) PostToolUseResponse(string, ToolPayload) (json.RawMessage, bool) {
	return cloneRaw(o.value), true
}

// CodeModeResult returns the JSON value directly. Mirrors Rust impl.
func (o JsonToolOutput) CodeModeResult(ToolPayload) json.RawMessage {
	return cloneRaw(o.value)
}

// strPtrFromBytes returns a *string holding the value's text. The JSON value is
// stored verbatim, matching Rust's `value.to_string()` for the function output
// body.
func strPtrFromBytes(value json.RawMessage) *string {
	s := string(value)
	return &s
}

// cloneRaw returns a defensive copy of a json.RawMessage so callers cannot
// mutate the output's underlying buffer (immutability).
func cloneRaw(value json.RawMessage) json.RawMessage {
	if value == nil {
		return nil
	}
	out := make(json.RawMessage, len(value))
	copy(out, value)
	return out
}
