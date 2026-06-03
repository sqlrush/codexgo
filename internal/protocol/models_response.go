package protocol

import (
	"encoding/json"
	"fmt"
)

// This file ports the Responses-API model types from `models.rs` that
// `ResponseItem` and the turn-item layer depend on: ContentItem,
// WebSearchAction, LocalShell{Status,Action,ExecAction}, the reasoning
// summary/content variants, and the dual-encoded FunctionCallOutputPayload.

// ContentItemKind is the discriminator for ContentItem. Rust:
// `#[serde(tag = "type", rename_all = "snake_case")]`.
type ContentItemKind string

const (
	ContentItemKindInputText  ContentItemKind = "input_text"
	ContentItemKindInputImage ContentItemKind = "input_image"
	ContentItemKindOutputText ContentItemKind = "output_text"
)

// ContentItem is a piece of message content (internally tagged on "type").
type ContentItem struct {
	Type ContentItemKind

	// InputText / OutputText variants.
	Text string
	// InputImage variant.
	ImageURL string
	Detail   *ImageDetail
}

// MarshalJSON emits the internally-tagged ContentItem.
func (c ContentItem) MarshalJSON() ([]byte, error) {
	m := map[string]any{"type": string(c.Type)}
	switch c.Type {
	case ContentItemKindInputText, ContentItemKindOutputText:
		m["text"] = c.Text
	case ContentItemKindInputImage:
		m["image_url"] = c.ImageURL
		if c.Detail != nil {
			m["detail"] = *c.Detail
		}
	default:
		return nil, fmt.Errorf("unknown ContentItem type: %q", c.Type)
	}
	return json.Marshal(m)
}

// UnmarshalJSON decodes the internally-tagged ContentItem.
func (c *ContentItem) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type     ContentItemKind `json:"type"`
		Text     string          `json:"text"`
		ImageURL string          `json:"image_url"`
		Detail   *ImageDetail    `json:"detail"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*c = ContentItem{Type: raw.Type, Text: raw.Text, ImageURL: raw.ImageURL, Detail: raw.Detail}
	return nil
}

// WebSearchActionKind is the discriminator for WebSearchAction. Rust:
// `#[serde(tag = "type", rename_all = "snake_case")]` with an `#[serde(other)]`
// fallback variant.
type WebSearchActionKind string

const (
	WebSearchActionKindSearch     WebSearchActionKind = "search"
	WebSearchActionKindOpenPage   WebSearchActionKind = "open_page"
	WebSearchActionKindFindInPage WebSearchActionKind = "find_in_page"
	// WebSearchActionKindOther is the catch-all for unknown action types
	// (#[serde(other)]).
	WebSearchActionKindOther WebSearchActionKind = "other"
)

// WebSearchAction describes a web search performed by the agent. Unknown action
// types deserialize to the Other variant (forward-compatible).
type WebSearchAction struct {
	Type WebSearchActionKind

	// Search variant (all optional).
	Query   *string
	Queries *[]string
	// OpenPage / FindInPage variants.
	URL *string
	// FindInPage variant.
	Pattern *string
}

// MarshalJSON emits the internally-tagged WebSearchAction. Optional fields use
// skip_serializing_if = Option::is_none semantics.
func (w WebSearchAction) MarshalJSON() ([]byte, error) {
	m := map[string]any{"type": string(w.Type)}
	switch w.Type {
	case WebSearchActionKindSearch:
		if w.Query != nil {
			m["query"] = *w.Query
		}
		if w.Queries != nil {
			m["queries"] = *w.Queries
		}
	case WebSearchActionKindOpenPage:
		if w.URL != nil {
			m["url"] = *w.URL
		}
	case WebSearchActionKindFindInPage:
		if w.URL != nil {
			m["url"] = *w.URL
		}
		if w.Pattern != nil {
			m["pattern"] = *w.Pattern
		}
	case WebSearchActionKindOther:
		// `#[serde(other)]` variants are unit; serde would error on serialize,
		// but for round-trip safety we emit just the type tag.
	default:
		return nil, fmt.Errorf("unknown WebSearchAction type: %q", w.Type)
	}
	return json.Marshal(m)
}

// UnmarshalJSON decodes the internally-tagged WebSearchAction, mapping unknown
// types to Other.
func (w *WebSearchAction) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type    string    `json:"type"`
		Query   *string   `json:"query"`
		Queries *[]string `json:"queries"`
		URL     *string   `json:"url"`
		Pattern *string   `json:"pattern"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	out := WebSearchAction{}
	switch WebSearchActionKind(raw.Type) {
	case WebSearchActionKindSearch:
		out.Type = WebSearchActionKindSearch
		out.Query = raw.Query
		out.Queries = raw.Queries
	case WebSearchActionKindOpenPage:
		out.Type = WebSearchActionKindOpenPage
		out.URL = raw.URL
	case WebSearchActionKindFindInPage:
		out.Type = WebSearchActionKindFindInPage
		out.URL = raw.URL
		out.Pattern = raw.Pattern
	default:
		out.Type = WebSearchActionKindOther
	}
	*w = out
	return nil
}

// LocalShellStatus is the status of a local shell call. Rust: snake_case.
type LocalShellStatus string

const (
	LocalShellStatusCompleted  LocalShellStatus = "completed"
	LocalShellStatusInProgress LocalShellStatus = "in_progress"
	LocalShellStatusIncomplete LocalShellStatus = "incomplete"
)

// LocalShellExecAction is the payload for an exec local shell action.
type LocalShellExecAction struct {
	Command          []string          `json:"command"`
	TimeoutMs        *uint64           `json:"timeout_ms"`
	WorkingDirectory *string           `json:"working_directory"`
	Env              map[string]string `json:"env"`
	User             *string           `json:"user"`
}

// LocalShellAction is an internally-tagged enum; the only variant is Exec, which
// flattens a LocalShellExecAction alongside the discriminator. Rust:
// `#[serde(tag = "type", rename_all = "snake_case")]` over `Exec(LocalShellExecAction)`.
type LocalShellAction struct {
	Type string `json:"type"` // always "exec"
	LocalShellExecAction
}

