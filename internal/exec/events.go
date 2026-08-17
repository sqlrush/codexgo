package exec

import (
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// This file ports the JSONL event vocabulary emitted by `codex exec` from
// codex-rs/exec/src/exec_events.rs. ThreadEvent is the top-level externally
// discriminated union (serde tag = "type") written one-per-line in --json mode.
// Each ThreadItem detail variant is internally tagged on "type" with snake_case
// names, flattened alongside the item id.
//
// Fidelity: the wire shapes here match the Rust ts-rs/serde derives exactly so
// real clients consuming the JSONL stream see identical bytes.

// ThreadEventKind is the "type" discriminator for a [ThreadEvent].
type ThreadEventKind string

const (
	// ThreadEventKindThreadStarted is emitted first when a thread is created.
	ThreadEventKindThreadStarted ThreadEventKind = "thread.started"
	// ThreadEventKindTurnStarted is emitted when a turn begins.
	ThreadEventKindTurnStarted ThreadEventKind = "turn.started"
	// ThreadEventKindTurnCompleted is emitted when a turn finishes successfully.
	ThreadEventKindTurnCompleted ThreadEventKind = "turn.completed"
	// ThreadEventKindTurnFailed is emitted when a turn fails.
	ThreadEventKindTurnFailed ThreadEventKind = "turn.failed"
	// ThreadEventKindItemStarted is emitted when a new item begins.
	ThreadEventKindItemStarted ThreadEventKind = "item.started"
	// ThreadEventKindItemUpdated is emitted when an item changes.
	ThreadEventKindItemUpdated ThreadEventKind = "item.updated"
	// ThreadEventKindItemCompleted is emitted when an item reaches a terminal state.
	ThreadEventKindItemCompleted ThreadEventKind = "item.completed"
	// ThreadEventKindError is an unrecoverable stream error.
	ThreadEventKindError ThreadEventKind = "error"
)

// ThreadEvent is one line of the exec JSONL stream. Exactly one payload field is
// populated, selected by Kind. It serializes as the externally-tagged Rust enum:
// the variant payload merged with a leading "type" tag.
type ThreadEvent struct {
	Kind ThreadEventKind

	ThreadStarted *ThreadStartedEvent
	TurnStarted   *TurnStartedEvent
	TurnCompleted *TurnCompletedEvent
	TurnFailed    *TurnFailedEvent
	ItemStarted   *ItemEvent
	ItemUpdated   *ItemEvent
	ItemCompleted *ItemEvent
	Error         *ThreadErrorEvent
}

// MarshalJSON emits the active variant payload flattened with its "type" tag.
func (e ThreadEvent) MarshalJSON() ([]byte, error) {
	var payload any
	switch e.Kind {
	case ThreadEventKindThreadStarted:
		payload = e.ThreadStarted
	case ThreadEventKindTurnStarted:
		payload = e.TurnStarted
	case ThreadEventKindTurnCompleted:
		payload = e.TurnCompleted
	case ThreadEventKindTurnFailed:
		payload = e.TurnFailed
	case ThreadEventKindItemStarted:
		payload = e.ItemStarted
	case ThreadEventKindItemUpdated:
		payload = e.ItemUpdated
	case ThreadEventKindItemCompleted:
		payload = e.ItemCompleted
	case ThreadEventKindError:
		payload = e.Error
	default:
		return nil, fmt.Errorf("exec: unknown ThreadEvent kind: %q", e.Kind)
	}
	if payload == nil {
		return nil, fmt.Errorf("exec: ThreadEvent kind %q has nil payload", e.Kind)
	}
	return mergeTaggedObject(string(e.Kind), payload)
}

// UnmarshalJSON decodes the externally-tagged ThreadEvent by its "type" tag.
func (e *ThreadEvent) UnmarshalJSON(data []byte) error {
	var tag struct {
		Type ThreadEventKind `json:"type"`
	}
	if err := json.Unmarshal(data, &tag); err != nil {
		return fmt.Errorf("exec: ThreadEvent missing type tag: %w", err)
	}
	out := ThreadEvent{Kind: tag.Type}
	var err error
	switch tag.Type {
	case ThreadEventKindThreadStarted:
		out.ThreadStarted = new(ThreadStartedEvent)
		err = json.Unmarshal(data, out.ThreadStarted)
	case ThreadEventKindTurnStarted:
		out.TurnStarted = new(TurnStartedEvent)
		err = json.Unmarshal(data, out.TurnStarted)
	case ThreadEventKindTurnCompleted:
		out.TurnCompleted = new(TurnCompletedEvent)
		err = json.Unmarshal(data, out.TurnCompleted)
	case ThreadEventKindTurnFailed:
		out.TurnFailed = new(TurnFailedEvent)
		err = json.Unmarshal(data, out.TurnFailed)
	case ThreadEventKindItemStarted:
		out.ItemStarted = new(ItemEvent)
		err = json.Unmarshal(data, out.ItemStarted)
	case ThreadEventKindItemUpdated:
		out.ItemUpdated = new(ItemEvent)
		err = json.Unmarshal(data, out.ItemUpdated)
	case ThreadEventKindItemCompleted:
		out.ItemCompleted = new(ItemEvent)
		err = json.Unmarshal(data, out.ItemCompleted)
	case ThreadEventKindError:
		out.Error = new(ThreadErrorEvent)
		err = json.Unmarshal(data, out.Error)
	default:
		return fmt.Errorf("exec: unknown ThreadEvent type tag: %q", tag.Type)
	}
	if err != nil {
		return err
	}
	*e = out
	return nil
}

// ThreadStartedEvent identifies a newly started thread.
type ThreadStartedEvent struct {
	ThreadID string `json:"thread_id"`
}

// TurnStartedEvent marks the start of a turn. It carries no payload.
type TurnStartedEvent struct{}

// TurnCompletedEvent reports a successful turn with its token usage.
type TurnCompletedEvent struct {
	Usage Usage `json:"usage"`
}

// TurnFailedEvent reports a failed turn with its error.
type TurnFailedEvent struct {
	Error ThreadErrorEvent `json:"error"`
}

// Usage reports token usage accumulated during a turn. All counts are i64.
type Usage struct {
	InputTokens           int64 `json:"input_tokens"`
	CachedInputTokens     int64 `json:"cached_input_tokens"`
	OutputTokens          int64 `json:"output_tokens"`
	ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
}

// ThreadErrorEvent is a fatal error surfaced by the stream.
type ThreadErrorEvent struct {
	Message string `json:"message"`
}

// ItemEvent wraps a [ThreadItem] for the item.started/updated/completed events.
type ItemEvent struct {
	Item ThreadItem `json:"item"`
}

// ThreadItem is one item in the exec stream: an id plus a typed detail payload.
// The detail is flattened (serde flatten) into the same JSON object as the id.
type ThreadItem struct {
	ID      string
	Details ThreadItemDetails
}

// MarshalJSON flattens the detail payload alongside the id, matching the Rust
// `#[serde(flatten)]` on ThreadItem::details.
func (t ThreadItem) MarshalJSON() ([]byte, error) {
	inner, err := json.Marshal(t.Details)
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(inner, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]json.RawMessage{}
	}
	id, err := json.Marshal(t.ID)
	if err != nil {
		return nil, err
	}
	m["id"] = id
	return json.Marshal(m)
}

