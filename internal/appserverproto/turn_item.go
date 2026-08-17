package appserverproto

import (
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// This file ports codex-rs app-server-protocol v2/turn.rs: the turn/* request
// methods (turn/start, turn/steer, turn/interrupt), the turn/* and item/*
// lifecycle notifications, and the v2 UserInput union. ThreadItem and the
// item.rs payload types live in item_thread.go (same package).
//
// Cross-area types owned by sibling files (Turn from thread_data.rs,
// SandboxPolicy from permissions.rs, etc.) are carried as json.RawMessage
// passthrough to stay byte-for-byte wire-faithful without redefining types that
// belong to another agent's scope. This mirrors the established v1.go pattern
// (e.g. ConversationSummary.Source).

// TurnStatus is the lifecycle status of a turn. Rust: v2::TurnStatus,
// serde rename_all = "camelCase".
type TurnStatus string

const (
	// TurnStatusCompleted means the turn finished successfully.
	TurnStatusCompleted TurnStatus = "completed"
	// TurnStatusInterrupted means the turn was interrupted.
	TurnStatusInterrupted TurnStatus = "interrupted"
	// TurnStatusFailed means the turn ended in error.
	TurnStatusFailed TurnStatus = "failed"
	// TurnStatusInProgress means the turn is still running.
	TurnStatusInProgress TurnStatus = "inProgress"
)

// TurnEnvironmentParams selects a turn-scoped environment by id and cwd.
// Rust: v2::TurnEnvironmentParams, camelCase. cwd is an absolute path.
type TurnEnvironmentParams struct {
	EnvironmentID string                `json:"environmentId"`
	Cwd           protocol.AbsolutePath `json:"cwd"`
}

// AdditionalContextKind classifies a client-provided context fragment.
// Rust: v2::AdditionalContextKind, serde rename_all = "lowercase".
type AdditionalContextKind string

const (
	// AdditionalContextKindUntrusted marks untrusted context.
	AdditionalContextKindUntrusted AdditionalContextKind = "untrusted"
	// AdditionalContextKindApplication marks application-supplied context.
	AdditionalContextKindApplication AdditionalContextKind = "application"
)

// AdditionalContextEntry is one client-provided context fragment.
// Rust: v2::AdditionalContextEntry, camelCase.
type AdditionalContextEntry struct {
	Value string                `json:"value"`
	Kind  AdditionalContextKind `json:"kind"`
}

// TurnStartParams starts a new turn on a thread. Rust: v2::TurnStartParams,
// camelCase, derive(Default).
//
// Field-emission rules (matched from the Rust attributes):
//   - input always serializes (Vec, even empty -> []).
//   - The Option fields below carry #[ts(optional = nullable)] but, except for
//     serviceTier, have no serde skip_serializing_if, so they are ALWAYS
//     emitted on the wire (null when absent). Go pointer fields without
//     omitempty replicate this.
//   - serviceTier uses default + double_option + skip_serializing_if, i.e. three
//     states: absent (omitted), null, value. Modeled by DoubleOption with the
//     `omitzero` tag.
//
// Cross-area override types (approvalPolicy, approvalsReviewer, sandboxPolicy,
// collaborationMode reuse) are taken as json.RawMessage passthrough so this file
// compiles against the foundation regardless of sibling load order, while the
// emitted JSON stays exact. effort/summary/personality reuse the internal
// protocol enums.
type TurnStartParams struct {
	ThreadID               string                            `json:"threadId"`
	ClientUserMessageID    *string                           `json:"clientUserMessageId"`
	Input                  []UserInput                       `json:"input"`
	ResponsesapiClientMeta map[string]string                 `json:"responsesapiClientMetadata"`
	AdditionalContext      map[string]AdditionalContextEntry `json:"additionalContext"`
	Environments           []TurnEnvironmentParams           `json:"environments"`
	Cwd                    *string                           `json:"cwd"`
	RuntimeWorkspaceRoots  []string                          `json:"runtimeWorkspaceRoots"`
	ApprovalPolicy         json.RawMessage                   `json:"approvalPolicy"`
	ApprovalsReviewer      json.RawMessage                   `json:"approvalsReviewer"`
	SandboxPolicy          json.RawMessage                   `json:"sandboxPolicy"`
	Permissions            *string                           `json:"permissions"`
	Model                  *string                           `json:"model"`
	ServiceTier            DoubleOption[string]              `json:"serviceTier,omitzero"`
	Effort                 *protocol.ReasoningEffort         `json:"effort"`
	Summary                *protocol.ReasoningSummary        `json:"summary"`
	Personality            *protocol.Personality             `json:"personality"`
	OutputSchema           json.RawMessage                   `json:"outputSchema"`
	CollaborationMode      json.RawMessage                   `json:"collaborationMode"`
}

// turnStartParamsAlias mirrors TurnStartParams without custom marshalling so the
// json.RawMessage passthrough fields render their literal JSON (never the base64
// []byte form) while still emitting null for absent passthrough fields.
type turnStartParamsAlias TurnStartParams

// MarshalJSON renders the params, normalizing nil json.RawMessage passthrough
// fields to the JSON null literal so absent cross-area options emit null (their
// Rust counterparts lack skip_serializing_if), matching the reference exactly.
// Input is a Rust Vec (not Option), so it always serializes as an array (-> []
// when empty, never null).
func (p TurnStartParams) MarshalJSON() ([]byte, error) {
	a := turnStartParamsAlias(p)
	a.Input = orEmptyUserInput(a.Input)
	a.ApprovalPolicy = rawOrNull(a.ApprovalPolicy)
	a.ApprovalsReviewer = rawOrNull(a.ApprovalsReviewer)
	a.SandboxPolicy = rawOrNull(a.SandboxPolicy)
	a.OutputSchema = rawOrNull(a.OutputSchema)
	a.CollaborationMode = rawOrNull(a.CollaborationMode)
	return json.Marshal(a)
}

// TurnStartResponse wraps the started Turn. Rust: v2::TurnStartResponse,
// camelCase. The Turn type is owned by thread_data.rs (sibling); to keep this
// file self-contained it is carried as raw JSON.
type TurnStartResponse struct {
	Turn json.RawMessage `json:"turn"`
}

// turnStartResponseAlias avoids recursion in MarshalJSON.
type turnStartResponseAlias TurnStartResponse

// MarshalJSON normalizes a nil Turn to null so the key is always present, as the
// Rust struct field has no skip_serializing_if.
func (r TurnStartResponse) MarshalJSON() ([]byte, error) {
	a := turnStartResponseAlias(r)
	a.Turn = rawOrNull(a.Turn)
	return json.Marshal(a)
}

// TurnSteerParams injects additional input into the active turn.
// Rust: v2::TurnSteerParams, camelCase, derive(Default). Same emission rules as
// TurnStartParams for the shared fields; expectedTurnId is required.
type TurnSteerParams struct {
	ThreadID               string                            `json:"threadId"`
	ClientUserMessageID    *string                           `json:"clientUserMessageId"`
	Input                  []UserInput                       `json:"input"`
	ResponsesapiClientMeta map[string]string                 `json:"responsesapiClientMetadata"`
	AdditionalContext      map[string]AdditionalContextEntry `json:"additionalContext"`
	ExpectedTurnID         string                            `json:"expectedTurnId"`
}

// turnSteerParamsAlias mirrors TurnSteerParams without custom marshalling to
// avoid MarshalJSON recursion.
type turnSteerParamsAlias TurnSteerParams

// MarshalJSON renders the params, normalizing the input Vec to an array. Input is
// a Rust Vec (not Option), so it always serializes as [] when empty, never null.
func (p TurnSteerParams) MarshalJSON() ([]byte, error) {
	a := turnSteerParamsAlias(p)
	a.Input = orEmptyUserInput(a.Input)
	return json.Marshal(a)
}

// TurnSteerResponse identifies the active turn that received the steer.
// Rust: v2::TurnSteerResponse, camelCase.
type TurnSteerResponse struct {
	TurnID string `json:"turnId"`
}

// TurnInterruptParams interrupts the identified turn on a thread.
// Rust: v2::TurnInterruptParams, camelCase.
type TurnInterruptParams struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
}