// NewLocalShellExecAction builds an Exec LocalShellAction.
func NewLocalShellExecAction(action LocalShellExecAction) LocalShellAction {
	return LocalShellAction{Type: "exec", LocalShellExecAction: action}
}

// ReasoningItemReasoningSummary is an internally-tagged reasoning summary. Rust:
// `#[serde(tag = "type", rename_all = "snake_case")]`; only SummaryText.
type ReasoningItemReasoningSummary struct {
	Type string `json:"type"` // always "summary_text"
	Text string `json:"text"`
}

// NewSummaryText builds a SummaryText reasoning summary.
func NewSummaryText(text string) ReasoningItemReasoningSummary {
	return ReasoningItemReasoningSummary{Type: "summary_text", Text: text}
}

// ReasoningItemContentKind is the discriminator for ReasoningItemContent. Rust:
// snake_case with variants ReasoningText and Text.
type ReasoningItemContentKind string

const (
	ReasoningItemContentKindReasoningText ReasoningItemContentKind = "reasoning_text"
	ReasoningItemContentKindText          ReasoningItemContentKind = "text"
)

// ReasoningItemContent is internally-tagged reasoning content.
type ReasoningItemContent struct {
	Type ReasoningItemContentKind `json:"type"`
	Text string                   `json:"text"`
}

// FunctionCallOutputPayload is the result of a tool call. On the wire the
// `output` field is encoded either as a plain string or as an array of
// structured content items (Rust uses a custom Serialize/Deserialize over an
// untagged body). `Success` is internal metadata that is never on the wire.
type FunctionCallOutputPayload struct {
	// Text is set (and ContentItems nil) for the string encoding.
	Text *string
	// ContentItems is set (and Text nil) for the array encoding.
	ContentItems []FunctionCallOutputContentItem
	// Success is internal metadata, not serialized.
	Success *bool
}

// FunctionCallOutputFromText builds a text-bodied payload.
func FunctionCallOutputFromText(text string) FunctionCallOutputPayload {
	return FunctionCallOutputPayload{Text: &text}
}

// FunctionCallOutputFromContentItems builds a content-items-bodied payload.
func FunctionCallOutputFromContentItems(items []FunctionCallOutputContentItem) FunctionCallOutputPayload {
	return FunctionCallOutputPayload{ContentItems: items}
}

// MarshalJSON encodes the body as either a string or an array, matching the
// Rust custom Serialize (the default body is the empty string).
func (p FunctionCallOutputPayload) MarshalJSON() ([]byte, error) {
	if p.ContentItems != nil {
		return json.Marshal(p.ContentItems)
	}
	if p.Text != nil {
		return json.Marshal(*p.Text)
	}
	return json.Marshal("")
}

// UnmarshalJSON decodes the dual string/array encoding (success stays nil).
func (p *FunctionCallOutputPayload) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*p = FunctionCallOutputPayload{Text: &s}
		return nil
	}
	var items []FunctionCallOutputContentItem
	if err := json.Unmarshal(data, &items); err != nil {
		return fmt.Errorf("function_call_output payload is neither string nor content-item array: %w", err)
	}
	*p = FunctionCallOutputPayload{ContentItems: items}
	return nil
}

// FunctionCallOutputContentItemKind is the discriminator for a function call
// output content item. Rust: snake_case.
type FunctionCallOutputContentItemKind string

const (
	FunctionCallOutputContentItemKindInputText        FunctionCallOutputContentItemKind = "input_text"
	FunctionCallOutputContentItemKindInputImage       FunctionCallOutputContentItemKind = "input_image"
	FunctionCallOutputContentItemKindEncryptedContent FunctionCallOutputContentItemKind = "encrypted_content"
)

// FunctionCallOutputContentItem is a structured content item returned by a tool.
type FunctionCallOutputContentItem struct {
	Type FunctionCallOutputContentItemKind

	// InputText variant.
	Text string
	// InputImage variant.
	ImageURL string
	Detail   *ImageDetail
	// EncryptedContent variant.
	EncryptedContent string
}

// MarshalJSON emits the internally-tagged content item.
func (f FunctionCallOutputContentItem) MarshalJSON() ([]byte, error) {
	m := map[string]any{"type": string(f.Type)}
	switch f.Type {
	case FunctionCallOutputContentItemKindInputText:
		m["text"] = f.Text
	case FunctionCallOutputContentItemKindInputImage:
		m["image_url"] = f.ImageURL
		if f.Detail != nil {
			m["detail"] = *f.Detail
		}
	case FunctionCallOutputContentItemKindEncryptedContent:
		m["encrypted_content"] = f.EncryptedContent
	default:
		return nil, fmt.Errorf("unknown FunctionCallOutputContentItem type: %q", f.Type)
	}
	return json.Marshal(m)
}

// UnmarshalJSON decodes the internally-tagged content item.
func (f *FunctionCallOutputContentItem) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type             FunctionCallOutputContentItemKind `json:"type"`
		Text             string                            `json:"text"`
		ImageURL         string                            `json:"image_url"`
		Detail           *ImageDetail                      `json:"detail"`
		EncryptedContent string                            `json:"encrypted_content"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*f = FunctionCallOutputContentItem{
		Type:             raw.Type,
		Text:             raw.Text,
		ImageURL:         raw.ImageURL,
		Detail:           raw.Detail,
		EncryptedContent: raw.EncryptedContent,
	}
	return nil
}
