package appserverproto

import (
	"encoding/json"
	"fmt"
	"sort"
)

// This file ports codex-rs app-server-protocol v2/item.rs (the ThreadItem union
// and its supporting types) and the wire-shaping helpers from
// item_builders.rs (convert_patch_changes). The item/started and item/completed
// notifications are registered in turn_item.go alongside the turn methods.
//
// Cross-area types owned by sibling files (McpToolCallResult / McpToolCallError
// from mcp.rs, RequestPermissionProfile / ExecPolicyAmendment / network types
// from permissions.rs) are carried as json.RawMessage passthrough; only the
// ThreadItem variants and lifecycle notifications listed in this agent's scope
// are defined as typed structs.

// CommandExecutionStatus is the lifecycle status of a command execution item.
// Rust: v2::CommandExecutionStatus, camelCase.
type CommandExecutionStatus string

const (
	CommandExecutionStatusInProgress CommandExecutionStatus = "inProgress"
	CommandExecutionStatusCompleted  CommandExecutionStatus = "completed"
	CommandExecutionStatusFailed     CommandExecutionStatus = "failed"
	CommandExecutionStatusDeclined   CommandExecutionStatus = "declined"
)

// CommandExecutionSource is the origin of a command execution.
// Rust: v2::CommandExecutionSource, camelCase, derive(Default) = Agent.
type CommandExecutionSource string

const (
	CommandExecutionSourceAgent                 CommandExecutionSource = "agent"
	CommandExecutionSourceUserShell             CommandExecutionSource = "userShell"
	CommandExecutionSourceUnifiedExecStartup    CommandExecutionSource = "unifiedExecStartup"
	CommandExecutionSourceUnifiedExecInteractio CommandExecutionSource = "unifiedExecInteraction"
)

// DefaultCommandExecutionSource is the serde default (Agent).
const DefaultCommandExecutionSource = CommandExecutionSourceAgent

// PatchApplyStatus is the lifecycle status of a file-change item.
// Rust: v2::PatchApplyStatus, camelCase.
type PatchApplyStatus string

const (
	PatchApplyStatusInProgress PatchApplyStatus = "inProgress"
	PatchApplyStatusCompleted  PatchApplyStatus = "completed"
	PatchApplyStatusFailed     PatchApplyStatus = "failed"
	PatchApplyStatusDeclined   PatchApplyStatus = "declined"
)

// McpToolCallStatus is the lifecycle status of an MCP tool-call item.
// Rust: v2::McpToolCallStatus, camelCase.
type McpToolCallStatus string

const (
	McpToolCallStatusInProgress McpToolCallStatus = "inProgress"
	McpToolCallStatusCompleted  McpToolCallStatus = "completed"
	McpToolCallStatusFailed     McpToolCallStatus = "failed"
)

// DynamicToolCallStatus is the lifecycle status of a dynamic tool-call item.
// Rust: v2::DynamicToolCallStatus, camelCase.
type DynamicToolCallStatus string

const (
	DynamicToolCallStatusInProgress DynamicToolCallStatus = "inProgress"
	DynamicToolCallStatusCompleted  DynamicToolCallStatus = "completed"
	DynamicToolCallStatusFailed     DynamicToolCallStatus = "failed"
)

// CollabAgentToolCallStatus is the lifecycle status of a collab tool-call item.
// Rust: v2::CollabAgentToolCallStatus, camelCase.
type CollabAgentToolCallStatus string

const (
	CollabAgentToolCallStatusInProgress CollabAgentToolCallStatus = "inProgress"
	CollabAgentToolCallStatusCompleted  CollabAgentToolCallStatus = "completed"
	CollabAgentToolCallStatusFailed     CollabAgentToolCallStatus = "failed"
)

// CollabAgentTool names a collab-agent tool. Rust: v2::CollabAgentTool, camelCase.
type CollabAgentTool string

const (
	CollabAgentToolSpawnAgent  CollabAgentTool = "spawnAgent"
	CollabAgentToolSendInput   CollabAgentTool = "sendInput"
	CollabAgentToolResumeAgent CollabAgentTool = "resumeAgent"
	CollabAgentToolWait        CollabAgentTool = "wait"
	CollabAgentToolCloseAgent  CollabAgentTool = "closeAgent"
)

// CollabAgentStatus is the last-known status of a collab agent.
// Rust: v2::CollabAgentStatus, camelCase.
type CollabAgentStatus string

