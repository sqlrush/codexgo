package threadstore

import (
	"encoding/json"
	"time"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/rollout"
)

// ClearableField models the Rust `ClearableField<T>` = `Option<Option<T>>`, an
// optional field whose value can itself be cleared.
//
// There are three states, matching the Rust serde `optional_option` helper:
//   - Absent (Present == false): the outer Option is None. A no-op patch field
//     that is omitted on serialize and decoded from a missing JSON key.
//   - Cleared (Present == true, Value == nil): Some(None). A clear request,
//     serialized as JSON null.
//   - Set (Present == true, Value != nil): Some(Some(v)). Serialized as the value.
type ClearableField[T any] struct {
	// Present reports whether the outer Option is Some (the field is part of the
	// patch). When false the field is omitted entirely.
	Present bool
	// Value carries the inner value. nil with Present true means "clear".
	Value *T
}

// SetClearable returns a ClearableField that sets the field to v.
func SetClearable[T any](v T) ClearableField[T] {
	return ClearableField[T]{Present: true, Value: &v}
}

// ClearField returns a ClearableField that clears the field (Some(None)).
func ClearField[T any]() ClearableField[T] {
	return ClearableField[T]{Present: true, Value: nil}
}

// IsSome reports whether the outer Option is Some (the field participates in the
// patch), mirroring the Rust `Option::is_some` checks in `merge`/`is_empty`.
func (c ClearableField[T]) IsSome() bool { return c.Present }

// Flatten returns the inner value pointer (nil when the field is absent or a
// clear request), mirroring Rust `Option::flatten` followed by
// `as_ref().cloned()` on a `ClearableField`.
func Flatten[T any](c ClearableField[T]) *T {
	if !c.Present {
		return nil
	}
	return c.Value
}

// flatten is the package-internal spelling of [Flatten].
func flatten[T any](c ClearableField[T]) *T { return Flatten(c) }

// GitInfoPatch is a patch for thread git metadata, mirroring the Rust
// `GitInfoPatch`. Each field uses [ClearableField] presence semantics.
type GitInfoPatch struct {
	// SHA is the replacement commit SHA, a clear request, or a no-op.
	SHA ClearableField[string]
	// Branch is the replacement branch name, a clear request, or a no-op.
	Branch ClearableField[string]
	// OriginURL is the replacement origin URL, a clear request, or a no-op.
	OriginURL ClearableField[string]
}

// Merge merges other into p using field-presence semantics. Present fields in
// other replace the current value, including clear requests.
func (p *GitInfoPatch) Merge(other GitInfoPatch) {
	if other.SHA.IsSome() {
		p.SHA = other.SHA
	}
	if other.Branch.IsSome() {
		p.Branch = other.Branch
	}
	if other.OriginURL.IsSome() {
		p.OriginURL = other.OriginURL
	}
}

// MarshalJSON serializes the patch with the `optional_option` semantics: absent
// fields are omitted, cleared fields serialize as null, set fields as the value.
func (p GitInfoPatch) MarshalJSON() ([]byte, error) {
	m := map[string]any{}
	putClearable(m, "sha", p.SHA)
	putClearable(m, "branch", p.Branch)
	putClearable(m, "origin_url", p.OriginURL)
	return json.Marshal(m)
}

// UnmarshalJSON decodes the patch, distinguishing missing keys (no-op) from
// explicit null (clear).
func (p *GitInfoPatch) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var err error
	if p.SHA, err = decodeClearable[string](raw, "sha"); err != nil {
		return err
	}
	if p.Branch, err = decodeClearable[string](raw, "branch"); err != nil {
		return err
	}
	if p.OriginURL, err = decodeClearable[string](raw, "origin_url"); err != nil {
		return err
	}
	return nil
}

