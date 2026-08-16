package protocol

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ResponseItemKind is the discriminator for ResponseItem. Rust:
// `#[serde(tag = "type", rename_all = "snake_case")]` with an `#[serde(other)]`
// Other fallback and a `compaction_summary` alias for Compaction.
type ResponseItemKind string

const (
	ResponseItemKindMessage              ResponseItemKind = "message"
	ResponseItemKindReasoning            ResponseItemKind = "reasoning"
	ResponseItemKindLocalShellCall       ResponseItemKind = "local_shell_call"
	ResponseItemKindFunctionCall         ResponseItemKind = "function_call"
	ResponseItemKindToolSearchCall       ResponseItemKind = "tool_search_call"
	ResponseItemKindFunctionCallOutput   ResponseItemKind = "function_call_output"
	ResponseItemKindCustomToolCall       ResponseItemKind = "custom_tool_call"
	ResponseItemKindCustomToolCallOutput ResponseItemKind = "custom_tool_call_output"
	ResponseItemKindToolSearchOutput     ResponseItemKind = "tool_search_output"
	ResponseItemKindWebSearchCall        ResponseItemKind = "web_search_call"
	ResponseItemKindImageGenerationCall  ResponseItemKind = "image_generation_call"
	ResponseItemKindCompaction           ResponseItemKind = "compaction"
	ResponseItemKindCompactionTrigger    ResponseItemKind = "compaction_trigger"
	ResponseItemKindContextCompaction    ResponseItemKind = "context_compaction"
	// Variants added by upstream 0.147 (spec 50 D0.6): inter-agent messages
	// and per-request additional tool descriptors.
	ResponseItemKindAgentMessage    ResponseItemKind = "agent_message"
	ResponseItemKindAdditionalTools ResponseItemKind = "additional_tools"
	// ResponseItemKindOther is the catch-all for unknown item types
	// (`#[serde(other)]`).
	ResponseItemKindOther ResponseItemKind = "other"
)

// ResponseItem is one item in a Responses-API conversation history. It is an
// internally-tagged enum (discriminator "type") with many variants; unknown
// types deserialize to the Other variant (forward-compatible).
//
// Several `id` fields are deserialize-only in Rust
// (`#[serde(default, skip_serializing)]`): they are read from the wire but never
// written back. These are kept here so callers retain the value, and MarshalJSON
// deliberately omits them.
type ResponseItem struct {
	Type ResponseItemKind

	// Message variant.
	MessageID *string       // deserialize-only
	Role      string        // "message"
	Content   []ContentItem // "message"
	Phase     *MessagePhase // "message", omit if nil

	// Reasoning variant.
	ReasoningID      string                          // deserialize-only
	Summary          []ReasoningItemReasoningSummary // "reasoning"
	ReasoningContent *[]ReasoningItemContent         // "reasoning", conditional
	// EncryptedContent serves both Reasoning (always emitted, may be null),
	// Compaction (required string), and ContextCompaction (omit if nil).
	EncryptedContent *string

	// LocalShellCall variant.
	LocalShellID     *string           // deserialize-only
	CallIDOpt        *string           // optional call_id (LocalShellCall, ToolSearchCall, ToolSearchOutput, CustomToolCall? no)
	LocalShellStatus LocalShellStatus  // "local_shell_call"
	Action           *LocalShellAction // "local_shell_call"

	// FunctionCall variant.
	FunctionCallID *string // deserialize-only
	Name           string  // function_call / custom_tool_call (also CustomToolCallOutput optional)
	Namespace      *string // function_call, omit if nil
	Arguments      string  // function_call (string of JSON)
	CallID         string  // required call_id (FunctionCall, FunctionCallOutput, CustomToolCall, CustomToolCallOutput)

	// ToolSearchCall variant.
	ToolSearchCallID *string         // deserialize-only
	StatusOpt        *string         // optional status (ToolSearchCall, CustomToolCall, WebSearchCall)
	Execution        string          // ToolSearchCall / ToolSearchOutput
	ArgumentsValue   json.RawMessage // ToolSearchCall arguments (arbitrary JSON)

	// FunctionCallOutput / CustomToolCallOutput variant.
	Output *FunctionCallOutputPayload

	// CustomToolCall variant.
	CustomToolCallID *string // deserialize-only
	Input            string  // custom_tool_call

	// CustomToolCallOutput variant.
	OutputName *string // custom_tool_call_output, omit if nil

	// ToolSearchOutput variant.
	ToolSearchStatus string            // required status
	Tools            []json.RawMessage // arbitrary tool descriptors

	// WebSearchCall variant.
	WebSearchID *string          // deserialize-only
	WebAction   *WebSearchAction // omit if nil

	// ImageGenerationCall variant.
	ImageID       string  // required id (NOT skip-serialized)
	ImageStatus   string  // required status
	RevisedPrompt *string // omit if nil
	Result        string  // required result

	// AgentMessage variant (0.147): a message between agents. ItemID is the
	// optional item id, serialized when present (0.147 shape).
	ItemID              *string                    // agent_message / additional_tools, omit if nil
	Author              string                     // agent_message
	Recipient           string                     // agent_message
	AgentMessageContent []AgentMessageInputContent // agent_message

	// AdditionalTools variant (0.147): Role + Tools ([]json.RawMessage) reuse
	// the Role and Tools fields above.

	// Extra captures the raw payload of an unknown ("other") item for
	// inspection. Matching Rust's lossy `#[serde(other)]`, this is NOT
	// re-serialized: an Other item always marshals to {"type":"other"}.
	Extra map[string]json.RawMessage
}

