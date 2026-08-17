package appserverproto

import (
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// This file ports the v2::ThreadItem union from app-server-protocol v2/item.rs.
// ThreadItem is internally tagged on "type" with camelCase variant names; each
// variant's fields are camelCase. It is split out from item_thread.go to keep
// files focused and under the size budget.
//
// Cross-area payloads (McpToolCallResult / McpToolCallError owned by mcp.rs) are
// carried as json.RawMessage so this file compiles against the foundation alone.

// ThreadItemKind is the "type" tag value identifying a ThreadItem variant.
// Rust: v2::ThreadItem, serde tag = "type", rename_all = "camelCase".
type ThreadItemKind string

const (
	ThreadItemKindUserMessage         ThreadItemKind = "userMessage"
	ThreadItemKindHookPrompt          ThreadItemKind = "hookPrompt"
	ThreadItemKindAgentMessage        ThreadItemKind = "agentMessage"
	ThreadItemKindPlan                ThreadItemKind = "plan"
	ThreadItemKindReasoning           ThreadItemKind = "reasoning"
	ThreadItemKindCommandExecution    ThreadItemKind = "commandExecution"
	ThreadItemKindFileChange          ThreadItemKind = "fileChange"
	ThreadItemKindMcpToolCall         ThreadItemKind = "mcpToolCall"
	ThreadItemKindDynamicToolCall     ThreadItemKind = "dynamicToolCall"
	ThreadItemKindCollabAgentToolCall ThreadItemKind = "collabAgentToolCall"
	ThreadItemKindWebSearch           ThreadItemKind = "webSearch"
	ThreadItemKindImageView           ThreadItemKind = "imageView"
	ThreadItemKindImageGeneration     ThreadItemKind = "imageGeneration"
	ThreadItemKindEnteredReviewMode   ThreadItemKind = "enteredReviewMode"
	ThreadItemKindExitedReviewMode    ThreadItemKind = "exitedReviewMode"
	ThreadItemKindContextCompaction   ThreadItemKind = "contextCompaction"
)

// ThreadItem is the internally-tagged union of items that can appear in a turn.
// Exactly one variant struct is populated, selected by Kind. Custom JSON
// (Marshal/Unmarshal) flattens the active variant alongside the "type" tag,
// matching serde's internally-tagged representation.
type ThreadItem struct {
	Kind ThreadItemKind

	UserMessage         *ThreadItemUserMessage
	HookPrompt          *ThreadItemHookPrompt
	AgentMessage        *ThreadItemAgentMessage
	Plan                *ThreadItemPlan
	Reasoning           *ThreadItemReasoning
	CommandExecution    *ThreadItemCommandExecution
	FileChange          *ThreadItemFileChange
	McpToolCall         *ThreadItemMcpToolCall
	DynamicToolCall     *ThreadItemDynamicToolCall
	CollabAgentToolCall *ThreadItemCollabAgentToolCall
	WebSearch           *ThreadItemWebSearch
	ImageView           *ThreadItemImageView
	ImageGeneration     *ThreadItemImageGeneration
	EnteredReviewMode   *ThreadItemReviewMode
	ExitedReviewMode    *ThreadItemReviewMode
	ContextCompaction   *ThreadItemContextCompaction
}

// ID returns the item id common to every variant, mirroring ThreadItem::id().
func (t ThreadItem) ID() string {
	switch t.Kind {
	case ThreadItemKindUserMessage:
		if t.UserMessage != nil {
			return t.UserMessage.ID
		}
	case ThreadItemKindHookPrompt:
		if t.HookPrompt != nil {
			return t.HookPrompt.ID
		}
	case ThreadItemKindAgentMessage:
		if t.AgentMessage != nil {
			return t.AgentMessage.ID
		}
	case ThreadItemKindPlan:
		if t.Plan != nil {
			return t.Plan.ID
		}
	case ThreadItemKindReasoning:
		if t.Reasoning != nil {
			return t.Reasoning.ID
		}
	case ThreadItemKindCommandExecution:
		if t.CommandExecution != nil {
			return t.CommandExecution.ID
		}
	case ThreadItemKindFileChange:
		if t.FileChange != nil {
			return t.FileChange.ID
		}
	case ThreadItemKindMcpToolCall:
		if t.McpToolCall != nil {
			return t.McpToolCall.ID
		}
	case ThreadItemKindDynamicToolCall:
		if t.DynamicToolCall != nil {
			return t.DynamicToolCall.ID
		}
	case ThreadItemKindCollabAgentToolCall:
		if t.CollabAgentToolCall != nil {
			return t.CollabAgentToolCall.ID
		}
	case ThreadItemKindWebSearch:
		if t.WebSearch != nil {
			return t.WebSearch.ID
		}
	case ThreadItemKindImageView:
		if t.ImageView != nil {
			return t.ImageView.ID
		}
	case ThreadItemKindImageGeneration:
		if t.ImageGeneration != nil {
			return t.ImageGeneration.ID
		}
	case ThreadItemKindEnteredReviewMode:
		if t.EnteredReviewMode != nil {
			return t.EnteredReviewMode.ID
		}
	case ThreadItemKindExitedReviewMode:
		if t.ExitedReviewMode != nil {
			return t.ExitedReviewMode.ID
		}
	case ThreadItemKindContextCompaction:
		if t.ContextCompaction != nil {
			return t.ContextCompaction.ID
		}
	}
	return ""
}

// --- Variant payloads ---------------------------------------------------------

// ThreadItemUserMessage carries a user message item. clientId is always emitted
// (null when absent); content always serializes ([] when empty).
type ThreadItemUserMessage struct {
	ID       string      `json:"id"`
	ClientID *string     `json:"clientId"`
	Content  []UserInput `json:"content"`
}

// ThreadItemHookPrompt carries a hook prompt item. fragments always serializes.
type ThreadItemHookPrompt struct {
	ID        string               `json:"id"`
	Fragments []HookPromptFragment `json:"fragments"`
}

// ThreadItemAgentMessage carries an assistant message item. phase and
// memoryCitation are #[serde(default)] without skip, so both are always emitted
// (null when absent).
type ThreadItemAgentMessage struct {
	ID             string                 `json:"id"`
	Text           string                 `json:"text"`
	Phase          *protocol.MessagePhase `json:"phase"`
	MemoryCitation *MemoryCitation        `json:"memoryCitation"`
}

// ThreadItemPlan carries a proposed plan item.
type ThreadItemPlan struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// ThreadItemReasoning carries a reasoning item. summary and content default to
// [] when empty.
type ThreadItemReasoning struct {
	ID      string   `json:"id"`
	Summary []string `json:"summary"`
	Content []string `json:"content"`
}

// ThreadItemCommandExecution carries a command-execution item. The optional
// fields below have no skip_serializing_if, so they are always emitted (null).
type ThreadItemCommandExecution struct {
	ID               string                 `json:"id"`
	Command          string                 `json:"command"`
	Cwd              protocol.AbsolutePath  `json:"cwd"`
	ProcessID        *string                `json:"processId"`
	Source           CommandExecutionSource `json:"source"`
	Status           CommandExecutionStatus `json:"status"`
	CommandActions   []CommandAction        `json:"commandActions"`
	AggregatedOutput *string                `json:"aggregatedOutput"`
	ExitCode         *int32                 `json:"exitCode"`
	DurationMs       *int64                 `json:"durationMs"`
}

// ThreadItemFileChange carries a file-change item. changes always serializes.
type ThreadItemFileChange struct {
	ID      string             `json:"id"`
	Changes []FileUpdateChange `json:"changes"`
	Status  PatchApplyStatus   `json:"status"`
}

// ThreadItemMcpToolCall carries an MCP tool-call item. mcpAppResourceUri uses
// skip_serializing_if (omitted when absent); the other Options are always
// emitted (null). result/error are cross-area types carried as raw JSON.
type ThreadItemMcpToolCall struct {
	ID                string            `json:"id"`
	Server            string            `json:"server"`
	Tool              string            `json:"tool"`
	Status            McpToolCallStatus `json:"status"`
	Arguments         json.RawMessage   `json:"arguments"`
	McpAppResourceURI *string           `json:"mcpAppResourceUri,omitempty"`
	PluginID          *string           `json:"pluginId"`
	Result            json.RawMessage   `json:"result"`
	Error             json.RawMessage   `json:"error"`
	DurationMs        *int64            `json:"durationMs"`
}

// ThreadItemDynamicToolCall carries a dynamic tool-call item.
type ThreadItemDynamicToolCall struct {
	ID           string                              `json:"id"`
	Namespace    *string                             `json:"namespace"`
	Tool         string                              `json:"tool"`
	Arguments    json.RawMessage                     `json:"arguments"`
	Status       DynamicToolCallStatus               `json:"status"`
	ContentItems *[]DynamicToolCallOutputContentItem `json:"contentItems"`
	Success      *bool                               `json:"success"`
	DurationMs   *int64                              `json:"durationMs"`
}

// ThreadItemCollabAgentToolCall carries a collab tool-call item. agentsStates
// always serializes ({} when empty); receiverThreadIds always serializes ([]).
type ThreadItemCollabAgentToolCall struct {
	ID                string                      `json:"id"`
	Tool              CollabAgentTool             `json:"tool"`
	Status            CollabAgentToolCallStatus   `json:"status"`
	SenderThreadID    string                      `json:"senderThreadId"`
	ReceiverThreadIDs []string                    `json:"receiverThreadIds"`
	Prompt            *string                     `json:"prompt"`
	Model             *string                     `json:"model"`
	ReasoningEffort   *protocol.ReasoningEffort   `json:"reasoningEffort"`
	AgentsStates      map[string]CollabAgentState `json:"agentsStates"`
}

// ThreadItemWebSearch carries a web-search item. action is always emitted (null).
type ThreadItemWebSearch struct {
	ID     string           `json:"id"`
	Query  string           `json:"query"`
	Action *WebSearchAction `json:"action"`
}

// ThreadItemImageView carries an image-view item.
type ThreadItemImageView struct {
	ID   string                `json:"id"`
	Path protocol.AbsolutePath `json:"path"`
}

// ThreadItemImageGeneration carries an image-generation item. savedPath uses
// skip_serializing_if (omitted when absent); revisedPrompt is always emitted.
type ThreadItemImageGeneration struct {
	ID            string                 `json:"id"`
	Status        string                 `json:"status"`
	RevisedPrompt *string                `json:"revisedPrompt"`
	Result        string                 `json:"result"`
	SavedPath     *protocol.AbsolutePath `json:"savedPath,omitempty"`
}

// ThreadItemReviewMode carries an entered/exited review-mode item (shared shape).
type ThreadItemReviewMode struct {
	ID     string `json:"id"`
	Review string `json:"review"`
}

// ThreadItemContextCompaction carries a context-compaction marker item.
type ThreadItemContextCompaction struct {
	ID string `json:"id"`
}

// --- JSON (internally tagged) -------------------------------------------------

// payload returns the active variant struct as the value to flatten, plus a
// guard error when the active pointer is nil. Slice/map fields that must always
// serialize are normalized here to their empty literal.
func (t ThreadItem) payload() (any, error) {
	switch t.Kind {
	case ThreadItemKindUserMessage:
		v := *mustItem(t.UserMessage, t.Kind)
		v.Content = orEmptyUserInput(v.Content)
		return v, nil
	case ThreadItemKindHookPrompt:
		v := *mustItem(t.HookPrompt, t.Kind)
		v.Fragments = orEmptyFragments(v.Fragments)
		return v, nil
	case ThreadItemKindAgentMessage:
		return *mustItem(t.AgentMessage, t.Kind), nil
	case ThreadItemKindPlan:
		return *mustItem(t.Plan, t.Kind), nil
	case ThreadItemKindReasoning:
		v := *mustItem(t.Reasoning, t.Kind)
		v.Summary = orEmptyStrings(v.Summary)
		v.Content = orEmptyStrings(v.Content)
		return v, nil
	case ThreadItemKindCommandExecution:
		v := *mustItem(t.CommandExecution, t.Kind)
		v.CommandActions = orEmptyCommandActions(v.CommandActions)
		return v, nil
	case ThreadItemKindFileChange:
		v := *mustItem(t.FileChange, t.Kind)
		v.Changes = orEmptyFileChanges(v.Changes)
		return v, nil
	case ThreadItemKindMcpToolCall:
		v := *mustItem(t.McpToolCall, t.Kind)
		v.Arguments = rawOrNull(v.Arguments)
		v.Result = rawOrNull(v.Result)
		v.Error = rawOrNull(v.Error)
		return v, nil
	case ThreadItemKindDynamicToolCall:
		v := *mustItem(t.DynamicToolCall, t.Kind)
		v.Arguments = rawOrNull(v.Arguments)
		return v, nil
	case ThreadItemKindCollabAgentToolCall:
		v := *mustItem(t.CollabAgentToolCall, t.Kind)
		v.ReceiverThreadIDs = orEmptyStrings(v.ReceiverThreadIDs)
		if v.AgentsStates == nil {
			v.AgentsStates = map[string]CollabAgentState{}
		}
		return v, nil
	case ThreadItemKindWebSearch:
		return *mustItem(t.WebSearch, t.Kind), nil
	case ThreadItemKindImageView:
		return *mustItem(t.ImageView, t.Kind), nil
	case ThreadItemKindImageGeneration:
		return *mustItem(t.ImageGeneration, t.Kind), nil
	case ThreadItemKindEnteredReviewMode:
		return *mustItem(t.EnteredReviewMode, t.Kind), nil
	case ThreadItemKindExitedReviewMode:
		return *mustItem(t.ExitedReviewMode, t.Kind), nil
	case ThreadItemKindContextCompaction:
		return *mustItem(t.ContextCompaction, t.Kind), nil
	default:
		return nil, fmt.Errorf("appserverproto: unknown ThreadItem kind: %q", t.Kind)
	}
}

// MarshalJSON emits the internally-tagged item: the variant fields merged with a
// leading "type" tag.
func (t ThreadItem) MarshalJSON() ([]byte, error) {
	payload, err := t.payload()
	if err != nil {
		return nil, err
	}
	inner, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return mergeTag(string(t.Kind), inner)
}

// UnmarshalJSON decodes the internally-tagged item by its "type" tag.
func (t *ThreadItem) UnmarshalJSON(data []byte) error {
	var tag struct {
		Type ThreadItemKind `json:"type"`
	}
	if err := json.Unmarshal(data, &tag); err != nil {
		return fmt.Errorf("appserverproto: ThreadItem missing type tag: %w", err)
	}
	out := ThreadItem{Kind: tag.Type}
	switch tag.Type {
	case ThreadItemKindUserMessage:
		out.UserMessage = new(ThreadItemUserMessage)
		if err := json.Unmarshal(data, out.UserMessage); err != nil {
			return err
		}
	case ThreadItemKindHookPrompt:
		out.HookPrompt = new(ThreadItemHookPrompt)
		if err := json.Unmarshal(data, out.HookPrompt); err != nil {
			return err
		}
	case ThreadItemKindAgentMessage:
		out.AgentMessage = new(ThreadItemAgentMessage)
		if err := json.Unmarshal(data, out.AgentMessage); err != nil {
			return err
		}
	case ThreadItemKindPlan:
		out.Plan = new(ThreadItemPlan)
		if err := json.Unmarshal(data, out.Plan); err != nil {
			return err
		}
	case ThreadItemKindReasoning:
		out.Reasoning = new(ThreadItemReasoning)
		if err := json.Unmarshal(data, out.Reasoning); err != nil {
			return err
		}
	case ThreadItemKindCommandExecution:
		out.CommandExecution = new(ThreadItemCommandExecution)
		if err := json.Unmarshal(data, out.CommandExecution); err != nil {
			return err
		}
	case ThreadItemKindFileChange:
		out.FileChange = new(ThreadItemFileChange)
		if err := json.Unmarshal(data, out.FileChange); err != nil {
			return err
		}
	case ThreadItemKindMcpToolCall:
		out.McpToolCall = new(ThreadItemMcpToolCall)
		if err := json.Unmarshal(data, out.McpToolCall); err != nil {
			return err
		}
	case ThreadItemKindDynamicToolCall:
		out.DynamicToolCall = new(ThreadItemDynamicToolCall)
		if err := json.Unmarshal(data, out.DynamicToolCall); err != nil {
			return err
		}
	case ThreadItemKindCollabAgentToolCall:
		out.CollabAgentToolCall = new(ThreadItemCollabAgentToolCall)
		if err := json.Unmarshal(data, out.CollabAgentToolCall); err != nil {
			return err
		}
	case ThreadItemKindWebSearch:
		out.WebSearch = new(ThreadItemWebSearch)
		if err := json.Unmarshal(data, out.WebSearch); err != nil {
			return err
		}
	case ThreadItemKindImageView:
		out.ImageView = new(ThreadItemImageView)
		if err := json.Unmarshal(data, out.ImageView); err != nil {
			return err
		}
	case ThreadItemKindImageGeneration:
		out.ImageGeneration = new(ThreadItemImageGeneration)
		if err := json.Unmarshal(data, out.ImageGeneration); err != nil {
			return err
		}
	case ThreadItemKindEnteredReviewMode:
		out.EnteredReviewMode = new(ThreadItemReviewMode)
		if err := json.Unmarshal(data, out.EnteredReviewMode); err != nil {
			return err
		}
	case ThreadItemKindExitedReviewMode:
		out.ExitedReviewMode = new(ThreadItemReviewMode)
		if err := json.Unmarshal(data, out.ExitedReviewMode); err != nil {
			return err
		}
	case ThreadItemKindContextCompaction:
		out.ContextCompaction = new(ThreadItemContextCompaction)
		if err := json.Unmarshal(data, out.ContextCompaction); err != nil {
			return err
		}
	default:
		return fmt.Errorf("appserverproto: unknown ThreadItem type tag: %q", tag.Type)
	}
	*t = out
	return nil
}

// mustItem returns the variant pointer or panics with a descriptive message when
// nil; a nil active variant is a programmer error (the Kind and payload pointer
// must agree).
func mustItem[T any](p *T, kind ThreadItemKind) *T {
	if p == nil {
		panic(fmt.Sprintf("appserverproto: ThreadItem kind %q has nil payload", kind))
	}
	return p
}

// mergeTag injects {"type": <kind>} as the first key of an existing JSON object,
// producing the internally-tagged representation. inner must encode a JSON
// object ("{...}").
func mergeTag(kind string, inner []byte) ([]byte, error) {
	tag, err := json.Marshal(kind)
	if err != nil {
		return nil, err
	}
	prefix := append([]byte(`{"type":`), tag...)
	if len(inner) < 2 || inner[0] != '{' {
		return nil, fmt.Errorf("appserverproto: ThreadItem payload is not a JSON object: %s", string(inner))
	}
	if string(inner) == "{}" {
		return append(prefix, '}'), nil
	}
	// inner starts with '{'; replace it with our prefix plus a comma.
	out := append(prefix, ',')
	out = append(out, inner[1:]...)
	return out, nil
}

func orEmptyUserInput(v []UserInput) []UserInput {
	if v == nil {
		return []UserInput{}
	}
	return v
}

func orEmptyFragments(v []HookPromptFragment) []HookPromptFragment {
	if v == nil {
		return []HookPromptFragment{}
	}
	return v
}

func orEmptyStrings(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func orEmptyCommandActions(v []CommandAction) []CommandAction {
	if v == nil {
		return []CommandAction{}
	}
	return v
}

func orEmptyFileChanges(v []FileUpdateChange) []FileUpdateChange {
	if v == nil {
		return []FileUpdateChange{}
	}
	return v
}
