package appserverproto

import (
	"encoding/json"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// This file ports app-server-protocol v2 thread.rs: the request/response and
// notification payloads for the thread/* method family, plus their registry
// entries.
//
// Type-ownership note: several aggregate types referenced by thread.rs are
// defined in sibling v2 modules (item.rs -> ThreadItem, turn.rs -> TurnStatus,
// thread_data.rs -> Thread/Turn/TurnItemsView/ThreadSource/GitInfo). To keep
// this file wire-faithful without redefining cross-area types, those nested
// aggregates are carried as json.RawMessage passthroughs (the established
// pattern from v1.go for Source / ForcedChatgptWorkspaceId). Types owned by
// thread.rs itself are defined fully here.

// =============================================================================
// Small enums owned by thread.rs
// =============================================================================

// ThreadStartSource classifies why a new thread was started. Rust:
// ThreadStartSource, serde rename_all = "camelCase".
type ThreadStartSource string

const (
	// ThreadStartSourceStartup marks a thread started at app startup.
	ThreadStartSourceStartup ThreadStartSource = "startup"
	// ThreadStartSourceClear marks a thread started by clearing a prior one.
	ThreadStartSourceClear ThreadStartSource = "clear"
)

// ThreadUnsubscribeStatus reports the outcome of thread/unsubscribe. Rust:
// serde rename_all = "camelCase".
type ThreadUnsubscribeStatus string

const (
	// ThreadUnsubscribeStatusNotLoaded means the thread was not loaded.
	ThreadUnsubscribeStatusNotLoaded ThreadUnsubscribeStatus = "notLoaded"
	// ThreadUnsubscribeStatusNotSubscribed means the caller was not subscribed.
	ThreadUnsubscribeStatusNotSubscribed ThreadUnsubscribeStatus = "notSubscribed"
	// ThreadUnsubscribeStatusUnsubscribed means the caller was unsubscribed.
	ThreadUnsubscribeStatusUnsubscribed ThreadUnsubscribeStatus = "unsubscribed"
)

// ThreadGoalStatus is the lifecycle status of a thread goal. Rust:
// v2_enum_from_core ThreadGoalStatus, serde rename_all = "camelCase".
type ThreadGoalStatus string

const (
	// ThreadGoalStatusActive marks an in-progress goal.
	ThreadGoalStatusActive ThreadGoalStatus = "active"
	// ThreadGoalStatusPaused marks a paused goal.
	ThreadGoalStatusPaused ThreadGoalStatus = "paused"
	// ThreadGoalStatusBlocked marks a blocked goal.
	ThreadGoalStatusBlocked ThreadGoalStatus = "blocked"
	// ThreadGoalStatusUsageLimited marks a usage-limited goal.
	ThreadGoalStatusUsageLimited ThreadGoalStatus = "usageLimited"
	// ThreadGoalStatusBudgetLimited marks a budget-limited goal.
	ThreadGoalStatusBudgetLimited ThreadGoalStatus = "budgetLimited"
	// ThreadGoalStatusComplete marks a completed goal.
	ThreadGoalStatusComplete ThreadGoalStatus = "complete"
)

// ThreadMemoryMode toggles persistent thread memory. Rust: serde rename_all =
// "lowercase".
type ThreadMemoryMode string

const (
	// ThreadMemoryModeEnabled enables persistent memory.
	ThreadMemoryModeEnabled ThreadMemoryMode = "enabled"
	// ThreadMemoryModeDisabled disables persistent memory.
	ThreadMemoryModeDisabled ThreadMemoryMode = "disabled"
)

// ThreadSourceKind filters thread listings by originating surface. Rust: serde
// rename_all = "camelCase" with VsCode renamed to "vscode".
type ThreadSourceKind string

const (
	// ThreadSourceKindCli is the CLI.
	ThreadSourceKindCli ThreadSourceKind = "cli"
	// ThreadSourceKindVsCode is the VS Code extension (wire value "vscode").
	ThreadSourceKindVsCode ThreadSourceKind = "vscode"
	// ThreadSourceKindExec is `codex exec`.
	ThreadSourceKindExec ThreadSourceKind = "exec"
	// ThreadSourceKindAppServer is the app-server.
	ThreadSourceKindAppServer ThreadSourceKind = "appServer"
	// ThreadSourceKindSubAgent is a spawned sub-agent.
	ThreadSourceKindSubAgent ThreadSourceKind = "subAgent"
	// ThreadSourceKindSubAgentReview is a review sub-agent.
	ThreadSourceKindSubAgentReview ThreadSourceKind = "subAgentReview"
	// ThreadSourceKindSubAgentCompact is a compaction sub-agent.
	ThreadSourceKindSubAgentCompact ThreadSourceKind = "subAgentCompact"
	// ThreadSourceKindSubAgentThreadSpawn is a thread-spawn sub-agent.
	ThreadSourceKindSubAgentThreadSpawn ThreadSourceKind = "subAgentThreadSpawn"
	// ThreadSourceKindSubAgentOther is any other sub-agent.
	ThreadSourceKindSubAgentOther ThreadSourceKind = "subAgentOther"
	// ThreadSourceKindUnknown is an unknown source.
	ThreadSourceKindUnknown ThreadSourceKind = "unknown"
)

// ThreadSortKey selects the sort column for thread listings. Rust: serde
// rename_all = "snake_case".
type ThreadSortKey string

const (
	// ThreadSortKeyCreatedAt sorts by creation time.
	ThreadSortKeyCreatedAt ThreadSortKey = "created_at"
	// ThreadSortKeyUpdatedAt sorts by last-updated time.
	ThreadSortKeyUpdatedAt ThreadSortKey = "updated_at"
)

// SortDirection selects ascending or descending order. Rust: serde rename_all =
// "snake_case".
type SortDirection string

const (
	// SortDirectionAsc sorts ascending.
	SortDirectionAsc SortDirection = "asc"
	// SortDirectionDesc sorts descending (newest first).
	SortDirectionDesc SortDirection = "desc"
)

// ThreadActiveFlag describes why a thread is in the Active state. Rust: serde
// rename_all = "camelCase".
type ThreadActiveFlag string

const (
	// ThreadActiveFlagWaitingOnApproval marks waiting on an approval.
	ThreadActiveFlagWaitingOnApproval ThreadActiveFlag = "waitingOnApproval"
	// ThreadActiveFlagWaitingOnUserInput marks waiting on user input.
	ThreadActiveFlagWaitingOnUserInput ThreadActiveFlag = "waitingOnUserInput"
)

// =============================================================================
// ThreadStatus: internally-tagged enum (tag = "type")
// =============================================================================

// ThreadStatusKind is the discriminator for ThreadStatus. Rust: serde tag =
// "type", rename_all = "camelCase".
type ThreadStatusKind string

const (
	// ThreadStatusKindNotLoaded means the thread is not loaded in memory.
	ThreadStatusKindNotLoaded ThreadStatusKind = "notLoaded"
	// ThreadStatusKindIdle means the thread is loaded and idle.
	ThreadStatusKindIdle ThreadStatusKind = "idle"
	// ThreadStatusKindSystemError means the thread hit a system error.
	ThreadStatusKindSystemError ThreadStatusKind = "systemError"
	// ThreadStatusKindActive means the thread is actively running a turn.
	ThreadStatusKindActive ThreadStatusKind = "active"
)

// ThreadStatus is the runtime status of a thread. Rust: an internally-tagged
// enum (tag = "type"); the Active variant additionally carries activeFlags.
type ThreadStatus struct {
	Kind ThreadStatusKind
	// ActiveFlags is populated only when Kind == ThreadStatusKindActive.
	ActiveFlags []ThreadActiveFlag
}

// MarshalJSON encodes the internally-tagged ThreadStatus.
func (s ThreadStatus) MarshalJSON() ([]byte, error) {
	switch s.Kind {
	case ThreadStatusKindActive:
		flags := s.ActiveFlags
		if flags == nil {
			flags = []ThreadActiveFlag{}
		}
		return json.Marshal(struct {
			Type        ThreadStatusKind   `json:"type"`
			ActiveFlags []ThreadActiveFlag `json:"activeFlags"`
		}{Type: ThreadStatusKindActive, ActiveFlags: flags})
	default:
		return json.Marshal(struct {
			Type ThreadStatusKind `json:"type"`
		}{Type: s.Kind})
	}
}

// UnmarshalJSON decodes the internally-tagged ThreadStatus.
func (s *ThreadStatus) UnmarshalJSON(data []byte) error {
	var probe struct {
		Type        ThreadStatusKind   `json:"type"`
		ActiveFlags []ThreadActiveFlag `json:"activeFlags"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	s.Kind = probe.Type
	if probe.Type == ThreadStatusKindActive {
		s.ActiveFlags = probe.ActiveFlags
	} else {
		s.ActiveFlags = nil
	}
	return nil
}

// =============================================================================
// ThreadListCwdFilter: untagged string | []string
// =============================================================================

// ThreadListCwdFilter filters thread listings by one or more cwd paths. Rust:
// serde untagged with variants One(String) and Many(Vec<String>).
type ThreadListCwdFilter struct {
	// One selects the single-string variant when non-nil.
	One *string
	// Many selects the array variant when non-nil.
	Many []string
}

// MarshalJSON emits the active untagged variant.
func (f ThreadListCwdFilter) MarshalJSON() ([]byte, error) {
	if f.One != nil {
		return json.Marshal(*f.One)
	}
	return json.Marshal(f.Many)
}

// UnmarshalJSON decodes whichever untagged variant is present, preferring the
// single-string form (serde tries variants in declaration order).
func (f *ThreadListCwdFilter) UnmarshalJSON(data []byte) error {
	var one string
	if err := json.Unmarshal(data, &one); err == nil {
		f.One = &one
		f.Many = nil
		return nil
	}
	var many []string
	if err := json.Unmarshal(data, &many); err != nil {
		return err
	}
	f.One = nil
	f.Many = many
	return nil
}

// =============================================================================
// DynamicToolSpec
// =============================================================================

// DynamicToolSpec describes a client-supplied dynamic tool. Rust: serde
// rename_all = "camelCase". On deserialize, deferLoading defaults from the
// legacy exposeToContext field (deferLoading = !exposeToContext) when absent.
type DynamicToolSpec struct {
	Namespace   *string         `json:"namespace"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	// DeferLoading is skip_serializing_if = Not::not (omitted when false).
	DeferLoading bool `json:"deferLoading,omitempty"`
}

// MarshalJSON omits the namespace key when nil (ts optional) and emits
// deferLoading only when true, matching the Rust Serialize derive.
func (d DynamicToolSpec) MarshalJSON() ([]byte, error) {
	m := map[string]any{
		"name":        d.Name,
		"description": d.Description,
		"inputSchema": d.InputSchema,
	}
	if d.Namespace != nil {
		m["namespace"] = *d.Namespace
	}
	if d.DeferLoading {
		m["deferLoading"] = true
	}
	return json.Marshal(m)
}

// UnmarshalJSON replicates the custom Deserialize that derives deferLoading from
// the legacy exposeToContext field when deferLoading is absent.
func (d *DynamicToolSpec) UnmarshalJSON(data []byte) error {
	var de struct {
		Namespace       *string         `json:"namespace"`
		Name            string          `json:"name"`
		Description     string          `json:"description"`
		InputSchema     json.RawMessage `json:"inputSchema"`
		DeferLoading    *bool           `json:"deferLoading"`
		ExposeToContext *bool           `json:"exposeToContext"`
	}
	if err := json.Unmarshal(data, &de); err != nil {
		return err
	}
	d.Namespace = de.Namespace
	d.Name = de.Name
	d.Description = de.Description
	d.InputSchema = de.InputSchema
	switch {
	case de.DeferLoading != nil:
		d.DeferLoading = *de.DeferLoading
	case de.ExposeToContext != nil:
		d.DeferLoading = !*de.ExposeToContext
	default:
		d.DeferLoading = false
	}
	return nil
}

// =============================================================================
// thread/start
// =============================================================================

// ThreadStartParams are the parameters for thread/start. Rust: serde rename_all
// = "camelCase". Fields with `ts(optional = nullable)` and no
// skip_serializing_if are emitted as null when absent; serviceTier uses the
// double_option three-state encoding (absent omitted, null clears, value sets);
// the two trailing bools are skip_serializing_if = Not::not.
type ThreadStartParams struct {
	Model                  *string                    `json:"model"`
	ModelProvider          *string                    `json:"modelProvider"`
	ServiceTier            *DoubleOption[string]      `json:"serviceTier,omitempty"`
	Cwd                    *string                    `json:"cwd"`
	RuntimeWorkspaceRoots  *[]string                  `json:"runtimeWorkspaceRoots"`
	ApprovalPolicy         *AskForApprovalV2          `json:"approvalPolicy"`
	ApprovalsReviewer      *ApprovalsReviewerV2       `json:"approvalsReviewer"`
	Sandbox                *SandboxModeV2             `json:"sandbox"`
	Permissions            *string                    `json:"permissions"`
	Config                 map[string]json.RawMessage `json:"config"`
	ServiceName            *string                    `json:"serviceName"`
	BaseInstructions       *string                    `json:"baseInstructions"`
	DeveloperInstructions  *string                    `json:"developerInstructions"`
	Personality            *protocol.Personality      `json:"personality"`
	Ephemeral              *bool                      `json:"ephemeral"`
	SessionStartSource     *ThreadStartSource         `json:"sessionStartSource"`
	ThreadSource           *json.RawMessage           `json:"threadSource"`
	Environments           *[]json.RawMessage         `json:"environments"`
	DynamicTools           *[]DynamicToolSpec         `json:"dynamicTools"`
	MockExperimentalField  *string                    `json:"mockExperimentalField"`
	ExperimentalRawEvents  bool                       `json:"experimentalRawEvents,omitempty"`
	PersistExtendedHistory bool                       `json:"persistExtendedHistory,omitempty"`
}

// MarshalJSON honors skip_serializing_if = Option::is_none on serviceTier (a
// nil pointer is omitted) while still emitting the other ts-nullable fields as
// null. The standard encoder handles every field except the value-typed
// DoubleOption inside the pointer, which is delegated to DoubleOption.
func (p ThreadStartParams) MarshalJSON() ([]byte, error) {
	type alias ThreadStartParams
	return marshalWithDoubleOption(alias(p), "serviceTier", p.ServiceTier)
}

// ThreadStartResponse is the result of thread/start. Rust: serde rename_all =
// "camelCase". The aggregate `thread` is carried as raw JSON (sibling-owned
// Thread type). runtimeWorkspaceRoots and instructionSources are
// `#[serde(default)]` Vecs (always serialized as arrays).
type ThreadStartResponse struct {
	Thread                  json.RawMessage                   `json:"thread"`
	Model                   string                            `json:"model"`
	ModelProvider           string                            `json:"modelProvider"`
	ServiceTier             *string                           `json:"serviceTier"`
	Cwd                     protocol.AbsolutePath             `json:"cwd"`
	RuntimeWorkspaceRoots   []protocol.AbsolutePath           `json:"runtimeWorkspaceRoots"`
	InstructionSources      []protocol.AbsolutePath           `json:"instructionSources"`
	ApprovalPolicy          AskForApprovalV2                  `json:"approvalPolicy"`
	ApprovalsReviewer       ApprovalsReviewerV2               `json:"approvalsReviewer"`
	Sandbox                 protocol.SandboxPolicy            `json:"sandbox"`
	ActivePermissionProfile *protocol.ActivePermissionProfile `json:"activePermissionProfile"`
	ReasoningEffort         *protocol.ReasoningEffort         `json:"reasoningEffort"`
}

// MarshalJSON guarantees the `#[serde(default)]` Vec fields emit as arrays.
func (r ThreadStartResponse) MarshalJSON() ([]byte, error) {
	type alias ThreadStartResponse
	out := alias(r)
	if out.RuntimeWorkspaceRoots == nil {
		out.RuntimeWorkspaceRoots = []protocol.AbsolutePath{}
	}
	if out.InstructionSources == nil {
		out.InstructionSources = []protocol.AbsolutePath{}
	}
	return json.Marshal(out)
}

// =============================================================================
// thread/settings/update + ThreadSettings
// =============================================================================

// ThreadSettingsUpdateParams overrides settings for subsequent turns. Rust:
// serde rename_all = "camelCase". serviceTier uses the double_option encoding.
type ThreadSettingsUpdateParams struct {
	ThreadID          string                      `json:"threadId"`
	Cwd               *string                     `json:"cwd"`
	ApprovalPolicy    *AskForApprovalV2           `json:"approvalPolicy"`
	ApprovalsReviewer *ApprovalsReviewerV2        `json:"approvalsReviewer"`
	SandboxPolicy     *protocol.SandboxPolicy     `json:"sandboxPolicy"`
	Permissions       *string                     `json:"permissions"`
	Model             *string                     `json:"model"`
	ServiceTier       *DoubleOption[string]       `json:"serviceTier,omitempty"`
	Effort            *protocol.ReasoningEffort   `json:"effort"`
	Summary           *protocol.ReasoningSummary  `json:"summary"`
	CollaborationMode *protocol.CollaborationMode `json:"collaborationMode"`
	Personality       *protocol.Personality       `json:"personality"`
}

// MarshalJSON honors skip_serializing_if = Option::is_none on serviceTier.
func (p ThreadSettingsUpdateParams) MarshalJSON() ([]byte, error) {
	type alias ThreadSettingsUpdateParams
	return marshalWithDoubleOption(alias(p), "serviceTier", p.ServiceTier)
}

// ThreadSettingsUpdateResponse is the empty result of thread/settings/update.
type ThreadSettingsUpdateResponse struct{}

// ThreadSettings is the effective settings snapshot for a thread. Rust: serde
// rename_all = "camelCase".
type ThreadSettings struct {
	Cwd                     protocol.AbsolutePath             `json:"cwd"`
	ApprovalPolicy          AskForApprovalV2                  `json:"approvalPolicy"`
	ApprovalsReviewer       ApprovalsReviewerV2               `json:"approvalsReviewer"`
	SandboxPolicy           protocol.SandboxPolicy            `json:"sandboxPolicy"`
	ActivePermissionProfile *protocol.ActivePermissionProfile `json:"activePermissionProfile"`
	Model                   string                            `json:"model"`
	ModelProvider           string                            `json:"modelProvider"`
	ServiceTier             *string                           `json:"serviceTier"`
	Effort                  *protocol.ReasoningEffort         `json:"effort"`
	Summary                 *protocol.ReasoningSummary        `json:"summary"`
	CollaborationMode       protocol.CollaborationMode        `json:"collaborationMode"`
	Personality             *protocol.Personality             `json:"personality"`
}

// ThreadSettingsUpdatedNotification announces a settings change. Rust:
// camelCase.
type ThreadSettingsUpdatedNotification struct {
	ThreadID       string         `json:"threadId"`
	ThreadSettings ThreadSettings `json:"threadSettings"`
}

// =============================================================================
// thread/resume
// =============================================================================

// ThreadResumeParams are the parameters for thread/resume. Rust: camelCase.
// `path` deserializes empty strings as absent; serviceTier uses double_option.
type ThreadResumeParams struct {
	ThreadID               string                              `json:"threadId"`
	History                *[]json.RawMessage                  `json:"history"`
	Path                   *string                             `json:"path"`
	Model                  *string                             `json:"model"`
	ModelProvider          *string                             `json:"modelProvider"`
	ServiceTier            *DoubleOption[string]               `json:"serviceTier,omitempty"`
	Cwd                    *string                             `json:"cwd"`
	RuntimeWorkspaceRoots  *[]string                           `json:"runtimeWorkspaceRoots"`
	ApprovalPolicy         *AskForApprovalV2                   `json:"approvalPolicy"`
	ApprovalsReviewer      *ApprovalsReviewerV2                `json:"approvalsReviewer"`
	Sandbox                *SandboxModeV2                      `json:"sandbox"`
	Permissions            *string                             `json:"permissions"`
	Config                 map[string]json.RawMessage          `json:"config"`
	BaseInstructions       *string                             `json:"baseInstructions"`
	DeveloperInstructions  *string                             `json:"developerInstructions"`
	Personality            *protocol.Personality               `json:"personality"`
	ExcludeTurns           bool                                `json:"excludeTurns,omitempty"`
	InitialTurnsPage       *ThreadResumeInitialTurnsPageParams `json:"initialTurnsPage"`
	PersistExtendedHistory bool                                `json:"persistExtendedHistory,omitempty"`
}

// MarshalJSON honors skip_serializing_if on serviceTier and treats an empty
// path string as absent (Rust deserialize_empty_path_as_none normalizes on
// input; the same normalization is applied here on output for fidelity).
func (p ThreadResumeParams) MarshalJSON() ([]byte, error) {
	p.Path = EmptyPathAsNone(p.Path)
	type alias ThreadResumeParams
	return marshalWithDoubleOption(alias(p), "serviceTier", p.ServiceTier)
}

// UnmarshalJSON normalizes an empty `path` to absent after decoding.
func (p *ThreadResumeParams) UnmarshalJSON(data []byte) error {
	type alias ThreadResumeParams
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*p = ThreadResumeParams(a)
	p.Path = EmptyPathAsNone(p.Path)
	return nil
}

// ThreadResumeInitialTurnsPageParams requests an inline turns page on resume.
// Rust: camelCase. itemsView carried as raw JSON (sibling-owned TurnItemsView).
type ThreadResumeInitialTurnsPageParams struct {
	Limit         *uint32          `json:"limit"`
	SortDirection *SortDirection   `json:"sortDirection"`
	ItemsView     *json.RawMessage `json:"itemsView"`
}

// ThreadResumeResponse is the result of thread/resume. Rust: camelCase. The
// thread aggregate and the optional initialTurnsPage are carried as raw JSON.
type ThreadResumeResponse struct {
	Thread                  json.RawMessage                   `json:"thread"`
	Model                   string                            `json:"model"`
	ModelProvider           string                            `json:"modelProvider"`
	ServiceTier             *string                           `json:"serviceTier"`
	Cwd                     protocol.AbsolutePath             `json:"cwd"`
	RuntimeWorkspaceRoots   []protocol.AbsolutePath           `json:"runtimeWorkspaceRoots"`
	InstructionSources      []protocol.AbsolutePath           `json:"instructionSources"`
	ApprovalPolicy          AskForApprovalV2                  `json:"approvalPolicy"`
	ApprovalsReviewer       ApprovalsReviewerV2               `json:"approvalsReviewer"`
	Sandbox                 protocol.SandboxPolicy            `json:"sandbox"`
	ActivePermissionProfile *protocol.ActivePermissionProfile `json:"activePermissionProfile"`
	ReasoningEffort         *protocol.ReasoningEffort         `json:"reasoningEffort"`
	InitialTurnsPage        json.RawMessage                   `json:"initialTurnsPage,omitempty"`
}

// MarshalJSON guarantees the `#[serde(default)]` Vec fields emit as arrays and
// emits a JSON null for initialTurnsPage when present-but-empty (the Rust field
// is `#[serde(default)] Option<TurnsPage>` with no skip, so it serializes as
// null when None).
func (r ThreadResumeResponse) MarshalJSON() ([]byte, error) {
	type alias ThreadResumeResponse
	out := alias(r)
	if out.RuntimeWorkspaceRoots == nil {
		out.RuntimeWorkspaceRoots = []protocol.AbsolutePath{}
	}
	if out.InstructionSources == nil {
		out.InstructionSources = []protocol.AbsolutePath{}
	}
	if out.InitialTurnsPage == nil {
		out.InitialTurnsPage = json.RawMessage("null")
	}
	return json.Marshal(out)
}

// TurnsPage is a paginated slice of turns. Rust: camelCase. `data` carries
// sibling-owned Turn values as raw JSON.
type TurnsPage struct {
	Data            []json.RawMessage `json:"data"`
	NextCursor      *string           `json:"nextCursor"`
	BackwardsCursor *string           `json:"backwardsCursor"`
}

// MarshalJSON guarantees `data` emits as an array (never null).
func (p TurnsPage) MarshalJSON() ([]byte, error) {
	type alias TurnsPage
	out := alias(p)
	if out.Data == nil {
		out.Data = []json.RawMessage{}
	}
	return json.Marshal(out)
}

// =============================================================================
// thread/fork
// =============================================================================

// ThreadForkParams are the parameters for thread/fork. Rust: camelCase. `path`
// empty-string is absent; serviceTier uses double_option; ephemeral and the
// trailing bools are skip_serializing_if = Not::not.
type ThreadForkParams struct {
	ThreadID               string                     `json:"threadId"`
	Path                   *string                    `json:"path"`
	Model                  *string                    `json:"model"`
	ModelProvider          *string                    `json:"modelProvider"`
	ServiceTier            *DoubleOption[string]      `json:"serviceTier,omitempty"`
	Cwd                    *string                    `json:"cwd"`
	RuntimeWorkspaceRoots  *[]string                  `json:"runtimeWorkspaceRoots"`
	ApprovalPolicy         *AskForApprovalV2          `json:"approvalPolicy"`
	ApprovalsReviewer      *ApprovalsReviewerV2       `json:"approvalsReviewer"`
	Sandbox                *SandboxModeV2             `json:"sandbox"`
	Permissions            *string                    `json:"permissions"`
	Config                 map[string]json.RawMessage `json:"config"`
	BaseInstructions       *string                    `json:"baseInstructions"`
	DeveloperInstructions  *string                    `json:"developerInstructions"`
	Ephemeral              bool                       `json:"ephemeral,omitempty"`
	ThreadSource           *json.RawMessage           `json:"threadSource"`
	ExcludeTurns           bool                       `json:"excludeTurns,omitempty"`
	PersistExtendedHistory bool                       `json:"persistExtendedHistory,omitempty"`
}

// MarshalJSON honors skip_serializing_if on serviceTier and normalizes an empty
// path to absent.
func (p ThreadForkParams) MarshalJSON() ([]byte, error) {
	p.Path = EmptyPathAsNone(p.Path)
	type alias ThreadForkParams
	return marshalWithDoubleOption(alias(p), "serviceTier", p.ServiceTier)
}

// UnmarshalJSON normalizes an empty `path` to absent after decoding.
func (p *ThreadForkParams) UnmarshalJSON(data []byte) error {
	type alias ThreadForkParams
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*p = ThreadForkParams(a)
	p.Path = EmptyPathAsNone(p.Path)
	return nil
}

// ThreadForkResponse is the result of thread/fork. Rust: camelCase.
type ThreadForkResponse struct {
	Thread                  json.RawMessage                   `json:"thread"`
	Model                   string                            `json:"model"`
	ModelProvider           string                            `json:"modelProvider"`
	ServiceTier             *string                           `json:"serviceTier"`
	Cwd                     protocol.AbsolutePath             `json:"cwd"`
	RuntimeWorkspaceRoots   []protocol.AbsolutePath           `json:"runtimeWorkspaceRoots"`
	InstructionSources      []protocol.AbsolutePath           `json:"instructionSources"`
	ApprovalPolicy          AskForApprovalV2                  `json:"approvalPolicy"`
	ApprovalsReviewer       ApprovalsReviewerV2               `json:"approvalsReviewer"`
	Sandbox                 protocol.SandboxPolicy            `json:"sandbox"`
	ActivePermissionProfile *protocol.ActivePermissionProfile `json:"activePermissionProfile"`
	ReasoningEffort         *protocol.ReasoningEffort         `json:"reasoningEffort"`
}

// MarshalJSON guarantees the `#[serde(default)]` Vec fields emit as arrays.
func (r ThreadForkResponse) MarshalJSON() ([]byte, error) {
	type alias ThreadForkResponse
	out := alias(r)
	if out.RuntimeWorkspaceRoots == nil {
		out.RuntimeWorkspaceRoots = []protocol.AbsolutePath{}
	}
	if out.InstructionSources == nil {
		out.InstructionSources = []protocol.AbsolutePath{}
	}
	return json.Marshal(out)
}

// =============================================================================
// thread/archive, thread/unarchive
// =============================================================================

// ThreadArchiveParams identifies the thread to archive. Rust: camelCase.
type ThreadArchiveParams struct {
	ThreadID string `json:"threadId"`
}

// ThreadArchiveResponse is the empty result of thread/archive.
type ThreadArchiveResponse struct{}

// ThreadUnarchiveParams identifies the thread to unarchive. Rust: camelCase.
type ThreadUnarchiveParams struct {
	ThreadID string `json:"threadId"`
}

// ThreadUnarchiveResponse returns the unarchived thread. Rust: camelCase.
type ThreadUnarchiveResponse struct {
	Thread json.RawMessage `json:"thread"`
}

// =============================================================================
// thread/unsubscribe
// =============================================================================

// ThreadUnsubscribeParams identifies the thread to unsubscribe from. Rust:
// camelCase.
type ThreadUnsubscribeParams struct {
	ThreadID string `json:"threadId"`
}

// ThreadUnsubscribeResponse reports the unsubscribe outcome. Rust: camelCase.
type ThreadUnsubscribeResponse struct {
	Status ThreadUnsubscribeStatus `json:"status"`
}

// =============================================================================
// thread/increment_elicitation, thread/decrement_elicitation
// =============================================================================

// ThreadIncrementElicitationParams identifies the thread whose elicitation
// counter is incremented. Rust: camelCase.
type ThreadIncrementElicitationParams struct {
	ThreadID string `json:"threadId"`
}

// ThreadIncrementElicitationResponse reports the post-increment counter. Rust:
// camelCase.
type ThreadIncrementElicitationResponse struct {
	Count  uint64 `json:"count"`
	Paused bool   `json:"paused"`
}

// ThreadDecrementElicitationParams identifies the thread whose elicitation
// counter is decremented. Rust: camelCase.
type ThreadDecrementElicitationParams struct {
	ThreadID string `json:"threadId"`
}

// ThreadDecrementElicitationResponse reports the post-decrement counter. Rust:
// camelCase.
type ThreadDecrementElicitationResponse struct {
	Count  uint64 `json:"count"`
	Paused bool   `json:"paused"`
}

// =============================================================================
// thread/name/set
// =============================================================================

// ThreadSetNameParams sets a thread's user-facing name. Rust: camelCase.
type ThreadSetNameParams struct {
	ThreadID string `json:"threadId"`
	Name     string `json:"name"`
}

// ThreadSetNameResponse is the empty result of thread/name/set.
type ThreadSetNameResponse struct{}

// =============================================================================
// thread/goal/*
// =============================================================================

// ThreadGoal is a thread's active goal. Rust: camelCase. Timestamp/budget
// numeric fields are i64.
type ThreadGoal struct {
	ThreadID        string           `json:"threadId"`
	Objective       string           `json:"objective"`
	Status          ThreadGoalStatus `json:"status"`
	TokenBudget     *int64           `json:"tokenBudget"`
	TokensUsed      int64            `json:"tokensUsed"`
	TimeUsedSeconds int64            `json:"timeUsedSeconds"`
	CreatedAt       int64            `json:"createdAt"`
	UpdatedAt       int64            `json:"updatedAt"`
}

// ThreadGoalSetParams sets or updates a thread goal. Rust: camelCase.
// tokenBudget uses the double_option encoding (absent omitted, null clears).
type ThreadGoalSetParams struct {
	ThreadID    string               `json:"threadId"`
	Objective   *string              `json:"objective"`
	Status      *ThreadGoalStatus    `json:"status"`
	TokenBudget *DoubleOption[int64] `json:"tokenBudget,omitempty"`
}

// MarshalJSON honors skip_serializing_if = Option::is_none on tokenBudget.
func (p ThreadGoalSetParams) MarshalJSON() ([]byte, error) {
	type alias ThreadGoalSetParams
	return marshalWithDoubleOption(alias(p), "tokenBudget", p.TokenBudget)
}

// ThreadGoalSetResponse returns the updated goal. Rust: camelCase.
type ThreadGoalSetResponse struct {
	Goal ThreadGoal `json:"goal"`
}

// ThreadGoalGetParams identifies the thread whose goal is read. Rust: camelCase.
type ThreadGoalGetParams struct {
	ThreadID string `json:"threadId"`
}

// ThreadGoalGetResponse returns the goal, if any. Rust: camelCase.
type ThreadGoalGetResponse struct {
	Goal *ThreadGoal `json:"goal"`
}

// ThreadGoalClearParams identifies the thread whose goal is cleared. Rust:
// camelCase.
type ThreadGoalClearParams struct {
	ThreadID string `json:"threadId"`
}

// ThreadGoalClearResponse reports whether a goal was cleared. Rust: camelCase.
type ThreadGoalClearResponse struct {
	Cleared bool `json:"cleared"`
}

// =============================================================================
// thread/metadata/update
// =============================================================================

// ThreadMetadataUpdateParams patches stored thread metadata. Rust: camelCase.
type ThreadMetadataUpdateParams struct {
	ThreadID string                             `json:"threadId"`
	GitInfo  *ThreadMetadataGitInfoUpdateParams `json:"gitInfo"`
}

// ThreadMetadataGitInfoUpdateParams patches stored Git metadata. Rust:
// camelCase. Each field uses the double_option encoding: absent leaves the
// value unchanged (omitted), null clears it, a string replaces it.
type ThreadMetadataGitInfoUpdateParams struct {
	Sha       *DoubleOption[string] `json:"sha,omitempty"`
	Branch    *DoubleOption[string] `json:"branch,omitempty"`
	OriginURL *DoubleOption[string] `json:"originUrl,omitempty"`
}

// MarshalJSON honors skip_serializing_if = Option::is_none on every field.
func (p ThreadMetadataGitInfoUpdateParams) MarshalJSON() ([]byte, error) {
	m := map[string]json.RawMessage{}
	if p.Sha != nil {
		b, err := json.Marshal(p.Sha)
		if err != nil {
			return nil, err
		}
		m["sha"] = b
	}
	if p.Branch != nil {
		b, err := json.Marshal(p.Branch)
		if err != nil {
			return nil, err
		}
		m["branch"] = b
	}
	if p.OriginURL != nil {
		b, err := json.Marshal(p.OriginURL)
		if err != nil {
			return nil, err
		}
		m["originUrl"] = b
	}
	return json.Marshal(m)
}

// ThreadMetadataUpdateResponse returns the updated thread. Rust: camelCase.
type ThreadMetadataUpdateResponse struct {
	Thread json.RawMessage `json:"thread"`
}

// =============================================================================
// thread/memoryMode/set, memory/reset
// =============================================================================

// ThreadMemoryModeSetParams sets the memory mode for a thread. Rust: camelCase.
type ThreadMemoryModeSetParams struct {
	ThreadID string           `json:"threadId"`
	Mode     ThreadMemoryMode `json:"mode"`
}

// ThreadMemoryModeSetResponse is the empty result of thread/memoryMode/set.
type ThreadMemoryModeSetResponse struct{}

// MemoryResetResponse is the empty result of memory/reset.
type MemoryResetResponse struct{}

// =============================================================================
// thread/compact/start, thread/shellCommand, thread/approveGuardianDeniedAction
// thread/backgroundTerminals/clean
// =============================================================================

// ThreadCompactStartParams identifies the thread to compact. Rust: camelCase.
type ThreadCompactStartParams struct {
	ThreadID string `json:"threadId"`
}

// ThreadCompactStartResponse is the empty result of thread/compact/start.
type ThreadCompactStartResponse struct{}

// ThreadShellCommandParams runs an unsandboxed shell command. Rust: camelCase.
type ThreadShellCommandParams struct {
	ThreadID string `json:"threadId"`
	Command  string `json:"command"`
}

// ThreadShellCommandResponse is the empty result of thread/shellCommand.
type ThreadShellCommandResponse struct{}

// ThreadApproveGuardianDeniedActionParams approves a guardian-denied action.
// Rust: camelCase. `event` is a serialized GuardianAssessmentEvent (raw JSON).
type ThreadApproveGuardianDeniedActionParams struct {
	ThreadID string          `json:"threadId"`
	Event    json.RawMessage `json:"event"`
}

// ThreadApproveGuardianDeniedActionResponse is the empty result.
type ThreadApproveGuardianDeniedActionResponse struct{}

// ThreadBackgroundTerminalsCleanParams identifies the thread whose background
// terminals are cleaned. Rust: camelCase.
type ThreadBackgroundTerminalsCleanParams struct {
	ThreadID string `json:"threadId"`
}

// ThreadBackgroundTerminalsCleanResponse is the empty result.
type ThreadBackgroundTerminalsCleanResponse struct{}

// =============================================================================
// thread/rollback
// =============================================================================

// ThreadRollbackParams drops the last N turns from a thread. Rust: camelCase.
type ThreadRollbackParams struct {
	ThreadID string `json:"threadId"`
	NumTurns uint32 `json:"numTurns"`
}

// ThreadRollbackResponse returns the rolled-back thread. Rust: camelCase.
type ThreadRollbackResponse struct {
	Thread json.RawMessage `json:"thread"`
}

// =============================================================================
// thread/list, thread/search, thread/loaded/list
// =============================================================================

// ThreadListParams filters and paginates thread listings. Rust: camelCase.
// useStateDbOnly is skip_serializing_if = Not::not.
type ThreadListParams struct {
	Cursor         *string              `json:"cursor"`
	Limit          *uint32              `json:"limit"`
	SortKey        *ThreadSortKey       `json:"sortKey"`
	SortDirection  *SortDirection       `json:"sortDirection"`
	ModelProviders *[]string            `json:"modelProviders"`
	SourceKinds    *[]ThreadSourceKind  `json:"sourceKinds"`
	Archived       *bool                `json:"archived"`
	Cwd            *ThreadListCwdFilter `json:"cwd"`
	UseStateDbOnly bool                 `json:"useStateDbOnly,omitempty"`
	SearchTerm     *string              `json:"searchTerm"`
}

// ThreadSearchParams searches thread listings by term. Rust: camelCase.
type ThreadSearchParams struct {
	Cursor        *string             `json:"cursor"`
	Limit         *uint32             `json:"limit"`
	SortKey       *ThreadSortKey      `json:"sortKey"`
	SortDirection *SortDirection      `json:"sortDirection"`
	SourceKinds   *[]ThreadSourceKind `json:"sourceKinds"`
	Archived      *bool               `json:"archived"`
	SearchTerm    string              `json:"searchTerm"`
}

// ThreadListResponse is a paginated list of threads. Rust: camelCase. `data`
// carries sibling-owned Thread values as raw JSON.
type ThreadListResponse struct {
	Data            []json.RawMessage `json:"data"`
	NextCursor      *string           `json:"nextCursor"`
	BackwardsCursor *string           `json:"backwardsCursor"`
}

// MarshalJSON guarantees `data` emits as an array (never null).
func (r ThreadListResponse) MarshalJSON() ([]byte, error) {
	type alias ThreadListResponse
	out := alias(r)
	if out.Data == nil {
		out.Data = []json.RawMessage{}
	}
	return json.Marshal(out)
}

// ThreadSearchResult is one search hit. Rust: camelCase. `thread` carries the
// sibling-owned Thread value as raw JSON.
type ThreadSearchResult struct {
	Thread  json.RawMessage `json:"thread"`
	Snippet string          `json:"snippet"`
}

// ThreadSearchResponse is a paginated list of search results. Rust: camelCase.
type ThreadSearchResponse struct {
	Data            []ThreadSearchResult `json:"data"`
	NextCursor      *string              `json:"nextCursor"`
	BackwardsCursor *string              `json:"backwardsCursor"`
}

// MarshalJSON guarantees `data` emits as an array (never null).
func (r ThreadSearchResponse) MarshalJSON() ([]byte, error) {
	type alias ThreadSearchResponse
	out := alias(r)
	if out.Data == nil {
		out.Data = []ThreadSearchResult{}
	}
	return json.Marshal(out)
}

// ThreadLoadedListParams paginates the loaded-thread listing. Rust: camelCase.
type ThreadLoadedListParams struct {
	Cursor *string `json:"cursor"`
	Limit  *uint32 `json:"limit"`
}

// ThreadLoadedListResponse lists loaded thread ids. Rust: camelCase.
type ThreadLoadedListResponse struct {
	Data       []string `json:"data"`
	NextCursor *string  `json:"nextCursor"`
}

// MarshalJSON guarantees `data` emits as an array (never null).
func (r ThreadLoadedListResponse) MarshalJSON() ([]byte, error) {
	type alias ThreadLoadedListResponse
	out := alias(r)
	if out.Data == nil {
		out.Data = []string{}
	}
	return json.Marshal(out)
}

// =============================================================================
// thread/read, thread/inject_items
// =============================================================================

// ThreadReadParams reads a thread, optionally with turns. Rust: camelCase.
// includeTurns is skip_serializing_if = Not::not.
type ThreadReadParams struct {
	ThreadID     string `json:"threadId"`
	IncludeTurns bool   `json:"includeTurns,omitempty"`
}

// ThreadReadResponse returns the thread. Rust: camelCase.
type ThreadReadResponse struct {
	Thread json.RawMessage `json:"thread"`
}

// ThreadInjectItemsParams appends raw Responses API items to a thread. Rust:
// camelCase. `items` carries opaque Responses API items as raw JSON.
type ThreadInjectItemsParams struct {
	ThreadID string            `json:"threadId"`
	Items    []json.RawMessage `json:"items"`
}

// MarshalJSON guarantees `items` emits as an array (never null).
func (p ThreadInjectItemsParams) MarshalJSON() ([]byte, error) {
	type alias ThreadInjectItemsParams
	out := alias(p)
	if out.Items == nil {
		out.Items = []json.RawMessage{}
	}
	return json.Marshal(out)
}

// ThreadInjectItemsResponse is the empty result of thread/inject_items.
type ThreadInjectItemsResponse struct{}

// =============================================================================
// thread/turns/list, thread/turns/items/list
// =============================================================================

// ThreadTurnsListParams paginates a thread's turns. Rust: camelCase. itemsView
// is carried as raw JSON (sibling-owned TurnItemsView).
type ThreadTurnsListParams struct {
	ThreadID      string           `json:"threadId"`
	Cursor        *string          `json:"cursor"`
	Limit         *uint32          `json:"limit"`
	SortDirection *SortDirection   `json:"sortDirection"`
	ItemsView     *json.RawMessage `json:"itemsView"`
}

// ThreadTurnsListResponse is a paginated list of turns. Rust: camelCase. `data`
// carries sibling-owned Turn values as raw JSON.
type ThreadTurnsListResponse struct {
	Data            []json.RawMessage `json:"data"`
	NextCursor      *string           `json:"nextCursor"`
	BackwardsCursor *string           `json:"backwardsCursor"`
}

// MarshalJSON guarantees `data` emits as an array (never null).
func (r ThreadTurnsListResponse) MarshalJSON() ([]byte, error) {
	type alias ThreadTurnsListResponse
	out := alias(r)
	if out.Data == nil {
		out.Data = []json.RawMessage{}
	}
	return json.Marshal(out)
}

// ThreadTurnsItemsListParams paginates a turn's items. Rust: camelCase.
type ThreadTurnsItemsListParams struct {
	ThreadID      string         `json:"threadId"`
	TurnID        string         `json:"turnId"`
	Cursor        *string        `json:"cursor"`
	Limit         *uint32        `json:"limit"`
	SortDirection *SortDirection `json:"sortDirection"`
}

// ThreadTurnsItemsListResponse is a paginated list of thread items. Rust:
// camelCase. `data` carries sibling-owned ThreadItem values as raw JSON.
type ThreadTurnsItemsListResponse struct {
	Data            []json.RawMessage `json:"data"`
	NextCursor      *string           `json:"nextCursor"`
	BackwardsCursor *string           `json:"backwardsCursor"`
}

// MarshalJSON guarantees `data` emits as an array (never null).
func (r ThreadTurnsItemsListResponse) MarshalJSON() ([]byte, error) {
	type alias ThreadTurnsItemsListResponse
	out := alias(r)
	if out.Data == nil {
		out.Data = []json.RawMessage{}
	}
	return json.Marshal(out)
}

// =============================================================================
// Token usage notification types
// =============================================================================

// TokenUsageBreakdown is a per-category token tally. Rust: camelCase, i64.
type TokenUsageBreakdown struct {
	TotalTokens           int64 `json:"totalTokens"`
	InputTokens           int64 `json:"inputTokens"`
	CachedInputTokens     int64 `json:"cachedInputTokens"`
	OutputTokens          int64 `json:"outputTokens"`
	ReasoningOutputTokens int64 `json:"reasoningOutputTokens"`
}

// ThreadTokenUsage is the cumulative and last-turn token usage. Rust: camelCase.
type ThreadTokenUsage struct {
	Total              TokenUsageBreakdown `json:"total"`
	Last               TokenUsageBreakdown `json:"last"`
	ModelContextWindow *int64              `json:"modelContextWindow"`
}

// ThreadTokenUsageUpdatedNotification announces updated token usage. Rust:
// camelCase.
type ThreadTokenUsageUpdatedNotification struct {
	ThreadID   string           `json:"threadId"`
	TurnID     string           `json:"turnId"`
	TokenUsage ThreadTokenUsage `json:"tokenUsage"`
}

// =============================================================================
// Thread/turn lifecycle notifications
// =============================================================================

// ThreadStartedNotification announces a newly started thread. Rust: camelCase.
type ThreadStartedNotification struct {
	Thread json.RawMessage `json:"thread"`
}

// ThreadStatusChangedNotification announces a thread status change. Rust:
// camelCase.
type ThreadStatusChangedNotification struct {
	ThreadID string       `json:"threadId"`
	Status   ThreadStatus `json:"status"`
}

// ThreadArchivedNotification announces a thread was archived. Rust: camelCase.
type ThreadArchivedNotification struct {
	ThreadID string `json:"threadId"`
}

// ThreadUnarchivedNotification announces a thread was unarchived. Rust:
// camelCase.
type ThreadUnarchivedNotification struct {
	ThreadID string `json:"threadId"`
}

// ThreadClosedNotification announces a thread was closed. Rust: camelCase.
type ThreadClosedNotification struct {
	ThreadID string `json:"threadId"`
}

// ThreadNameUpdatedNotification announces a thread name change. Rust: camelCase.
// threadName is skip_serializing_if = Option::is_none (omitted when absent).
type ThreadNameUpdatedNotification struct {
	ThreadID   string  `json:"threadId"`
	ThreadName *string `json:"threadName,omitempty"`
}

// ThreadGoalUpdatedNotification announces a thread goal change. Rust: camelCase.
type ThreadGoalUpdatedNotification struct {
	ThreadID string     `json:"threadId"`
	TurnID   *string    `json:"turnId"`
	Goal     ThreadGoal `json:"goal"`
}

// ThreadGoalClearedNotification announces a thread goal was cleared. Rust:
// camelCase.
type ThreadGoalClearedNotification struct {
	ThreadID string `json:"threadId"`
}

// ContextCompactedNotification is the deprecated context-compaction event. Rust:
// camelCase.
type ContextCompactedNotification struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
}

// =============================================================================
// Mock experimental method (test-only gating fixture)
// =============================================================================

// MockExperimentalMethodParams is a test-only experimental params fixture. Rust:
// camelCase.
type MockExperimentalMethodParams struct {
	Value *string `json:"value"`
}

// MockExperimentalMethodResponse echoes the input value. Rust: camelCase.
type MockExperimentalMethodResponse struct {
	Echoed *string `json:"echoed"`
}

// =============================================================================
// marshalWithDoubleOption: shared helper for skip-on-absent double options
// =============================================================================

// marshalWithDoubleOption serializes v (which must be a struct with the named
// double-option field declared `json:"<key>,omitempty"` as a pointer) and then
// re-injects the double-option value under <key> only when the pointer is
// non-nil. Go's encoding/json cannot omit a non-pointer struct nor render a
// pointer-to-DoubleOption through omitempty correctly (a non-nil pointer to a
// zero DoubleOption is never "empty"), so this helper does the field-level
// omission that Rust's skip_serializing_if = Option::is_none expresses.
//
// When the pointer is nil the key is omitted entirely; otherwise the inner
// DoubleOption marshals to either its value or JSON null (Present-but-not-Set).
func marshalWithDoubleOption[T any, V any](v T, key string, opt *DoubleOption[V]) ([]byte, error) {
	base, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(base, &fields); err != nil {
		return nil, err
	}
	delete(fields, key)
	if opt != nil {
		b, err := json.Marshal(opt)
		if err != nil {
			return nil, err
		}
		fields[key] = b
	}
	return json.Marshal(fields)
}

// =============================================================================
// Method + notification registration
// =============================================================================

func init() {
	// Client request methods (thread/*). Experimental flags mirror the
	// `#[experimental(...)]` attributes on the method definitions in common.rs.
	Register(MethodSpec{
		Method:    "thread/start",
		NewParams: func() any { return new(ThreadStartParams) },
		NewResult: func() any { return new(ThreadStartResponse) },
	})
	Register(MethodSpec{
		Method:    "thread/resume",
		NewParams: func() any { return new(ThreadResumeParams) },
		NewResult: func() any { return new(ThreadResumeResponse) },
	})
	Register(MethodSpec{
		Method:    "thread/fork",
		NewParams: func() any { return new(ThreadForkParams) },
		NewResult: func() any { return new(ThreadForkResponse) },
	})
	Register(MethodSpec{
		Method:    "thread/archive",
		NewParams: func() any { return new(ThreadArchiveParams) },
		NewResult: func() any { return new(ThreadArchiveResponse) },
	})
	Register(MethodSpec{
		Method:    "thread/unarchive",
		NewParams: func() any { return new(ThreadUnarchiveParams) },
		NewResult: func() any { return new(ThreadUnarchiveResponse) },
	})
	Register(MethodSpec{
		Method:    "thread/unsubscribe",
		NewParams: func() any { return new(ThreadUnsubscribeParams) },
		NewResult: func() any { return new(ThreadUnsubscribeResponse) },
	})
	Register(MethodSpec{
		Method:       "thread/increment_elicitation",
		NewParams:    func() any { return new(ThreadIncrementElicitationParams) },
		NewResult:    func() any { return new(ThreadIncrementElicitationResponse) },
		Experimental: true,
	})
	Register(MethodSpec{
		Method:       "thread/decrement_elicitation",
		NewParams:    func() any { return new(ThreadDecrementElicitationParams) },
		NewResult:    func() any { return new(ThreadDecrementElicitationResponse) },
		Experimental: true,
	})
	Register(MethodSpec{
		Method:    "thread/name/set",
		NewParams: func() any { return new(ThreadSetNameParams) },
		NewResult: func() any { return new(ThreadSetNameResponse) },
	})
	Register(MethodSpec{
		Method:    "thread/goal/set",
		NewParams: func() any { return new(ThreadGoalSetParams) },
		NewResult: func() any { return new(ThreadGoalSetResponse) },
	})
	Register(MethodSpec{
		Method:    "thread/goal/get",
		NewParams: func() any { return new(ThreadGoalGetParams) },
		NewResult: func() any { return new(ThreadGoalGetResponse) },
	})
	Register(MethodSpec{
		Method:    "thread/goal/clear",
		NewParams: func() any { return new(ThreadGoalClearParams) },
		NewResult: func() any { return new(ThreadGoalClearResponse) },
	})
	Register(MethodSpec{
		Method:    "thread/metadata/update",
		NewParams: func() any { return new(ThreadMetadataUpdateParams) },
		NewResult: func() any { return new(ThreadMetadataUpdateResponse) },
	})
	Register(MethodSpec{
		Method:       "thread/settings/update",
		NewParams:    func() any { return new(ThreadSettingsUpdateParams) },
		NewResult:    func() any { return new(ThreadSettingsUpdateResponse) },
		Experimental: true,
	})
	Register(MethodSpec{
		Method:       "thread/memoryMode/set",
		NewParams:    func() any { return new(ThreadMemoryModeSetParams) },
		NewResult:    func() any { return new(ThreadMemoryModeSetResponse) },
		Experimental: true,
	})
	Register(MethodSpec{
		Method:       "memory/reset",
		NewParams:    func() any { var v any; return &v },
		NewResult:    func() any { return new(MemoryResetResponse) },
		Experimental: true,
	})
	Register(MethodSpec{
		Method:    "thread/compact/start",
		NewParams: func() any { return new(ThreadCompactStartParams) },
		NewResult: func() any { return new(ThreadCompactStartResponse) },
	})
	Register(MethodSpec{
		Method:    "thread/shellCommand",
		NewParams: func() any { return new(ThreadShellCommandParams) },
		NewResult: func() any { return new(ThreadShellCommandResponse) },
	})
	Register(MethodSpec{
		Method:    "thread/approveGuardianDeniedAction",
		NewParams: func() any { return new(ThreadApproveGuardianDeniedActionParams) },
		NewResult: func() any { return new(ThreadApproveGuardianDeniedActionResponse) },
	})
	Register(MethodSpec{
		Method:       "thread/backgroundTerminals/clean",
		NewParams:    func() any { return new(ThreadBackgroundTerminalsCleanParams) },
		NewResult:    func() any { return new(ThreadBackgroundTerminalsCleanResponse) },
		Experimental: true,
	})
	Register(MethodSpec{
		Method:    "thread/rollback",
		NewParams: func() any { return new(ThreadRollbackParams) },
		NewResult: func() any { return new(ThreadRollbackResponse) },
	})
	Register(MethodSpec{
		Method:    "thread/list",
		NewParams: func() any { return new(ThreadListParams) },
		NewResult: func() any { return new(ThreadListResponse) },
	})
	Register(MethodSpec{
		Method:       "thread/search",
		NewParams:    func() any { return new(ThreadSearchParams) },
		NewResult:    func() any { return new(ThreadSearchResponse) },
		Experimental: true,
	})
	Register(MethodSpec{
		Method:    "thread/loaded/list",
		NewParams: func() any { return new(ThreadLoadedListParams) },
		NewResult: func() any { return new(ThreadLoadedListResponse) },
	})
	Register(MethodSpec{
		Method:    "thread/read",
		NewParams: func() any { return new(ThreadReadParams) },
		NewResult: func() any { return new(ThreadReadResponse) },
	})
	Register(MethodSpec{
		Method:       "thread/turns/list",
		NewParams:    func() any { return new(ThreadTurnsListParams) },
		NewResult:    func() any { return new(ThreadTurnsListResponse) },
		Experimental: true,
	})
	Register(MethodSpec{
		Method:       "thread/turns/items/list",
		NewParams:    func() any { return new(ThreadTurnsItemsListParams) },
		NewResult:    func() any { return new(ThreadTurnsItemsListResponse) },
		Experimental: true,
	})
	Register(MethodSpec{
		Method:    "thread/inject_items",
		NewParams: func() any { return new(ThreadInjectItemsParams) },
		NewResult: func() any { return new(ThreadInjectItemsResponse) },
	})

	// Server notifications (thread/*).
	RegisterNotification(NotificationSpec{
		Method:    "thread/started",
		NewParams: func() any { return new(ThreadStartedNotification) },
	})
	RegisterNotification(NotificationSpec{
		Method:    "thread/status/changed",
		NewParams: func() any { return new(ThreadStatusChangedNotification) },
	})
	RegisterNotification(NotificationSpec{
		Method:    "thread/archived",
		NewParams: func() any { return new(ThreadArchivedNotification) },
	})
	RegisterNotification(NotificationSpec{
		Method:    "thread/unarchived",
		NewParams: func() any { return new(ThreadUnarchivedNotification) },
	})
	RegisterNotification(NotificationSpec{
		Method:    "thread/closed",
		NewParams: func() any { return new(ThreadClosedNotification) },
	})
	RegisterNotification(NotificationSpec{
		Method:    "thread/name/updated",
		NewParams: func() any { return new(ThreadNameUpdatedNotification) },
	})
	RegisterNotification(NotificationSpec{
		Method:    "thread/goal/updated",
		NewParams: func() any { return new(ThreadGoalUpdatedNotification) },
	})
	RegisterNotification(NotificationSpec{
		Method:    "thread/goal/cleared",
		NewParams: func() any { return new(ThreadGoalClearedNotification) },
	})
	RegisterNotification(NotificationSpec{
		Method:       "thread/settings/updated",
		NewParams:    func() any { return new(ThreadSettingsUpdatedNotification) },
		Experimental: true,
	})
	RegisterNotification(NotificationSpec{
		Method:    "thread/tokenUsage/updated",
		NewParams: func() any { return new(ThreadTokenUsageUpdatedNotification) },
	})
	RegisterNotification(NotificationSpec{
		Method:    "thread/compacted",
		NewParams: func() any { return new(ContextCompactedNotification) },
	})
}