// UnmarshalJSON decodes the id plus the flattened detail payload.
func (t *ThreadItem) UnmarshalJSON(data []byte) error {
	var idOnly struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &idOnly); err != nil {
		return err
	}
	t.ID = idOnly.ID
	return t.Details.UnmarshalJSON(data)
}

// ThreadItemDetailKind is the "type" discriminator for a [ThreadItemDetails].
type ThreadItemDetailKind string

const (
	// ThreadItemDetailKindAgentMessage is the agent's response message.
	ThreadItemDetailKindAgentMessage ThreadItemDetailKind = "agent_message"
	// ThreadItemDetailKindReasoning is the agent's reasoning summary.
	ThreadItemDetailKindReasoning ThreadItemDetailKind = "reasoning"
	// ThreadItemDetailKindCommandExecution tracks a command run by the agent.
	ThreadItemDetailKindCommandExecution ThreadItemDetailKind = "command_execution"
	// ThreadItemDetailKindFileChange is a set of file changes by the agent.
	ThreadItemDetailKindFileChange ThreadItemDetailKind = "file_change"
	// ThreadItemDetailKindMcpToolCall is a call to an MCP tool.
	ThreadItemDetailKindMcpToolCall ThreadItemDetailKind = "mcp_tool_call"
	// ThreadItemDetailKindWebSearch is a web-search request.
	ThreadItemDetailKindWebSearch ThreadItemDetailKind = "web_search"
	// ThreadItemDetailKindTodoList is the agent's running to-do list.
	ThreadItemDetailKindTodoList ThreadItemDetailKind = "todo_list"
	// ThreadItemDetailKindError is a non-fatal error surfaced as an item.
	ThreadItemDetailKindError ThreadItemDetailKind = "error"
)

// ThreadItemDetails is the internally-tagged (on "type") detail payload of a
// [ThreadItem]. Exactly one variant pointer is populated, selected by Kind.
type ThreadItemDetails struct {
	Kind ThreadItemDetailKind

	AgentMessage     *AgentMessageItem
	Reasoning        *ReasoningItem
	CommandExecution *CommandExecutionItem
	FileChange       *FileChangeItem
	McpToolCall      *McpToolCallItem
	WebSearch        *WebSearchItem
	TodoList         *TodoListItem
	Error            *ErrorItem
}