const (
	CollabAgentStatusPendingInit CollabAgentStatus = "pendingInit"
	CollabAgentStatusRunning     CollabAgentStatus = "running"
	CollabAgentStatusInterrupted CollabAgentStatus = "interrupted"
	CollabAgentStatusCompleted   CollabAgentStatus = "completed"
	CollabAgentStatusErrored     CollabAgentStatus = "errored"
	CollabAgentStatusShutdown    CollabAgentStatus = "shutdown"
	CollabAgentStatusNotFound    CollabAgentStatus = "notFound"
)

// CollabAgentState is a collab agent's status plus an optional message.
// Rust: v2::CollabAgentState, camelCase. message always emitted (null when None).
type CollabAgentState struct {
	Status  CollabAgentStatus `json:"status"`
	Message *string           `json:"message"`
}

// HookPromptFragment is one fragment of a hook prompt item.
// Rust: v2::HookPromptFragment, camelCase.
type HookPromptFragment struct {
	Text      string `json:"text"`
	HookRunID string `json:"hookRunId"`
}

// MemoryCitationEntry is one cited memory location. Rust: v2::MemoryCitationEntry,
// camelCase.
type MemoryCitationEntry struct {
	Path      string `json:"path"`
	LineStart uint32 `json:"lineStart"`
	LineEnd   uint32 `json:"lineEnd"`
	Note      string `json:"note"`
}

// MemoryCitation groups cited memory entries and the threads they came from.
// Rust: v2::MemoryCitation, camelCase. Both Vecs always serialize ([] when empty).
type MemoryCitation struct {
	Entries   []MemoryCitationEntry `json:"entries"`
	ThreadIDs []string              `json:"threadIds"`
}

// MarshalJSON ensures the Vec-backed fields emit [] (never null) when empty,
// matching Rust's always-serialized Vec.
func (m MemoryCitation) MarshalJSON() ([]byte, error) {
	type alias MemoryCitation
	out := alias(m)
	if out.Entries == nil {
		out.Entries = []MemoryCitationEntry{}
	}
	if out.ThreadIDs == nil {
		out.ThreadIDs = []string{}
	}
	return json.Marshal(out)
}

// PatchChangeKind is the internally-tagged ("type") kind of a file change.
// Rust: v2::PatchChangeKind, tag = "type", rename_all = "camelCase". The Update
// variant carries an optional movePath (always emitted; null when absent).
type PatchChangeKind struct {
	Kind string // "add" | "delete" | "update"
	// MovePath applies only to the "update" variant. It is always emitted on the
	// wire (no skip_serializing_if), so a nil value renders as movePath: null.
	MovePath *string
}

const (
	PatchChangeKindAdd    = "add"
	PatchChangeKindDelete = "delete"
	PatchChangeKindUpdate = "update"
)

// patchChangeUpdate is the wire shape of the update variant.
type patchChangeUpdate struct {
	Type     string  `json:"type"`
	MovePath *string `json:"movePath"`
}

// patchChangeUnit is the wire shape of the add/delete variants.
type patchChangeUnit struct {
	Type string `json:"type"`
}

// MarshalJSON encodes the internally-tagged change kind.
func (p PatchChangeKind) MarshalJSON() ([]byte, error) {
	switch p.Kind {
	case PatchChangeKindAdd, PatchChangeKindDelete:
		return json.Marshal(patchChangeUnit{Type: p.Kind})
	case PatchChangeKindUpdate:
		return json.Marshal(patchChangeUpdate{Type: p.Kind, MovePath: p.MovePath})
	default:
		return nil, fmt.Errorf("appserverproto: unknown PatchChangeKind: %q", p.Kind)
	}
}

// UnmarshalJSON decodes the internally-tagged change kind.
func (p *PatchChangeKind) UnmarshalJSON(data []byte) error {
	var tag patchChangeUnit
	if err := json.Unmarshal(data, &tag); err != nil {
		return err
	}
	switch tag.Type {
	case PatchChangeKindAdd, PatchChangeKindDelete:
		*p = PatchChangeKind{Kind: tag.Type}
	case PatchChangeKindUpdate:
		var v patchChangeUpdate
		if err := json.Unmarshal(data, &v); err != nil {
			return err
		}
		*p = PatchChangeKind{Kind: v.Type, MovePath: v.MovePath}
	default:
		return fmt.Errorf("appserverproto: unknown PatchChangeKind type: %q", tag.Type)
	}
	return nil
}