// IsUserMessage reports whether this is an ordinary user-role message.
func (r ResponseItem) IsUserMessage() bool {
	return r.Type == ResponseItemKindMessage && r.Role == "user"
}

// MarshalJSON encodes the internally-tagged ResponseItem, omitting all
// deserialize-only id fields to match the Rust serde output.
func (r ResponseItem) MarshalJSON() ([]byte, error) {
	m := map[string]any{"type": string(r.Type)}
	switch r.Type {
	case ResponseItemKindMessage:
		m["role"] = r.Role
		m["content"] = r.Content
		if r.Phase != nil {
			m["phase"] = *r.Phase
		}
	case ResponseItemKindReasoning:
		m["summary"] = r.Summary
		// content is emitted only when present AND it contains at least one
		// ReasoningText item (inverse of should_serialize_reasoning_content).
		if shouldEmitReasoningContent(r.ReasoningContent) {
			m["content"] = *r.ReasoningContent
		}
		// encrypted_content is always present (Option<String>, may be null).
		m["encrypted_content"] = r.EncryptedContent
	case ResponseItemKindLocalShellCall:
		m["call_id"] = r.CallIDOpt // Option<String>, always emitted (may be null)
		m["status"] = r.LocalShellStatus
		m["action"] = r.Action
	case ResponseItemKindFunctionCall:
		m["name"] = r.Name
		if r.Namespace != nil {
			m["namespace"] = *r.Namespace
		}
		m["arguments"] = r.Arguments
		m["call_id"] = r.CallID
	case ResponseItemKindToolSearchCall:
		m["call_id"] = r.CallIDOpt // always emitted (may be null)
		if r.StatusOpt != nil {
			m["status"] = *r.StatusOpt
		}
		m["execution"] = r.Execution
		if r.ArgumentsValue != nil {
			m["arguments"] = r.ArgumentsValue
		} else {
			m["arguments"] = json.RawMessage("null")
		}
	case ResponseItemKindFunctionCallOutput:
		m["call_id"] = r.CallID
		m["output"] = r.Output
	case ResponseItemKindCustomToolCall:
		if r.StatusOpt != nil {
			m["status"] = *r.StatusOpt
		}
		m["call_id"] = r.CallID
		m["name"] = r.Name
		m["input"] = r.Input
	case ResponseItemKindCustomToolCallOutput:
		m["call_id"] = r.CallID
		if r.OutputName != nil {
			m["name"] = *r.OutputName
		}
		m["output"] = r.Output
	case ResponseItemKindToolSearchOutput:
		m["call_id"] = r.CallIDOpt // always emitted (may be null)
		m["status"] = r.ToolSearchStatus
		m["execution"] = r.Execution
		m["tools"] = r.Tools
	case ResponseItemKindWebSearchCall:
		if r.StatusOpt != nil {
			m["status"] = *r.StatusOpt
		}
		if r.WebAction != nil {
			m["action"] = *r.WebAction
		}
	case ResponseItemKindImageGenerationCall:
		m["id"] = r.ImageID
		m["status"] = r.ImageStatus
		if r.RevisedPrompt != nil {
			m["revised_prompt"] = *r.RevisedPrompt
		}
		m["result"] = r.Result
	case ResponseItemKindCompaction:
		// encrypted_content is a required String here.
		ec := ""
		if r.EncryptedContent != nil {
			ec = *r.EncryptedContent
		}
		m["encrypted_content"] = ec
	case ResponseItemKindCompactionTrigger:
		// unit variant; only the type tag.
	case ResponseItemKindContextCompaction:
		if r.EncryptedContent != nil {
			m["encrypted_content"] = *r.EncryptedContent
		}
	case ResponseItemKindAgentMessage:
		if r.ItemID != nil {
			m["id"] = *r.ItemID
		}
		m["author"] = r.Author
		m["recipient"] = r.Recipient
		m["content"] = r.AgentMessageContent
	case ResponseItemKindAdditionalTools:
		if r.ItemID != nil {
			m["id"] = *r.ItemID
		}
		m["role"] = r.Role
		m["tools"] = r.Tools
	case ResponseItemKindOther:
		// Rust's `#[serde(other)]` Other is a lossy unit variant: it discards
		// the original payload and serializes as just {"type":"other"}. We match
		// that wire behavior exactly. The captured Extra is retained on the Go
		// value for inspection only and is intentionally NOT re-emitted.
	default:
		return nil, fmt.Errorf("unknown ResponseItem type: %q", r.Type)
	}
	return json.Marshal(m)
}