// TurnInterruptResponse is the empty interrupt acknowledgement.
// Rust: v2::TurnInterruptResponse (unit struct -> serializes as {}).
type TurnInterruptResponse struct{}

// UserInputKind discriminates the v2 UserInput union (the "type" tag value).
// Rust: v2::UserInput, serde tag = "type", rename_all = "camelCase".
type UserInputKind string

const (
	// UserInputKindText is plain text input with optional UI spans.
	UserInputKindText UserInputKind = "text"
	// UserInputKindImage is a remote image URL.
	UserInputKindImage UserInputKind = "image"
	// UserInputKindLocalImage is a local image path.
	UserInputKindLocalImage UserInputKind = "localImage"
	// UserInputKindSkill references a skill by name and path.
	UserInputKindSkill UserInputKind = "skill"
	// UserInputKindMention references a file/mention by name and path.
	UserInputKindMention UserInputKind = "mention"
)

// ByteRange is a half-open [start, end) byte range within a text buffer.
// Rust: v2::ByteRange, camelCase. start/end are usize.
type ByteRange struct {
	Start uint64 `json:"start"`
	End   uint64 `json:"end"`
}

// TextElement is a UI span within a UserInput text buffer.
// Rust: v2::TextElement, camelCase. placeholder has no skip_serializing_if, so
// it is always emitted (null when absent).
type TextElement struct {
	ByteRange   ByteRange `json:"byteRange"`
	Placeholder *string   `json:"placeholder"`
}