// ThreadMetadataPatch is a patch for thread metadata, mirroring the Rust
// `ThreadMetadataPatch`. Every field is literal: an absent field leaves the value
// unchanged; a present field applies the supplied value. Fields modeled with
// [ClearableField] support clearing via [ClearField].
type ThreadMetadataPatch struct {
	// Name is the replacement user-facing thread name (clearable).
	Name ClearableField[string]
	// RolloutPath is the known local rollout path for stores that expose one.
	RolloutPath *string
	// Preview is the best available preview text for discovery/listing.
	Preview *string
	// Title is the best-effort title derived from history.
	Title *string
	// ModelProvider is the model provider associated with the thread.
	ModelProvider *string
	// Model is the latest observed model.
	Model *string
	// ReasoningEffort is the latest observed reasoning effort (clearable since
	// 0.147: a turn may drop the effort override).
	ReasoningEffort ClearableField[protocol.ReasoningEffort]
	// CreatedAt is the creation timestamp when known.
	CreatedAt *time.Time
	// UpdatedAt is the last update timestamp for this metadata observation.
	UpdatedAt *time.Time
	// AdvanceRecencyAt, when set, moves the thread's recency watermark forward
	// (0.147 `advance_recency_at`); stores never move recency backwards.
	AdvanceRecencyAt *time.Time
	// Source is the session source.
	Source *rollout.SessionSource
	// ThreadSource is the optional analytics source classification (clearable).
	ThreadSource ClearableField[rollout.ThreadSource]
	// AgentNickname is the optional agent nickname (clearable).
	AgentNickname ClearableField[string]
	// AgentRole is the optional agent role (clearable).
	AgentRole ClearableField[string]
	// AgentPath is the optional canonical agent path (clearable).
	AgentPath ClearableField[string]
	// Cwd is the working directory.
	Cwd *string
	// CliVersion is the CLI version that created the thread.
	CliVersion *string
	// ApprovalMode is the approval mode.
	ApprovalMode *protocol.AskForApproval
	// PermissionProfile is the canonical runtime permissions.
	PermissionProfile *protocol.PermissionProfile
	// TokenUsage is the last observed token usage.
	TokenUsage *protocol.TokenUsage
	// FirstUserMessage is the first user message observed for this thread.
	FirstUserMessage *string
	// GitInfo is the git metadata patch.
	GitInfo *GitInfoPatch
	// MemoryMode is the thread memory behavior.
	MemoryMode *protocol.ThreadMemoryMode
}

// Merge merges other into p using field-presence semantics, mirroring the Rust
// `ThreadMetadataPatch::merge`. Present fields replace the current value,
// including clear requests; nested patches use the same semantics.
func (p *ThreadMetadataPatch) Merge(other ThreadMetadataPatch) {
	if other.Name.IsSome() {
		p.Name = other.Name
	}
	if other.RolloutPath != nil {
		p.RolloutPath = other.RolloutPath
	}
	if other.Preview != nil {
		p.Preview = other.Preview
	}
	if other.Title != nil {
		p.Title = other.Title
	}
	if other.ModelProvider != nil {
		p.ModelProvider = other.ModelProvider
	}
	if other.Model != nil {
		p.Model = other.Model
	}
	if other.ReasoningEffort.IsSome() {
		p.ReasoningEffort = other.ReasoningEffort
	}
	if other.CreatedAt != nil {
		p.CreatedAt = other.CreatedAt
	}
	if other.UpdatedAt != nil {
		p.UpdatedAt = other.UpdatedAt
	}
	if other.AdvanceRecencyAt != nil {
		p.AdvanceRecencyAt = other.AdvanceRecencyAt
	}
	if other.Source != nil {
		p.Source = other.Source
	}
	if other.ThreadSource.IsSome() {
		p.ThreadSource = other.ThreadSource
	}
	if other.AgentNickname.IsSome() {
		p.AgentNickname = other.AgentNickname
	}
	if other.AgentRole.IsSome() {
		p.AgentRole = other.AgentRole
	}
	if other.AgentPath.IsSome() {
		p.AgentPath = other.AgentPath
	}
	if other.Cwd != nil {
		p.Cwd = other.Cwd
	}
	if other.CliVersion != nil {
		p.CliVersion = other.CliVersion
	}
	if other.ApprovalMode != nil {
		p.ApprovalMode = other.ApprovalMode
	}
	if other.PermissionProfile != nil {
		p.PermissionProfile = other.PermissionProfile
	}
	if other.TokenUsage != nil {
		p.TokenUsage = other.TokenUsage
	}
	if other.FirstUserMessage != nil {
		p.FirstUserMessage = other.FirstUserMessage
	}
	if other.GitInfo != nil {
		if p.GitInfo == nil {
			p.GitInfo = &GitInfoPatch{}
		}
		p.GitInfo.Merge(*other.GitInfo)
	}
	if other.MemoryMode != nil {
		p.MemoryMode = other.MemoryMode
	}
}

