// Package backendclient provides a small REST client for the Codex/ChatGPT
// backend-api used by cloud tasks and cloud requirements. It is a faithful Go
// port of the Rust `codex-backend-client` crate.
//
// The on-disk/wire JSON formats match the reference implementation exactly so
// the client is drop-in compatible with the Rust client.
package backendclient

import (
	"encoding/json"
	"fmt"
	"strings"
)

// CodeTaskDetailsResponse is a hand-rolled model for the Cloud Tasks
// task-details response. It mirrors the Rust `CodeTaskDetailsResponse`.
type CodeTaskDetailsResponse struct {
	CurrentUserTurn      *Turn `json:"current_user_turn"`
	CurrentAssistantTurn *Turn `json:"current_assistant_turn"`
	CurrentDiffTaskTurn  *Turn `json:"current_diff_task_turn"`
}

// Turn mirrors the Rust `Turn`.
type Turn struct {
	ID               *string    `json:"id"`
	AttemptPlacement *int64     `json:"attempt_placement"`
	TurnStatus       *string    `json:"turn_status"`
	SiblingTurnIDs   []string   `json:"sibling_turn_ids"`
	InputItems       []TurnItem `json:"input_items"`
	OutputItems      []TurnItem `json:"output_items"`
	Worklog          *Worklog   `json:"worklog"`
	Error            *TurnError `json:"error"`
}

// TurnItem mirrors the Rust `TurnItem`. The serde `type` field becomes Kind.
type TurnItem struct {
	Kind       string            `json:"type"`
	Role       *string           `json:"role"`
	Content    []ContentFragment `json:"content"`
	Diff       *string           `json:"diff"`
	OutputDiff *DiffPayload      `json:"output_diff"`
}

// ContentFragment is an untagged enum that is either a structured object with a
// content_type/text or a bare string. It mirrors the Rust `ContentFragment`.
type ContentFragment struct {
	// Structured holds the object form. Nil means the Text form is used.
	Structured *StructuredContent
	// Text holds the bare-string form.
	Text *string
}

// UnmarshalJSON decodes either the structured object or a bare string,
// matching serde's `#[serde(untagged)]` behavior (object first, then string).
func (c *ContentFragment) UnmarshalJSON(data []byte) error {
	var structured StructuredContent
	if err := json.Unmarshal(data, &structured); err == nil {
		c.Structured = &structured
		c.Text = nil
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("content fragment is neither structured nor string: %w", err)
	}
	c.Text = &s
	c.Structured = nil
	return nil
}

// text returns the displayable text of a fragment, or "" when none.
// It mirrors the Rust `ContentFragment::text`.
func (c ContentFragment) text() string {
	if c.Structured != nil {
		ct := ""
		if c.Structured.ContentType != nil {
			ct = *c.Structured.ContentType
		}
		if strings.EqualFold(ct, "text") && c.Structured.Text != nil && *c.Structured.Text != "" {
			return *c.Structured.Text
		}
		return ""
	}
	if c.Text != nil && strings.TrimSpace(*c.Text) != "" {
		return *c.Text
	}
	return ""
}

// StructuredContent mirrors the Rust `StructuredContent`.
type StructuredContent struct {
	ContentType *string `json:"content_type"`
	Text        *string `json:"text"`
}

// DiffPayload mirrors the Rust `DiffPayload`.
type DiffPayload struct {
	Diff *string `json:"diff"`
}

// Worklog mirrors the Rust `Worklog`.
type Worklog struct {
	Messages []WorklogMessage `json:"messages"`
}

// WorklogMessage mirrors the Rust `WorklogMessage`.
type WorklogMessage struct {
	Author  *Author         `json:"author"`
	Content *WorklogContent `json:"content"`
}

// Author mirrors the Rust `Author`.
type Author struct {
	Role *string `json:"role"`
}

// WorklogContent mirrors the Rust `WorklogContent`.
type WorklogContent struct {
	Parts []ContentFragment `json:"parts"`
}

// TurnError mirrors the Rust `TurnError`.
type TurnError struct {
	Code    *string `json:"code"`
	Message *string `json:"message"`
}

