package protocol

import (
	"encoding/json"
	"fmt"
)

// This file ports `items.rs`: the TurnItem family of streamed conversation
// items and their payload structs.
//
// Some fields reference types owned by other protocol modules that later agents
// will port (FileChange, PatchApplyStatus from protocol.rs; CallToolResult from
// mcp.rs). To avoid redefining types that belong to those modules, those fields
// are modeled here as json.RawMessage so they round-trip losslessly. When the
// owning types land, callers may decode these raw payloads with them.

// TurnItemKind is the discriminator for TurnItem. Rust uses
// `#[serde(tag = "type")]` WITHOUT rename_all, so the discriminator values are
// the variant identifiers verbatim (PascalCase).
type TurnItemKind string

const (
	TurnItemKindUserMessage       TurnItemKind = "UserMessage"
	TurnItemKindHookPrompt        TurnItemKind = "HookPrompt"
	TurnItemKindAgentMessage      TurnItemKind = "AgentMessage"
	TurnItemKindPlan              TurnItemKind = "Plan"
	TurnItemKindReasoning         TurnItemKind = "Reasoning"
	TurnItemKindWebSearch         TurnItemKind = "WebSearch"
	TurnItemKindImageView         TurnItemKind = "ImageView"
	TurnItemKindImageGeneration   TurnItemKind = "ImageGeneration"
	TurnItemKindFileChange        TurnItemKind = "FileChange"
	TurnItemKindMcpToolCall       TurnItemKind = "McpToolCall"
	TurnItemKindContextCompaction TurnItemKind = "ContextCompaction"
	// TurnItemKindCollabAgentToolCall was added upstream in 0.147 (spec 50 D0.7):
	// the lifecycle item for spawn/send_input/resume/wait/close collab tools.
	TurnItemKindCollabAgentToolCall TurnItemKind = "CollabAgentToolCall"
)

// TurnItem is a single item in a turn-item stream. It is internally tagged on
// "type"; exactly one of the variant pointers below is non-nil.
type TurnItem struct {
	Type TurnItemKind

	UserMessage         *UserMessageItem
	HookPrompt          *HookPromptItem
	AgentMessage        *AgentMessageItem
	Plan                *PlanItem
	Reasoning           *ReasoningTurnItem
	WebSearch           *WebSearchItem
	ImageView           *ImageViewItem
	ImageGeneration     *ImageGenerationItem
	FileChange          *FileChangeItem
	McpToolCall         *McpToolCallItem
	ContextCompaction   *ContextCompactionItem
	CollabAgentToolCall *CollabAgentToolCallItem
}

// ID returns the id of the active variant.
func (t TurnItem) ID() string {
	switch t.Type {
	case TurnItemKindUserMessage:
		return t.UserMessage.ID
	case TurnItemKindHookPrompt:
		return t.HookPrompt.ID
	case TurnItemKindAgentMessage:
		return t.AgentMessage.ID
	case TurnItemKindPlan:
		return t.Plan.ID
	case TurnItemKindReasoning:
		return t.Reasoning.ID
	case TurnItemKindWebSearch:
		return t.WebSearch.ID
	case TurnItemKindImageView:
		return t.ImageView.ID
	case TurnItemKindImageGeneration:
		return t.ImageGeneration.ID
	case TurnItemKindFileChange:
		return t.FileChange.ID
	case TurnItemKindMcpToolCall:
		return t.McpToolCall.ID
	case TurnItemKindContextCompaction:
		return t.ContextCompaction.ID
	case TurnItemKindCollabAgentToolCall:
		return t.CollabAgentToolCall.ID
	default:
		return ""
	}
}