// IsEmpty reports whether the patch carries no fields, mirroring the Rust
// `ThreadMetadataPatch::is_empty`.
func (p ThreadMetadataPatch) IsEmpty() bool {
	return !p.Name.IsSome() &&
		p.RolloutPath == nil &&
		p.Preview == nil &&
		p.Title == nil &&
		p.ModelProvider == nil &&
		p.Model == nil &&
		!p.ReasoningEffort.IsSome() &&
		p.CreatedAt == nil &&
		p.UpdatedAt == nil &&
		p.AdvanceRecencyAt == nil &&
		p.Source == nil &&
		!p.ThreadSource.IsSome() &&
		!p.AgentNickname.IsSome() &&
		!p.AgentRole.IsSome() &&
		!p.AgentPath.IsSome() &&
		p.Cwd == nil &&
		p.CliVersion == nil &&
		p.ApprovalMode == nil &&
		p.PermissionProfile == nil &&
		p.TokenUsage == nil &&
		p.FirstUserMessage == nil &&
		p.GitInfo == nil &&
		p.MemoryMode == nil
}

// MarshalJSON serializes the patch. Plain Option fields are omitted when nil;
// clearable fields use the `optional_option` semantics.
func (p ThreadMetadataPatch) MarshalJSON() ([]byte, error) {
	m := map[string]any{}
	putClearable(m, "name", p.Name)
	putOption(m, "rollout_path", p.RolloutPath)
	putOption(m, "preview", p.Preview)
	putOption(m, "title", p.Title)
	putOption(m, "model_provider", p.ModelProvider)
	putOption(m, "model", p.Model)
	putClearable(m, "reasoning_effort", p.ReasoningEffort)
	putOption(m, "created_at", p.CreatedAt)
	putOption(m, "updated_at", p.UpdatedAt)
	putOption(m, "advance_recency_at", p.AdvanceRecencyAt)
	putOption(m, "source", p.Source)
	putClearable(m, "thread_source", p.ThreadSource)
	putClearable(m, "agent_nickname", p.AgentNickname)
	putClearable(m, "agent_role", p.AgentRole)
	putClearable(m, "agent_path", p.AgentPath)
	putOption(m, "cwd", p.Cwd)
	putOption(m, "cli_version", p.CliVersion)
	putOption(m, "approval_mode", p.ApprovalMode)
	putOption(m, "permission_profile", p.PermissionProfile)
	putOption(m, "token_usage", p.TokenUsage)
	putOption(m, "first_user_message", p.FirstUserMessage)
	putOption(m, "git_info", p.GitInfo)
	putOption(m, "memory_mode", p.MemoryMode)
	return json.Marshal(m)
}