// FileUpdateChange is a single file change with a rendered diff.
// Rust: v2::FileUpdateChange, camelCase.
type FileUpdateChange struct {
	Path string          `json:"path"`
	Kind PatchChangeKind `json:"kind"`
	Diff string          `json:"diff"`
}

// CommandActionKind discriminates the internally-tagged ("type") CommandAction.
// Rust: v2::CommandAction, tag = "type", rename_all = "camelCase".
type CommandActionKind string

const (
	CommandActionKindRead      CommandActionKind = "read"
	CommandActionKindListFiles CommandActionKind = "listFiles"
	CommandActionKindSearch    CommandActionKind = "search"
	CommandActionKindUnknown   CommandActionKind = "unknown"
)

// CommandAction is a best-effort parse of one command segment.
// Rust: v2::CommandAction (internally tagged). Optional fields without
// skip_serializing_if are always emitted (null when absent).
type CommandAction struct {
	Kind CommandActionKind

	Command string  // all variants
	Name    string  // read
	Path    *string // read (required, abs path), listFiles/search (optional)
	Query   *string // search
}

type commandActionRead struct {
	Type    CommandActionKind `json:"type"`
	Command string            `json:"command"`
	Name    string            `json:"name"`
	Path    string            `json:"path"`
}

type commandActionListFiles struct {
	Type    CommandActionKind `json:"type"`
	Command string            `json:"command"`
	Path    *string           `json:"path"`
}

type commandActionSearch struct {
	Type    CommandActionKind `json:"type"`
	Command string            `json:"command"`
	Query   *string           `json:"query"`
	Path    *string           `json:"path"`
}

type commandActionUnknown struct {
	Type    CommandActionKind `json:"type"`
	Command string            `json:"command"`
}

// MarshalJSON encodes the internally-tagged command action.
func (c CommandAction) MarshalJSON() ([]byte, error) {
	switch c.Kind {
	case CommandActionKindRead:
		path := ""
		if c.Path != nil {
			path = *c.Path
		}
		return json.Marshal(commandActionRead{Type: c.Kind, Command: c.Command, Name: c.Name, Path: path})
	case CommandActionKindListFiles:
		return json.Marshal(commandActionListFiles{Type: c.Kind, Command: c.Command, Path: c.Path})
	case CommandActionKindSearch:
		return json.Marshal(commandActionSearch{Type: c.Kind, Command: c.Command, Query: c.Query, Path: c.Path})
	case CommandActionKindUnknown:
		return json.Marshal(commandActionUnknown{Type: c.Kind, Command: c.Command})
	default:
		return nil, fmt.Errorf("appserverproto: unknown CommandAction kind: %q", c.Kind)
	}
}

// UnmarshalJSON decodes the internally-tagged command action.
func (c *CommandAction) UnmarshalJSON(data []byte) error {
	var tag struct {
		Type CommandActionKind `json:"type"`
	}
	if err := json.Unmarshal(data, &tag); err != nil {
		return err
	}
	switch tag.Type {
	case CommandActionKindRead:
		var v commandActionRead
		if err := json.Unmarshal(data, &v); err != nil {
			return err
		}
		path := v.Path
		*c = CommandAction{Kind: v.Type, Command: v.Command, Name: v.Name, Path: &path}
	case CommandActionKindListFiles:
		var v commandActionListFiles
		if err := json.Unmarshal(data, &v); err != nil {
			return err
		}
		*c = CommandAction{Kind: v.Type, Command: v.Command, Path: v.Path}
	case CommandActionKindSearch:
		var v commandActionSearch
		if err := json.Unmarshal(data, &v); err != nil {
			return err
		}
		*c = CommandAction{Kind: v.Type, Command: v.Command, Query: v.Query, Path: v.Path}
	case CommandActionKindUnknown:
		var v commandActionUnknown
		if err := json.Unmarshal(data, &v); err != nil {
			return err
		}
		*c = CommandAction{Kind: v.Type, Command: v.Command}
	default:
		return fmt.Errorf("appserverproto: unknown CommandAction type: %q", tag.Type)
	}
	return nil
}

// WebSearchActionKind discriminates the internally-tagged WebSearchAction.
// Rust: v2::WebSearchAction, tag = "type", rename_all = "camelCase", with a
// catch-all #[serde(other)] Other variant.
type WebSearchActionKind string

