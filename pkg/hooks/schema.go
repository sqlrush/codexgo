package hooks

import "encoding/json"

// nullableString mirrors the Rust NullableString: a transparent Option<String>
// that always serializes (as the string or JSON null) rather than being
// omitted. It is used for fields like transcript_path and last_assistant_message.
type nullableString struct {
	value *string
}

func nullableFromString(value *string) nullableString { return nullableString{value: value} }

func (n nullableString) MarshalJSON() ([]byte, error) {
	if n.value == nil {
		return []byte("null"), nil
	}
	return json.Marshal(*n.value)
}

// subagentCommandInputFields carries the optional agent_id/agent_type fields
// that are flattened into per-event command inputs when a hook runs inside a
// thread-spawned subagent. Mirrors SubagentCommandInputFields.
type subagentCommandInputFields struct {
	agentID   *string
	agentType *string
}

func subagentFieldsFrom(ctx *SubagentHookContext) subagentCommandInputFields {
	if ctx == nil {
		return subagentCommandInputFields{}
	}
	id := ctx.AgentID
	typ := ctx.AgentType
	return subagentCommandInputFields{agentID: &id, agentType: &typ}
}

// SubagentHookContext identifies a thread-spawned subagent when a normal hook
// runs inside it. Mirrors SubagentHookContext.
type SubagentHookContext struct {
	AgentID   string
	AgentType string
}

// --- Command input wire types (serialized to hook stdin) ---
//
// Field order matches the Rust struct declaration order, which serde preserves.
// Optional agent_id/agent_type use omitempty to mirror skip_serializing_if.

type preToolUseCommandInput struct {
	SessionID      string          `json:"session_id"`
	TurnID         string          `json:"turn_id"`
	AgentID        *string         `json:"agent_id,omitempty"`
	AgentType      *string         `json:"agent_type,omitempty"`
	TranscriptPath nullableString  `json:"transcript_path"`
	Cwd            string          `json:"cwd"`
	HookEventName  string          `json:"hook_event_name"`
	Model          string          `json:"model"`
	PermissionMode string          `json:"permission_mode"`
	ToolName       string          `json:"tool_name"`
	ToolInput      json.RawMessage `json:"tool_input"`
	ToolUseID      string          `json:"tool_use_id"`
}

type permissionRequestCommandInput struct {
	SessionID      string          `json:"session_id"`
	TurnID         string          `json:"turn_id"`
	AgentID        *string         `json:"agent_id,omitempty"`
	AgentType      *string         `json:"agent_type,omitempty"`
	TranscriptPath nullableString  `json:"transcript_path"`
	Cwd            string          `json:"cwd"`
	HookEventName  string          `json:"hook_event_name"`
	Model          string          `json:"model"`
	PermissionMode string          `json:"permission_mode"`
	ToolName       string          `json:"tool_name"`
	ToolInput      json.RawMessage `json:"tool_input"`
}

type postToolUseCommandInput struct {
	SessionID      string          `json:"session_id"`
	TurnID         string          `json:"turn_id"`
	AgentID        *string         `json:"agent_id,omitempty"`
	AgentType      *string         `json:"agent_type,omitempty"`
	TranscriptPath nullableString  `json:"transcript_path"`
	Cwd            string          `json:"cwd"`
	HookEventName  string          `json:"hook_event_name"`
	Model          string          `json:"model"`
	PermissionMode string          `json:"permission_mode"`
	ToolName       string          `json:"tool_name"`
	ToolInput      json.RawMessage `json:"tool_input"`
	ToolResponse   json.RawMessage `json:"tool_response"`
	ToolUseID      string          `json:"tool_use_id"`
}

type preCompactCommandInput struct {
	SessionID      string         `json:"session_id"`
	TurnID         string         `json:"turn_id"`
	AgentID        *string        `json:"agent_id,omitempty"`
	AgentType      *string        `json:"agent_type,omitempty"`
	TranscriptPath nullableString `json:"transcript_path"`
	Cwd            string         `json:"cwd"`
	HookEventName  string         `json:"hook_event_name"`
	Model          string         `json:"model"`
	Trigger        string         `json:"trigger"`
}

type postCompactCommandInput struct {
	SessionID      string         `json:"session_id"`
	TurnID         string         `json:"turn_id"`
	AgentID        *string        `json:"agent_id,omitempty"`
	AgentType      *string        `json:"agent_type,omitempty"`
	TranscriptPath nullableString `json:"transcript_path"`
	Cwd            string         `json:"cwd"`
	HookEventName  string         `json:"hook_event_name"`
	Model          string         `json:"model"`
	Trigger        string         `json:"trigger"`
}

type sessionStartCommandInput struct {
	SessionID      string         `json:"session_id"`
	TranscriptPath nullableString `json:"transcript_path"`
	Cwd            string         `json:"cwd"`
	HookEventName  string         `json:"hook_event_name"`
	Model          string         `json:"model"`
	PermissionMode string         `json:"permission_mode"`
	Source         string         `json:"source"`
}

type subagentStartCommandInput struct {
	SessionID      string         `json:"session_id"`
	TurnID         string         `json:"turn_id"`
	TranscriptPath nullableString `json:"transcript_path"`
	Cwd            string         `json:"cwd"`
	HookEventName  string         `json:"hook_event_name"`
	Model          string         `json:"model"`
	PermissionMode string         `json:"permission_mode"`
	AgentID        string         `json:"agent_id"`
	AgentType      string         `json:"agent_type"`
}

type userPromptSubmitCommandInput struct {
	SessionID      string         `json:"session_id"`
	TurnID         string         `json:"turn_id"`
	AgentID        *string        `json:"agent_id,omitempty"`
	AgentType      *string        `json:"agent_type,omitempty"`
	TranscriptPath nullableString `json:"transcript_path"`
	Cwd            string         `json:"cwd"`
	HookEventName  string         `json:"hook_event_name"`
	Model          string         `json:"model"`
	PermissionMode string         `json:"permission_mode"`
	Prompt         string         `json:"prompt"`
}

type stopCommandInput struct {
	SessionID            string         `json:"session_id"`
	TurnID               string         `json:"turn_id"`
	TranscriptPath       nullableString `json:"transcript_path"`
	Cwd                  string         `json:"cwd"`
	HookEventName        string         `json:"hook_event_name"`
	Model                string         `json:"model"`
	PermissionMode       string         `json:"permission_mode"`
	StopHookActive       bool           `json:"stop_hook_active"`
	LastAssistantMessage nullableString `json:"last_assistant_message"`
}

type subagentStopCommandInput struct {
	SessionID            string         `json:"session_id"`
	TurnID               string         `json:"turn_id"`
	TranscriptPath       nullableString `json:"transcript_path"`
	AgentTranscriptPath  nullableString `json:"agent_transcript_path"`
	Cwd                  string         `json:"cwd"`
	HookEventName        string         `json:"hook_event_name"`
	Model                string         `json:"model"`
	PermissionMode       string         `json:"permission_mode"`
	StopHookActive       bool           `json:"stop_hook_active"`
	AgentID              string         `json:"agent_id"`
	AgentType            string         `json:"agent_type"`
	LastAssistantMessage nullableString `json:"last_assistant_message"`
}