// textValues returns the text values of a turn item's content fragments.
func (t TurnItem) textValues() []string {
	out := make([]string, 0, len(t.Content))
	for _, fragment := range t.Content {
		if v := fragment.text(); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// diffText extracts the unified diff string from a turn item when present.
// It mirrors the Rust `TurnItem::diff_text`.
func (t TurnItem) diffText() (string, bool) {
	switch t.Kind {
	case "output_diff":
		if t.Diff != nil && *t.Diff != "" {
			return *t.Diff, true
		}
	case "pr":
		if t.OutputDiff != nil && t.OutputDiff.Diff != nil && *t.OutputDiff.Diff != "" {
			return *t.OutputDiff.Diff, true
		}
	}
	return "", false
}

func (t *Turn) unifiedDiff() (string, bool) {
	if t == nil {
		return "", false
	}
	for _, item := range t.OutputItems {
		if diff, ok := item.diffText(); ok {
			return diff, true
		}
	}
	return "", false
}

func (t *Turn) messageTexts() []string {
	if t == nil {
		return nil
	}
	var out []string
	for _, item := range t.OutputItems {
		if item.Kind == "message" {
			out = append(out, item.textValues()...)
		}
	}
	if t.Worklog != nil {
		for _, message := range t.Worklog.Messages {
			if message.isAssistant() {
				out = append(out, message.textValues()...)
			}
		}
	}
	return out
}

func (t *Turn) userPrompt() (string, bool) {
	if t == nil {
		return "", false
	}
	var parts []string
	for _, item := range t.InputItems {
		if item.Kind != "message" {
			continue
		}
		isUser := true
		if item.Role != nil {
			isUser = strings.EqualFold(*item.Role, "user")
		}
		if !isUser {
			continue
		}
		parts = append(parts, item.textValues()...)
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, "\n\n"), true
}

func (t *Turn) errorSummary() (string, bool) {
	if t == nil || t.Error == nil {
		return "", false
	}
	return t.Error.summary()
}

func (m WorklogMessage) isAssistant() bool {
	if m.Author == nil || m.Author.Role == nil {
		return false
	}
	return strings.EqualFold(*m.Author.Role, "assistant")
}

func (m WorklogMessage) textValues() []string {
	if m.Content == nil {
		return nil
	}
	out := make([]string, 0, len(m.Content.Parts))
	for _, fragment := range m.Content.Parts {
		if v := fragment.text(); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func (e *TurnError) summary() (string, bool) {
	code := ""
	if e.Code != nil {
		code = *e.Code
	}
	message := ""
	if e.Message != nil {
		message = *e.Message
	}
	switch {
	case code == "" && message == "":
		return "", false
	case code != "" && message == "":
		return code, true
	case code == "" && message != "":
		return message, true
	default:
		return fmt.Sprintf("%s: %s", code, message), true
	}
}

// UnifiedDiff attempts to extract a unified diff string from the diff or
// assistant turn. It mirrors the Rust `CodeTaskDetailsResponseExt::unified_diff`.
func (r CodeTaskDetailsResponse) UnifiedDiff() (string, bool) {
	for _, turn := range []*Turn{r.CurrentDiffTaskTurn, r.CurrentAssistantTurn} {
		if diff, ok := turn.unifiedDiff(); ok {
			return diff, true
		}
	}
	return "", false
}

// AssistantTextMessages extracts assistant text output messages (no diff) from
// the current turns. It mirrors the Rust `assistant_text_messages`.
func (r CodeTaskDetailsResponse) AssistantTextMessages() []string {
	var out []string
	for _, turn := range []*Turn{r.CurrentDiffTaskTurn, r.CurrentAssistantTurn} {
		out = append(out, turn.messageTexts()...)
	}
	return out
}

// UserTextPrompt extracts the user's prompt text from the current user turn.
// It mirrors the Rust `user_text_prompt`.
func (r CodeTaskDetailsResponse) UserTextPrompt() (string, bool) {
	return r.CurrentUserTurn.userPrompt()
}

// AssistantErrorMessage extracts an assistant error message when the turn
// failed and provided one. It mirrors the Rust `assistant_error_message`.
func (r CodeTaskDetailsResponse) AssistantErrorMessage() (string, bool) {
	return r.CurrentAssistantTurn.errorSummary()
}

// TaskListItem mirrors the relevant fields of the backend task list item.
type TaskListItem struct {
	ID                string                     `json:"id"`
	Title             string                     `json:"title"`
	UpdatedAt         *float64                   `json:"updated_at"`
	TaskStatusDisplay map[string]json.RawMessage `json:"task_status_display"`
	PullRequests      []json.RawMessage          `json:"pull_requests"`
}

// PaginatedListTaskListItem mirrors the backend paginated task list response.
type PaginatedListTaskListItem struct {
	Items  []TaskListItem `json:"items"`
	Cursor *string        `json:"cursor"`
}

// TurnAttemptsSiblingTurnsResponse mirrors the Rust
// `TurnAttemptsSiblingTurnsResponse`.
type TurnAttemptsSiblingTurnsResponse struct {
	SiblingTurns []map[string]json.RawMessage `json:"sibling_turns"`
}

// ConfigFileResponse mirrors the backend config-file response used by cloud
// requirements. Only the contents field is consumed.
type ConfigFileResponse struct {
	Contents *string `json:"contents"`
}