const (
	WebSearchActionKindSearch     WebSearchActionKind = "search"
	WebSearchActionKindOpenPage   WebSearchActionKind = "openPage"
	WebSearchActionKindFindInPage WebSearchActionKind = "findInPage"
	WebSearchActionKindOther      WebSearchActionKind = "other"
)

// WebSearchAction is a web-search sub-action. Rust: v2::WebSearchAction.
// The Other variant is the serde catch-all; unrecognized tags decode to Other
// and Other serializes as {"type":"other"}.
type WebSearchAction struct {
	Kind WebSearchActionKind

	Query   *string  // search
	Queries []string // search
	URL     *string  // openPage, findInPage
	Pattern *string  // findInPage
}

type webSearchSearch struct {
	Type    WebSearchActionKind `json:"type"`
	Query   *string             `json:"query"`
	Queries []string            `json:"queries"`
}

type webSearchOpenPage struct {
	Type WebSearchActionKind `json:"type"`
	URL  *string             `json:"url"`
}

type webSearchFindInPage struct {
	Type    WebSearchActionKind `json:"type"`
	URL     *string             `json:"url"`
	Pattern *string             `json:"pattern"`
}

type webSearchOther struct {
	Type WebSearchActionKind `json:"type"`
}

// MarshalJSON encodes the internally-tagged web-search action.
func (w WebSearchAction) MarshalJSON() ([]byte, error) {
	switch w.Kind {
	case WebSearchActionKindSearch:
		return json.Marshal(webSearchSearch{Type: w.Kind, Query: w.Query, Queries: w.Queries})
	case WebSearchActionKindOpenPage:
		return json.Marshal(webSearchOpenPage{Type: w.Kind, URL: w.URL})
	case WebSearchActionKindFindInPage:
		return json.Marshal(webSearchFindInPage{Type: w.Kind, URL: w.URL, Pattern: w.Pattern})
	case WebSearchActionKindOther, "":
		return json.Marshal(webSearchOther{Type: WebSearchActionKindOther})
	default:
		return nil, fmt.Errorf("appserverproto: unknown WebSearchAction kind: %q", w.Kind)
	}
}

// UnmarshalJSON decodes the internally-tagged web-search action; any unrecognized
// tag maps to Other (serde #[serde(other)]).
func (w *WebSearchAction) UnmarshalJSON(data []byte) error {
	var tag struct {
		Type WebSearchActionKind `json:"type"`
	}
	if err := json.Unmarshal(data, &tag); err != nil {
		return err
	}
	switch tag.Type {
	case WebSearchActionKindSearch:
		var v webSearchSearch
		if err := json.Unmarshal(data, &v); err != nil {
			return err
		}
		*w = WebSearchAction{Kind: v.Type, Query: v.Query, Queries: v.Queries}
	case WebSearchActionKindOpenPage:
		var v webSearchOpenPage
		if err := json.Unmarshal(data, &v); err != nil {
			return err
		}
		*w = WebSearchAction{Kind: v.Type, URL: v.URL}
	case WebSearchActionKindFindInPage:
		var v webSearchFindInPage
		if err := json.Unmarshal(data, &v); err != nil {
			return err
		}
		*w = WebSearchAction{Kind: v.Type, URL: v.URL, Pattern: v.Pattern}
	default:
		*w = WebSearchAction{Kind: WebSearchActionKindOther}
	}
	return nil
}

// DynamicToolCallOutputContentItemKind discriminates the internally-tagged
// DynamicToolCallOutputContentItem. Rust: tag = "type", rename_all = "camelCase".
type DynamicToolCallOutputContentItemKind string

const (
	DynamicToolCallOutputContentItemKindInputText  DynamicToolCallOutputContentItemKind = "inputText"
	DynamicToolCallOutputContentItemKindInputImage DynamicToolCallOutputContentItemKind = "inputImage"
)

// DynamicToolCallOutputContentItem is one content item returned by a dynamic
// tool call. Rust: v2::DynamicToolCallOutputContentItem (internally tagged).
type DynamicToolCallOutputContentItem struct {
	Kind     DynamicToolCallOutputContentItemKind
	Text     string // inputText
	ImageURL string // inputImage
}

type dynToolInputText struct {
	Type DynamicToolCallOutputContentItemKind `json:"type"`
	Text string                               `json:"text"`
}

type dynToolInputImage struct {
	Type     DynamicToolCallOutputContentItemKind `json:"type"`
	ImageURL string                               `json:"imageUrl"`
}