// MarshalJSON emits the active variant's payload merged with the "type" tag.
func (t TurnItem) MarshalJSON() ([]byte, error) {
	var payload any
	switch t.Type {
	case TurnItemKindUserMessage:
		payload = t.UserMessage
	case TurnItemKindHookPrompt:
		payload = t.HookPrompt
	case TurnItemKindAgentMessage:
		payload = t.AgentMessage
	case TurnItemKindPlan:
		payload = t.Plan
	case TurnItemKindReasoning:
		payload = t.Reasoning
	case TurnItemKindWebSearch:
		payload = t.WebSearch
	case TurnItemKindImageView:
		payload = t.ImageView
	case TurnItemKindImageGeneration:
		payload = t.ImageGeneration
	case TurnItemKindFileChange:
		payload = t.FileChange
	case TurnItemKindMcpToolCall:
		payload = t.McpToolCall
	case TurnItemKindContextCompaction:
		payload = t.ContextCompaction
	case TurnItemKindCollabAgentToolCall:
		payload = t.CollabAgentToolCall
	default:
		return nil, fmt.Errorf("unknown TurnItem type: %q", t.Type)
	}
	return mergeTaggedObject(string(t.Type), payload)
}

// UnmarshalJSON decodes the internally-tagged TurnItem.
func (t *TurnItem) UnmarshalJSON(data []byte) error {
	var probe struct {
		Type TurnItemKind `json:"type"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	out := TurnItem{Type: probe.Type}
	var err error
	switch probe.Type {
	case TurnItemKindUserMessage:
		out.UserMessage = new(UserMessageItem)
		err = json.Unmarshal(data, out.UserMessage)
	case TurnItemKindHookPrompt:
		out.HookPrompt = new(HookPromptItem)
		err = json.Unmarshal(data, out.HookPrompt)
	case TurnItemKindAgentMessage:
		out.AgentMessage = new(AgentMessageItem)
		err = json.Unmarshal(data, out.AgentMessage)
	case TurnItemKindPlan:
		out.Plan = new(PlanItem)
		err = json.Unmarshal(data, out.Plan)
	case TurnItemKindReasoning:
		out.Reasoning = new(ReasoningTurnItem)
		err = json.Unmarshal(data, out.Reasoning)
	case TurnItemKindWebSearch:
		out.WebSearch = new(WebSearchItem)
		err = json.Unmarshal(data, out.WebSearch)
	case TurnItemKindImageView:
		out.ImageView = new(ImageViewItem)
		err = json.Unmarshal(data, out.ImageView)
	case TurnItemKindImageGeneration:
		out.ImageGeneration = new(ImageGenerationItem)
		err = json.Unmarshal(data, out.ImageGeneration)
	case TurnItemKindFileChange:
		out.FileChange = new(FileChangeItem)
		err = json.Unmarshal(data, out.FileChange)
	case TurnItemKindMcpToolCall:
		out.McpToolCall = new(McpToolCallItem)
		err = json.Unmarshal(data, out.McpToolCall)
	case TurnItemKindContextCompaction:
		out.ContextCompaction = new(ContextCompactionItem)
		err = json.Unmarshal(data, out.ContextCompaction)
	case TurnItemKindCollabAgentToolCall:
		out.CollabAgentToolCall = new(CollabAgentToolCallItem)
		err = json.Unmarshal(data, out.CollabAgentToolCall)
	default:
		return fmt.Errorf("unknown TurnItem type: %q", probe.Type)
	}
	if err != nil {
		return err
	}
	*t = out
	return nil
}

// mergeTaggedObject marshals payload to an object and injects the "type" tag.
func mergeTaggedObject(typeTag string, payload any) ([]byte, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]json.RawMessage{}
	}
	m["type"] = json.RawMessage(`"` + typeTag + `"`)
	return json.Marshal(m)
}

// UserMessageItem is a user-authored turn item. `client_id` is omitted when nil.
type UserMessageItem struct {
	ID       string      `json:"id"`
	ClientID *string     `json:"client_id,omitempty"`
	Content  []UserInput `json:"content"`
}

// HookPromptItem carries one or more hook prompt fragments.
type HookPromptItem struct {
	ID        string               `json:"id"`
	Fragments []HookPromptFragment `json:"fragments"`
}

// HookPromptFragment is a single hook prompt fragment. Rust: camelCase.
type HookPromptFragment struct {
	Text      string `json:"text"`
	HookRunID string `json:"hookRunId"`
}

// AgentMessageContentKind is the discriminator for AgentMessageContent. Rust:
// `#[serde(tag = "type")]` (no rename_all), only the Text variant.
type AgentMessageContentKind string

const (
	AgentMessageContentKindText AgentMessageContentKind = "Text"
)

// AgentMessageContent is an internally-tagged assistant content chunk.
type AgentMessageContent struct {
	Type AgentMessageContentKind `json:"type"`
	Text string                  `json:"text"`
}

// NewAgentMessageText builds a Text AgentMessageContent.
func NewAgentMessageText(text string) AgentMessageContent {
	return AgentMessageContent{Type: AgentMessageContentKindText, Text: text}
}

// AgentMessageItem is an assistant-authored message turn item. `phase` and
// `memory_citation` are omitted when nil.
type AgentMessageItem struct {
	ID             string                `json:"id"`
	Content        []AgentMessageContent `json:"content"`
	Phase          *MessagePhase         `json:"phase,omitempty"`
	MemoryCitation *MemoryCitation       `json:"memory_citation,omitempty"`
}

// PlanItem is a plan-text turn item.
type PlanItem struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// ReasoningTurnItem is the reasoning turn item (named to avoid colliding with
// the Responses-API ReasoningItemContent types). Maps to Rust `ReasoningItem`
// in items.rs. `raw_content` uses `#[serde(default)]`.
type ReasoningTurnItem struct {
	ID          string   `json:"id"`
	SummaryText []string `json:"summary_text"`
	RawContent  []string `json:"raw_content"`
}

// WebSearchItem is a completed web-search turn item.
type WebSearchItem struct {
	ID     string          `json:"id"`
	Query  string          `json:"query"`
	Action WebSearchAction `json:"action"`
}

// ImageViewItem records that an image was viewed.
type ImageViewItem struct {
	ID   string       `json:"id"`
	Path AbsolutePath `json:"path"`
}

// ImageGenerationItem records an image generation result. Optional fields are
// omitted when nil.
type ImageGenerationItem struct {
	ID            string        `json:"id"`
	Status        string        `json:"status"`
	RevisedPrompt *string       `json:"revised_prompt,omitempty"`
	Result        string        `json:"result"`
	SavedPath     *AbsolutePath `json:"saved_path,omitempty"`
}

// FileChangeItem records a set of file changes from a patch apply.
//
// `changes` maps a path to a FileChange (owned by protocol.rs); it is kept as
// raw JSON here. `status` is a PatchApplyStatus (also owned by protocol.rs),
// kept raw and omitted when absent. The remaining optional fields are omitted
// when nil.
type FileChangeItem struct {
	ID           string                     `json:"id"`
	Changes      map[string]json.RawMessage `json:"changes"`
	Status       json.RawMessage            `json:"status,omitempty"`
	AutoApproved *bool                      `json:"auto_approved,omitempty"`
	Stdout       *string                    `json:"stdout,omitempty"`
	Stderr       *string                    `json:"stderr,omitempty"`
}

// McpToolCallStatus is the status of an MCP tool call turn item. Rust:
// camelCase.
type McpToolCallStatus string

const (
	McpToolCallStatusInProgress McpToolCallStatus = "inProgress"
	McpToolCallStatusCompleted  McpToolCallStatus = "completed"
	McpToolCallStatusFailed     McpToolCallStatus = "failed"
)

// McpToolCallError carries an MCP tool call error message. Rust: camelCase.
type McpToolCallError struct {
	Message string `json:"message"`
}

// McpToolCallItem records an MCP tool invocation. Rust: camelCase.
//
// `result` is a CallToolResult (owned by mcp.rs) and is kept as raw JSON here.
// `duration` is a Rust Duration serialized as a string ("#[ts(type = "string")]"
// with the default serde Duration encoding); it is kept raw and omitted when
// absent.
type McpToolCallItem struct {
	ID                string            `json:"id"`
	Server            string            `json:"server"`
	Tool              string            `json:"tool"`
	Arguments         json.RawMessage   `json:"arguments"`
	McpAppResourceURI *string           `json:"mcpAppResourceUri,omitempty"`
	PluginID          *string           `json:"pluginId,omitempty"`
	Status            McpToolCallStatus `json:"status"`
	Result            json.RawMessage   `json:"result,omitempty"`
	Error             *McpToolCallError `json:"error,omitempty"`
	Duration          json.RawMessage   `json:"duration,omitempty"`
}

// ContextCompactionItem marks a context-compaction turn item.
type ContextCompactionItem struct {
	ID string `json:"id"`
}

// ----------------------------------------------------------------------------
// CollabAgentToolCall (0.147, spec 50 D0.7)
// ----------------------------------------------------------------------------

// CollabAgentTool names the multi-agent v1 tool a [CollabAgentToolCallItem]
// reports on. Mirrors Rust `CollabAgentTool` (`rename_all = "snake_case"`).
type CollabAgentTool string

const (
	CollabAgentToolSpawnAgent  CollabAgentTool = "spawn_agent"
	CollabAgentToolSendInput   CollabAgentTool = "send_input"
	CollabAgentToolResumeAgent CollabAgentTool = "resume_agent"
	CollabAgentToolWait        CollabAgentTool = "wait"
	CollabAgentToolCloseAgent  CollabAgentTool = "close_agent"
)

// CollabAgentToolCallStatus is the lifecycle status of a collab tool call.
// Mirrors Rust `CollabAgentToolCallStatus` (`rename_all = "snake_case"`).
type CollabAgentToolCallStatus string

const (
	CollabAgentToolCallStatusInProgress CollabAgentToolCallStatus = "in_progress"
	CollabAgentToolCallStatusCompleted  CollabAgentToolCallStatus = "completed"
	// CollabAgentToolCallStatusFailed marks a call whose sub-agent(s) ended in
	// Errored/NotFound (upstream `wait_tool_call_status`) or whose dispatch
	// failed — the failure is surfaced to the parent instead of an empty success.
	CollabAgentToolCallStatusFailed CollabAgentToolCallStatus = "failed"
)

// CollabAgentToolCallItem is the streamed lifecycle item for a collab tool call.
// Mirrors Rust `CollabAgentToolCallItem`. AgentsStates maps ThreadID (string
// on the wire) to the agent's status.
type CollabAgentToolCallItem struct {
	ID                string                    `json:"id"`
	Tool              CollabAgentTool           `json:"tool"`
	Status            CollabAgentToolCallStatus `json:"status"`
	SenderThreadID    ThreadID                  `json:"sender_thread_id"`
	ReceiverThreadIDs []ThreadID                `json:"receiver_thread_ids"`
	ReceiverAgents    []CollabAgentRef          `json:"receiver_agents"`
	Prompt            *string                   `json:"prompt,omitempty"`
	Model             *string                   `json:"model,omitempty"`
	ReasoningEffort   *ReasoningEffort          `json:"reasoning_effort,omitempty"`
	AgentsStates      map[string]AgentStatus    `json:"agents_states"`
}

// WaitToolCallStatus derives the wait call's status from the final statuses of
// its targets, mirroring upstream `wait_tool_call_status`: any Errored or
// NotFound target makes the call Failed, otherwise Completed.
func WaitToolCallStatus(statuses map[string]AgentStatus) CollabAgentToolCallStatus {
	for _, st := range statuses {
		if st.Kind == AgentStatusErrored || st.Kind == AgentStatusNotFound {
			return CollabAgentToolCallStatusFailed
		}
	}
	return CollabAgentToolCallStatusCompleted
}