// UserInput is the v2 internally-tagged user-input union. Exactly one variant is
// populated; the active variant is selected by Kind and discriminated on the
// wire by the "type" field.
//
// Emission rules from the Rust enum:
//   - Text: text (string) + textElements (Vec, always emitted as [] when empty).
//   - Image / LocalImage: detail (Option, #[serde(default)], no skip -> always
//     emitted, null when absent) + url / path.
//   - Skill / Mention: name + path (both required strings; Skill.path is a
//     PathBuf, Mention.path a String, both bare strings on the wire).
type UserInput struct {
	Kind UserInputKind

	// Text variant.
	Text         string
	TextElements []TextElement

	// Image / LocalImage variant.
	Detail *protocol.ImageDetail
	URL    string // Image
	Path   string // LocalImage, Skill, Mention

	// Skill / Mention variant.
	Name string
}

// userInputText is the wire shape of the Text variant.
type userInputText struct {
	Type         UserInputKind `json:"type"`
	Text         string        `json:"text"`
	TextElements []TextElement `json:"textElements"`
}

// userInputImage is the wire shape of the Image variant.
type userInputImage struct {
	Type   UserInputKind         `json:"type"`
	Detail *protocol.ImageDetail `json:"detail"`
	URL    string                `json:"url"`
}

// userInputLocalImage is the wire shape of the LocalImage variant.
type userInputLocalImage struct {
	Type   UserInputKind         `json:"type"`
	Detail *protocol.ImageDetail `json:"detail"`
	Path   string                `json:"path"`
}

// userInputNamePath is the wire shape of the Skill and Mention variants.
type userInputNamePath struct {
	Type UserInputKind `json:"type"`
	Name string        `json:"name"`
	Path string        `json:"path"`
}

// MarshalJSON encodes the active UserInput variant with its "type" tag.
func (u UserInput) MarshalJSON() ([]byte, error) {
	switch u.Kind {
	case UserInputKindText:
		te := u.TextElements
		if te == nil {
			te = []TextElement{}
		}
		return json.Marshal(userInputText{Type: u.Kind, Text: u.Text, TextElements: te})
	case UserInputKindImage:
		return json.Marshal(userInputImage{Type: u.Kind, Detail: u.Detail, URL: u.URL})
	case UserInputKindLocalImage:
		return json.Marshal(userInputLocalImage{Type: u.Kind, Detail: u.Detail, Path: u.Path})
	case UserInputKindSkill, UserInputKindMention:
		return json.Marshal(userInputNamePath{Type: u.Kind, Name: u.Name, Path: u.Path})
	default:
		return nil, fmt.Errorf("appserverproto: unknown UserInput kind: %q", u.Kind)
	}
}