// MarshalJSON emits the active variant payload merged with its "type" tag.
func (d ThreadItemDetails) MarshalJSON() ([]byte, error) {
	var payload any
	switch d.Kind {
	case ThreadItemDetailKindAgentMessage:
		payload = d.AgentMessage
	case ThreadItemDetailKindReasoning:
		payload = d.Reasoning
	case ThreadItemDetailKindCommandExecution:
		payload = d.CommandExecution
	case ThreadItemDetailKindFileChange:
		payload = d.FileChange
	case ThreadItemDetailKindMcpToolCall:
		payload = d.McpToolCall
	case ThreadItemDetailKindWebSearch:
		payload = d.WebSearch
	case ThreadItemDetailKindTodoList:
		payload = d.TodoList
	case ThreadItemDetailKindError:
		payload = d.Error
	default:
		return nil, fmt.Errorf("exec: unknown ThreadItemDetails kind: %q", d.Kind)
	}
	if payload == nil {
		return nil, fmt.Errorf("exec: ThreadItemDetails kind %q has nil payload", d.Kind)
	}
	return mergeTaggedObject(string(d.Kind), payload)
}

// UnmarshalJSON decodes the internally-tagged details by its "type" tag.
func (d *ThreadItemDetails) UnmarshalJSON(data []byte) error {
	var tag struct {
		Type ThreadItemDetailKind `json:"type"`
	}
	if err := json.Unmarshal(data, &tag); err != nil {
		return fmt.Errorf("exec: ThreadItemDetails missing type tag: %w", err)
	}
	out := ThreadItemDetails{Kind: tag.Type}
	var err error
	switch tag.Type {
	case ThreadItemDetailKindAgentMessage:
		out.AgentMessage = new(AgentMessageItem)
		err = json.Unmarshal(data, out.AgentMessage)
	case ThreadItemDetailKindReasoning:
		out.Reasoning = new(ReasoningItem)
		err = json.Unmarshal(data, out.Reasoning)
	case ThreadItemDetailKindCommandExecution:
		out.CommandExecution = new(CommandExecutionItem)
		err = json.Unmarshal(data, out.CommandExecution)
	case ThreadItemDetailKindFileChange:
		out.FileChange = new(FileChangeItem)
		err = json.Unmarshal(data, out.FileChange)
	case ThreadItemDetailKindMcpToolCall:
		out.McpToolCall = new(McpToolCallItem)
		err = json.Unmarshal(data, out.McpToolCall)
	case ThreadItemDetailKindWebSearch:
		out.WebSearch = new(WebSearchItem)
		err = json.Unmarshal(data, out.WebSearch)
	case ThreadItemDetailKindTodoList:
		out.TodoList = new(TodoListItem)
		err = json.Unmarshal(data, out.TodoList)
	case ThreadItemDetailKindError:
		out.Error = new(ErrorItem)
		err = json.Unmarshal(data, out.Error)
	default:
		return fmt.Errorf("exec: unknown ThreadItemDetails type tag: %q", tag.Type)
	}
	if err != nil {
		return err
	}
	*d = out
	return nil
}

// AgentMessageItem is the agent's textual (or structured-JSON) response.
type AgentMessageItem struct {
	Text string `json:"text"`
}

// ReasoningItem is the agent's reasoning summary.
type ReasoningItem struct {
	Text string `json:"text"`
}

// CommandExecutionStatus is the lifecycle status of a command execution.
type CommandExecutionStatus string

const (
	// CommandExecutionStatusInProgress means the command is still running.
	CommandExecutionStatusInProgress CommandExecutionStatus = "in_progress"
	// CommandExecutionStatusCompleted means the command exited successfully.
	CommandExecutionStatusCompleted CommandExecutionStatus = "completed"
	// CommandExecutionStatusFailed means the command exited with a failure.
	CommandExecutionStatusFailed CommandExecutionStatus = "failed"
	// CommandExecutionStatusDeclined means the command was declined.
	CommandExecutionStatusDeclined CommandExecutionStatus = "declined"
)

// CommandExecutionItem tracks a command executed by the agent.
type CommandExecutionItem struct {
	Command          string                 `json:"command"`
	AggregatedOutput string                 `json:"aggregated_output"`
	ExitCode         *int32                 `json:"exit_code"`
	Status           CommandExecutionStatus `json:"status"`
}

// PatchChangeKind classifies one file change in a patch.
type PatchChangeKind string

const (
	// PatchChangeKindAdd marks an added file.
	PatchChangeKindAdd PatchChangeKind = "add"
	// PatchChangeKindDelete marks a deleted file.
	PatchChangeKindDelete PatchChangeKind = "delete"
	// PatchChangeKindUpdate marks an updated (or moved) file.
	PatchChangeKindUpdate PatchChangeKind = "update"
)