// MarshalJSON encodes the internally-tagged content item.
func (d DynamicToolCallOutputContentItem) MarshalJSON() ([]byte, error) {
	switch d.Kind {
	case DynamicToolCallOutputContentItemKindInputText:
		return json.Marshal(dynToolInputText{Type: d.Kind, Text: d.Text})
	case DynamicToolCallOutputContentItemKindInputImage:
		return json.Marshal(dynToolInputImage{Type: d.Kind, ImageURL: d.ImageURL})
	default:
		return nil, fmt.Errorf("appserverproto: unknown DynamicToolCallOutputContentItem kind: %q", d.Kind)
	}
}

// UnmarshalJSON decodes the internally-tagged content item.
func (d *DynamicToolCallOutputContentItem) UnmarshalJSON(data []byte) error {
	var tag struct {
		Type DynamicToolCallOutputContentItemKind `json:"type"`
	}
	if err := json.Unmarshal(data, &tag); err != nil {
		return err
	}
	switch tag.Type {
	case DynamicToolCallOutputContentItemKindInputText:
		var v dynToolInputText
		if err := json.Unmarshal(data, &v); err != nil {
			return err
		}
		*d = DynamicToolCallOutputContentItem{Kind: v.Type, Text: v.Text}
	case DynamicToolCallOutputContentItemKindInputImage:
		var v dynToolInputImage
		if err := json.Unmarshal(data, &v); err != nil {
			return err
		}
		*d = DynamicToolCallOutputContentItem{Kind: v.Type, ImageURL: v.ImageURL}
	default:
		return fmt.Errorf("appserverproto: unknown DynamicToolCallOutputContentItem type: %q", tag.Type)
	}
	return nil
}

// convertPatchChanges renders a path->FileChange map as a sorted slice of
// FileUpdateChange, mirroring item_builders::convert_patch_changes (sorted by
// path). FileChange is the internal protocol type; the rendered diff matches
// the Rust format_file_change_diff. Defined here so the item area owns the
// presentation projection used by both live notifications and rebuilt history.
func convertPatchChanges(changes map[string]fileChange) []FileUpdateChange {
	out := make([]FileUpdateChange, 0, len(changes))
	for path, change := range changes {
		out = append(out, FileUpdateChange{
			Path: path,
			Kind: mapPatchChangeKind(change),
			Diff: formatFileChangeDiff(change),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// fileChange is a local view of the relevant subset of the internal protocol
// FileChange union used by convertPatchChanges. It is intentionally minimal so
// this file does not depend on a sibling-owned wire type.
type fileChange struct {
	Kind        string // "add" | "delete" | "update"
	Content     string // add, delete
	UnifiedDiff string // update
	MovePath    *string
}

func mapPatchChangeKind(c fileChange) PatchChangeKind {
	switch c.Kind {
	case PatchChangeKindAdd:
		return PatchChangeKind{Kind: PatchChangeKindAdd}
	case PatchChangeKindDelete:
		return PatchChangeKind{Kind: PatchChangeKindDelete}
	default:
		return PatchChangeKind{Kind: PatchChangeKindUpdate, MovePath: c.MovePath}
	}
}

func formatFileChangeDiff(c fileChange) string {
	switch c.Kind {
	case PatchChangeKindAdd, PatchChangeKindDelete:
		return c.Content
	default:
		if c.MovePath != nil {
			return fmt.Sprintf("%s\n\nMoved to: %s", c.UnifiedDiff, *c.MovePath)
		}
		return c.UnifiedDiff
	}
}

// ItemStartedNotification is emitted when a thread item's lifecycle begins.
// Rust: v2::ItemStartedNotification, camelCase. startedAtMs is a Unix ms
// timestamp.
type ItemStartedNotification struct {
	Item        ThreadItem `json:"item"`
	ThreadID    string     `json:"threadId"`
	TurnID      string     `json:"turnId"`
	StartedAtMs int64      `json:"startedAtMs"`
}

// ItemCompletedNotification is emitted when a thread item's lifecycle completes.
// Rust: v2::ItemCompletedNotification, camelCase. completedAtMs is a Unix ms
// timestamp.
type ItemCompletedNotification struct {
	Item          ThreadItem `json:"item"`
	ThreadID      string     `json:"threadId"`
	TurnID        string     `json:"turnId"`
	CompletedAtMs int64      `json:"completedAtMs"`
}