// UnmarshalJSON decodes a UserInput by its "type" tag.
func (u *UserInput) UnmarshalJSON(data []byte) error {
	var tag struct {
		Type UserInputKind `json:"type"`
	}
	if err := json.Unmarshal(data, &tag); err != nil {
		return fmt.Errorf("appserverproto: UserInput missing type tag: %w", err)
	}
	switch tag.Type {
	case UserInputKindText:
		var v userInputText
		if err := json.Unmarshal(data, &v); err != nil {
			return err
		}
		*u = UserInput{Kind: v.Type, Text: v.Text, TextElements: v.TextElements}
	case UserInputKindImage:
		var v userInputImage
		if err := json.Unmarshal(data, &v); err != nil {
			return err
		}
		*u = UserInput{Kind: v.Type, Detail: v.Detail, URL: v.URL}
	case UserInputKindLocalImage:
		var v userInputLocalImage
		if err := json.Unmarshal(data, &v); err != nil {
			return err
		}
		*u = UserInput{Kind: v.Type, Detail: v.Detail, Path: v.Path}
	case UserInputKindSkill, UserInputKindMention:
		var v userInputNamePath
		if err := json.Unmarshal(data, &v); err != nil {
			return err
		}
		*u = UserInput{Kind: v.Type, Name: v.Name, Path: v.Path}
	default:
		return fmt.Errorf("appserverproto: unknown UserInput type tag: %q", tag.Type)
	}
	return nil
}

// TurnStartedNotification is emitted when a turn begins.
// Rust: v2::TurnStartedNotification, camelCase. Turn is a sibling type, carried
// as raw JSON (always emitted; no skip_serializing_if on the Rust field).
type TurnStartedNotification struct {
	ThreadID string          `json:"threadId"`
	Turn     json.RawMessage `json:"turn"`
}

// turnStartedAlias avoids recursion in MarshalJSON.
type turnStartedAlias TurnStartedNotification

// MarshalJSON normalizes a nil Turn to null.
func (n TurnStartedNotification) MarshalJSON() ([]byte, error) {
	a := turnStartedAlias(n)
	a.Turn = rawOrNull(a.Turn)
	return json.Marshal(a)
}

// Usage reports token usage for a turn. Rust: v2::Usage, camelCase. All counts
// are i32 and always emitted.
type Usage struct {
	InputTokens       int32 `json:"inputTokens"`
	CachedInputTokens int32 `json:"cachedInputTokens"`
	OutputTokens      int32 `json:"outputTokens"`
}

// TurnCompletedNotification is emitted when a turn finishes.
// Rust: v2::TurnCompletedNotification, camelCase.
type TurnCompletedNotification struct {
	ThreadID string          `json:"threadId"`
	Turn     json.RawMessage `json:"turn"`
}

// turnCompletedAlias avoids recursion in MarshalJSON.
type turnCompletedAlias TurnCompletedNotification

// MarshalJSON normalizes a nil Turn to null.
func (n TurnCompletedNotification) MarshalJSON() ([]byte, error) {
	a := turnCompletedAlias(n)
	a.Turn = rawOrNull(a.Turn)
	return json.Marshal(a)
}

// rawOrNull returns the JSON null literal for an empty raw message, otherwise the
// raw message unchanged. Used so passthrough fields without skip_serializing_if
// emit null when absent instead of an invalid empty token.
func rawOrNull(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("null")
	}
	return raw
}

func init() {
	Register(MethodSpec{
		Method:    "turn/start",
		NewParams: func() any { return new(TurnStartParams) },
		NewResult: func() any { return new(TurnStartResponse) },
	})
	Register(MethodSpec{
		Method:    "turn/steer",
		NewParams: func() any { return new(TurnSteerParams) },
		NewResult: func() any { return new(TurnSteerResponse) },
	})
	Register(MethodSpec{
		Method:    "turn/interrupt",
		NewParams: func() any { return new(TurnInterruptParams) },
		NewResult: func() any { return new(TurnInterruptResponse) },
	})

	RegisterNotification(NotificationSpec{
		Method:    "turn/started",
		NewParams: func() any { return new(TurnStartedNotification) },
	})
	RegisterNotification(NotificationSpec{
		Method:    "turn/completed",
		NewParams: func() any { return new(TurnCompletedNotification) },
	})
	RegisterNotification(NotificationSpec{
		Method:    "item/started",
		NewParams: func() any { return new(ItemStartedNotification) },
	})
	RegisterNotification(NotificationSpec{
		Method:    "item/completed",
		NewParams: func() any { return new(ItemCompletedNotification) },
	})
}