func shouldEmitReasoningContent(content *[]ReasoningItemContent) bool {
	if content == nil {
		return false
	}
	for _, c := range *content {
		if c.Type == ReasoningItemContentKindReasoningText {
			return true
		}
	}
	return false
}

// UnmarshalJSON decodes the internally-tagged ResponseItem, mapping unknown
// types (and the explicit "other" type) to the Other variant.
func (r *ResponseItem) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type             string                          `json:"type"`
		ID               json.RawMessage                 `json:"id"`
		Role             string                          `json:"role"`
		Content          json.RawMessage                 `json:"content"`
		Phase            *MessagePhase                   `json:"phase"`
		Summary          []ReasoningItemReasoningSummary `json:"summary"`
		EncryptedContent *string                         `json:"encrypted_content"`
		CallID           json.RawMessage                 `json:"call_id"`
		Status           json.RawMessage                 `json:"status"`
		Action           json.RawMessage                 `json:"action"`
		Name             *string                         `json:"name"`
		Namespace        *string                         `json:"namespace"`
		Arguments        json.RawMessage                 `json:"arguments"`
		Execution        string                          `json:"execution"`
		Output           *FunctionCallOutputPayload      `json:"output"`
		Input            string                          `json:"input"`
		Tools            []json.RawMessage               `json:"tools"`
		RevisedPrompt    *string                         `json:"revised_prompt"`
		Result           string                          `json:"result"`
		Author           string                          `json:"author"`
		Recipient        string                          `json:"recipient"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	out := ResponseItem{EncryptedContent: raw.EncryptedContent}
	kind := ResponseItemKind(raw.Type)
	// "compaction_summary" is a deserialize alias for Compaction.
	if raw.Type == "compaction_summary" {
		kind = ResponseItemKindCompaction
	}

	switch kind {
	case ResponseItemKindMessage:
		out.Type = kind
		out.MessageID = optStringFromRaw(raw.ID)
		out.Role = raw.Role
		if len(raw.Content) > 0 {
			if err := json.Unmarshal(raw.Content, &out.Content); err != nil {
				return err
			}
		}
		out.Phase = raw.Phase
	case ResponseItemKindReasoning:
		out.Type = kind
		if raw.ID != nil {
			_ = json.Unmarshal(raw.ID, &out.ReasoningID)
		}
		out.Summary = raw.Summary
		if len(raw.Content) > 0 && string(raw.Content) != "null" {
			var rc []ReasoningItemContent
			if err := json.Unmarshal(raw.Content, &rc); err != nil {
				return err
			}
			out.ReasoningContent = &rc
		}
	case ResponseItemKindLocalShellCall:
		out.Type = kind
		out.LocalShellID = optStringFromRaw(raw.ID)
		out.CallIDOpt = optStringFromRaw(raw.CallID)
		if len(raw.Status) > 0 {
			_ = json.Unmarshal(raw.Status, &out.LocalShellStatus)
		}
		if len(raw.Action) > 0 && string(raw.Action) != "null" {
			var a LocalShellAction
			if err := json.Unmarshal(raw.Action, &a); err != nil {
				return err
			}
			out.Action = &a
		}
	case ResponseItemKindFunctionCall:
		out.Type = kind
		out.FunctionCallID = optStringFromRaw(raw.ID)
		if raw.Name != nil {
			out.Name = *raw.Name
		}
		out.Namespace = raw.Namespace
		if len(raw.Arguments) > 0 {
			_ = json.Unmarshal(raw.Arguments, &out.Arguments)
		}
		out.CallID = stringFromRaw(raw.CallID)
	case ResponseItemKindToolSearchCall:
		out.Type = kind
		out.ToolSearchCallID = optStringFromRaw(raw.ID)
		out.CallIDOpt = optStringFromRaw(raw.CallID)
		out.StatusOpt = optStringFromRaw(raw.Status)
		out.Execution = raw.Execution
		out.ArgumentsValue = raw.Arguments
	case ResponseItemKindFunctionCallOutput:
		out.Type = kind
		out.CallID = stringFromRaw(raw.CallID)
		out.Output = raw.Output
	case ResponseItemKindCustomToolCall:
		out.Type = kind
		out.CustomToolCallID = optStringFromRaw(raw.ID)
		out.StatusOpt = optStringFromRaw(raw.Status)
		out.CallID = stringFromRaw(raw.CallID)
		if raw.Name != nil {
			out.Name = *raw.Name
		}
		out.Input = raw.Input
	case ResponseItemKindCustomToolCallOutput:
		out.Type = kind
		out.CallID = stringFromRaw(raw.CallID)
		out.OutputName = raw.Name
		out.Output = raw.Output
	case ResponseItemKindToolSearchOutput:
		out.Type = kind
		out.CallIDOpt = optStringFromRaw(raw.CallID)
		out.ToolSearchStatus = stringFromRaw(raw.Status)
		out.Execution = raw.Execution
		out.Tools = raw.Tools
	case ResponseItemKindWebSearchCall:
		out.Type = kind
		out.WebSearchID = optStringFromRaw(raw.ID)
		out.StatusOpt = optStringFromRaw(raw.Status)
		if len(raw.Action) > 0 && string(raw.Action) != "null" {
			var wa WebSearchAction
			if err := json.Unmarshal(raw.Action, &wa); err != nil {
				return err
			}
			out.WebAction = &wa
		}
	case ResponseItemKindImageGenerationCall:
		out.Type = kind
		out.ImageID = stringFromRaw(raw.ID)
		out.ImageStatus = stringFromRaw(raw.Status)
		out.RevisedPrompt = raw.RevisedPrompt
		out.Result = raw.Result
	case ResponseItemKindCompaction:
		out.Type = kind
		// encrypted_content already captured.
	case ResponseItemKindCompactionTrigger:
		out.Type = kind
	case ResponseItemKindContextCompaction:
		out.Type = kind
	case ResponseItemKindAgentMessage:
		out.Type = kind
		out.ItemID = optStringFromRaw(raw.ID)
		out.Author = raw.Author
		out.Recipient = raw.Recipient
		if len(raw.Content) > 0 {
			if err := json.Unmarshal(raw.Content, &out.AgentMessageContent); err != nil {
				return err
			}
		}
	case ResponseItemKindAdditionalTools:
		out.Type = kind
		out.ItemID = optStringFromRaw(raw.ID)
		out.Role = raw.Role
		out.Tools = raw.Tools
	default:
		out.Type = ResponseItemKindOther
		var extra map[string]json.RawMessage
		if err := json.Unmarshal(data, &extra); err != nil {
			return err
		}
		out.Extra = extra
	}
	*r = out
	return nil
}

// stringFromRaw decodes a JSON string from a RawMessage, returning "" for
// absent/null/non-string values.
func stringFromRaw(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// optStringFromRaw decodes an optional JSON string from a RawMessage, returning
// nil for absent/null values.
func optStringFromRaw(raw json.RawMessage) *string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil
	}
	return &s
}

// AgentMessageInputContentKind tags an [AgentMessageInputContent] part.
type AgentMessageInputContentKind string

const (
	// AgentMessageInputContentKindInputText is plaintext.
	AgentMessageInputContentKindInputText AgentMessageInputContentKind = "input_text"
	// AgentMessageInputContentKindEncryptedContent is provider-encrypted text.
	AgentMessageInputContentKindEncryptedContent AgentMessageInputContentKind = "encrypted_content"
)

// AgentMessageInputContent is one part of an inter-agent message (0.147
// `AgentMessageInputContent`, internally tagged on `type`).
type AgentMessageInputContent struct {
	Type AgentMessageInputContentKind
	// Text is set for input_text.
	Text string
	// EncryptedContent is set for encrypted_content.
	EncryptedContent string
}

// MarshalJSON emits the internally-tagged part.
func (c AgentMessageInputContent) MarshalJSON() ([]byte, error) {
	m := map[string]any{"type": string(c.Type)}
	switch c.Type {
	case AgentMessageInputContentKindEncryptedContent:
		m["encrypted_content"] = c.EncryptedContent
	default:
		m["text"] = c.Text
	}
	return json.Marshal(m)
}

// UnmarshalJSON decodes the internally-tagged part.
func (c *AgentMessageInputContent) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type             AgentMessageInputContentKind `json:"type"`
		Text             string                       `json:"text"`
		EncryptedContent string                       `json:"encrypted_content"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*c = AgentMessageInputContent{Type: raw.Type, Text: raw.Text, EncryptedContent: raw.EncryptedContent}
	return nil
}

// PlaintextAgentMessageContent returns the locally readable text when an agent
// message is entirely plaintext, mirroring Rust
// `plaintext_agent_message_content`: parts are joined with "\n"; ok is false
// when any part is encrypted or the joined text is blank.
func PlaintextAgentMessageContent(content []AgentMessageInputContent) (text string, ok bool) {
	parts := make([]string, 0, len(content))
	for _, part := range content {
		if part.Type == AgentMessageInputContentKindEncryptedContent {
			return "", false
		}
		parts = append(parts, part.Text)
	}
	joined := strings.Join(parts, "\n")
	if strings.TrimSpace(joined) == "" {
		return "", false
	}
	return joined, true
}