// FileUpdateChange is one path-level change within a [FileChangeItem].
type FileUpdateChange struct {
	Path string          `json:"path"`
	Kind PatchChangeKind `json:"kind"`
}

// PatchApplyStatus is the lifecycle status of a patch application.
type PatchApplyStatus string

const (
	// PatchApplyStatusInProgress means the patch is being applied.
	PatchApplyStatusInProgress PatchApplyStatus = "in_progress"
	// PatchApplyStatusCompleted means the patch applied successfully.
	PatchApplyStatusCompleted PatchApplyStatus = "completed"
	// PatchApplyStatusFailed means the patch failed (or was declined).
	PatchApplyStatusFailed PatchApplyStatus = "failed"
)

// FileChangeItem is a set of file changes by the agent.
type FileChangeItem struct {
	Changes []FileUpdateChange `json:"changes"`
	Status  PatchApplyStatus   `json:"status"`
}

// MarshalJSON guarantees changes serializes as an array (never null).
func (i FileChangeItem) MarshalJSON() ([]byte, error) {
	type alias FileChangeItem
	out := alias(i)
	if out.Changes == nil {
		out.Changes = []FileUpdateChange{}
	}
	return json.Marshal(out)
}

// McpToolCallStatus is the lifecycle status of an MCP tool call.
type McpToolCallStatus string

const (
	// McpToolCallStatusInProgress means the call is in flight.
	McpToolCallStatusInProgress McpToolCallStatus = "in_progress"
	// McpToolCallStatusCompleted means the call completed.
	McpToolCallStatusCompleted McpToolCallStatus = "completed"
	// McpToolCallStatusFailed means the call failed.
	McpToolCallStatusFailed McpToolCallStatus = "failed"
)

// McpToolCallItemResult is the success payload of an MCP tool call. The optional
// _meta field is omitted when absent, matching the Rust skip_serializing_if.
type McpToolCallItemResult struct {
	Content           []json.RawMessage `json:"content"`
	Meta              json.RawMessage   `json:"_meta,omitempty"`
	StructuredContent json.RawMessage   `json:"structured_content"`
}

// MarshalJSON guarantees content serializes as an array and structured_content
// serializes as null when absent (the Rust field has no skip_serializing_if).
func (r McpToolCallItemResult) MarshalJSON() ([]byte, error) {
	type alias McpToolCallItemResult
	out := alias(r)
	if out.Content == nil {
		out.Content = []json.RawMessage{}
	}
	if out.StructuredContent == nil {
		out.StructuredContent = json.RawMessage("null")
	}
	return json.Marshal(out)
}

// McpToolCallItemError is the failure payload of an MCP tool call.
type McpToolCallItemError struct {
	Message string `json:"message"`
}

// McpToolCallItem is a call to an MCP tool.
type McpToolCallItem struct {
	Server    string                 `json:"server"`
	Tool      string                 `json:"tool"`
	Arguments json.RawMessage        `json:"arguments"`
	Result    *McpToolCallItemResult `json:"result"`
	Error     *McpToolCallItemError  `json:"error"`
	Status    McpToolCallStatus      `json:"status"`
}

// MarshalJSON guarantees arguments serializes (null when absent).
func (i McpToolCallItem) MarshalJSON() ([]byte, error) {
	type alias McpToolCallItem
	out := alias(i)
	if out.Arguments == nil {
		out.Arguments = json.RawMessage("null")
	}
	return json.Marshal(out)
}

// WebSearchItem captures a web-search request.
type WebSearchItem struct {
	ID     string                   `json:"id"`
	Query  string                   `json:"query"`
	Action protocol.WebSearchAction `json:"action"`
}

// TodoItem is one entry in the agent's to-do list.
type TodoItem struct {
	Text      string `json:"text"`
	Completed bool   `json:"completed"`
}

// TodoListItem tracks the agent's running to-do list.
type TodoListItem struct {
	Items []TodoItem `json:"items"`
}

// MarshalJSON guarantees items serializes as an array (never null).
func (i TodoListItem) MarshalJSON() ([]byte, error) {
	type alias TodoListItem
	out := alias(i)
	if out.Items == nil {
		out.Items = []TodoItem{}
	}
	return json.Marshal(out)
}

// ErrorItem describes a non-fatal error surfaced as an item.
type ErrorItem struct {
	Message string `json:"message"`
}

// mergeTaggedObject marshals payload to a JSON object and injects the "type"
// tag, producing the externally/internally-tagged representation serde uses.
func mergeTaggedObject(typeTag string, payload any) ([]byte, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("exec: tagged payload is not a JSON object: %w", err)
	}
	if m == nil {
		m = map[string]json.RawMessage{}
	}
	m["type"] = json.RawMessage(`"` + typeTag + `"`)
	return json.Marshal(m)
}