// UnmarshalJSON decodes the patch, distinguishing missing keys from null.
func (p *ThreadMetadataPatch) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var err error
	if p.Name, err = decodeClearable[string](raw, "name"); err != nil {
		return err
	}
	if p.RolloutPath, err = decodeOption[string](raw, "rollout_path"); err != nil {
		return err
	}
	if p.Preview, err = decodeOption[string](raw, "preview"); err != nil {
		return err
	}
	if p.Title, err = decodeOption[string](raw, "title"); err != nil {
		return err
	}
	if p.ModelProvider, err = decodeOption[string](raw, "model_provider"); err != nil {
		return err
	}
	if p.Model, err = decodeOption[string](raw, "model"); err != nil {
		return err
	}
	if p.ReasoningEffort, err = decodeClearable[protocol.ReasoningEffort](raw, "reasoning_effort"); err != nil {
		return err
	}
	if p.CreatedAt, err = decodeOption[time.Time](raw, "created_at"); err != nil {
		return err
	}
	if p.UpdatedAt, err = decodeOption[time.Time](raw, "updated_at"); err != nil {
		return err
	}
	if p.AdvanceRecencyAt, err = decodeOption[time.Time](raw, "advance_recency_at"); err != nil {
		return err
	}
	if p.Source, err = decodeOption[rollout.SessionSource](raw, "source"); err != nil {
		return err
	}
	if p.ThreadSource, err = decodeClearable[rollout.ThreadSource](raw, "thread_source"); err != nil {
		return err
	}
	if p.AgentNickname, err = decodeClearable[string](raw, "agent_nickname"); err != nil {
		return err
	}
	if p.AgentRole, err = decodeClearable[string](raw, "agent_role"); err != nil {
		return err
	}
	if p.AgentPath, err = decodeClearable[string](raw, "agent_path"); err != nil {
		return err
	}
	if p.Cwd, err = decodeOption[string](raw, "cwd"); err != nil {
		return err
	}
	if p.CliVersion, err = decodeOption[string](raw, "cli_version"); err != nil {
		return err
	}
	if p.ApprovalMode, err = decodeOption[protocol.AskForApproval](raw, "approval_mode"); err != nil {
		return err
	}
	if p.PermissionProfile, err = decodeOption[protocol.PermissionProfile](raw, "permission_profile"); err != nil {
		return err
	}
	if p.TokenUsage, err = decodeOption[protocol.TokenUsage](raw, "token_usage"); err != nil {
		return err
	}
	if p.FirstUserMessage, err = decodeOption[string](raw, "first_user_message"); err != nil {
		return err
	}
	if p.GitInfo, err = decodeOption[GitInfoPatch](raw, "git_info"); err != nil {
		return err
	}
	if p.MemoryMode, err = decodeOption[protocol.ThreadMemoryMode](raw, "memory_mode"); err != nil {
		return err
	}
	return nil
}

// putClearable writes a ClearableField into m using optional_option semantics.
func putClearable[T any](m map[string]any, key string, field ClearableField[T]) {
	if !field.Present {
		return
	}
	if field.Value == nil {
		m[key] = nil
		return
	}
	m[key] = *field.Value
}

// putOption writes a plain Option (pointer) into m, omitting nil. Mirrors serde
// for fields without skip_serializing_if where None encodes as null only when the
// Rust field is not skipped; here every patch Option field except the clearable
// ones is a plain `Option<T>` that serde serializes as null when None unless the
// surrounding type omits it. The Rust ThreadMetadataPatch derives Serialize
// without skip on plain fields, so None encodes as null. To match, we still emit
// null for present-but-nil. Since Go pointers cannot distinguish, nil is treated
// as None and emitted as null to match serde's default field behavior.
func putOption[T any](m map[string]any, key string, value *T) {
	if value == nil {
		m[key] = nil
		return
	}
	m[key] = *value
}

// decodeClearable decodes a ClearableField, treating a missing key as absent and
// an explicit null as a clear request.
func decodeClearable[T any](raw map[string]json.RawMessage, key string) (ClearableField[T], error) {
	data, ok := raw[key]
	if !ok {
		return ClearableField[T]{}, nil
	}
	if isJSONNull(data) {
		return ClearableField[T]{Present: true, Value: nil}, nil
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return ClearableField[T]{}, err
	}
	return ClearableField[T]{Present: true, Value: &v}, nil
}

// decodeOption decodes a plain Option, treating both a missing key and explicit
// null as None.
func decodeOption[T any](raw map[string]json.RawMessage, key string) (*T, error) {
	data, ok := raw[key]
	if !ok || isJSONNull(data) {
		return nil, nil
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func isJSONNull(data json.RawMessage) bool {
	return string(data) == "null"
}
