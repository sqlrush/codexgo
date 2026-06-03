package protocol

import "encoding/json"

// This file ports `dynamic_tools.rs`: the dynamic tool spec, call request,
// response, and output content item types. Rust uses camelCase for all of
// these.

// DynamicToolSpec describes a dynamically-registered tool.
//
// Serialization (Rust derives Serialize): camelCase. `namespace` is omitted
// when nil (`skip_serializing_if = "Option::is_none"`); `deferLoading` is
// always emitted with `#[serde(default)]`.
//
// Deserialization is custom in Rust: `deferLoading` defaults from the legacy
// `exposeToContext` field when absent (defer_loading = !expose_to_context).
type DynamicToolSpec struct {
	Namespace    *string         `json:"namespace,omitempty"`
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	DeferLoading bool            `json:"deferLoading"`
}

// UnmarshalJSON mirrors the Rust custom Deserialize: `deferLoading` falls back
// to the inverse of the legacy `exposeToContext` flag when not present.
func (d *DynamicToolSpec) UnmarshalJSON(data []byte) error {
	var raw struct {
		Namespace       *string         `json:"namespace"`
		Name            string          `json:"name"`
		Description     string          `json:"description"`
		InputSchema     json.RawMessage `json:"inputSchema"`
		DeferLoading    *bool           `json:"deferLoading"`
		ExposeToContext *bool           `json:"exposeToContext"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	defer_ := false
	switch {
	case raw.DeferLoading != nil:
		defer_ = *raw.DeferLoading
	case raw.ExposeToContext != nil:
		defer_ = !*raw.ExposeToContext
	}

	*d = DynamicToolSpec{
		Namespace:    raw.Namespace,
		Name:         raw.Name,
		Description:  raw.Description,
		InputSchema:  raw.InputSchema,
		DeferLoading: defer_,
	}
	return nil
}

// DynamicToolCallRequest is a request to invoke a dynamic tool.
//
// `startedAtMs` and `namespace` use `#[serde(default)]`.
type DynamicToolCallRequest struct {
	CallID      string          `json:"callId"`
	TurnID      string          `json:"turnId"`
	StartedAtMs int64           `json:"startedAtMs"`
	Namespace   *string         `json:"namespace"`
	Tool        string          `json:"tool"`
	Arguments   json.RawMessage `json:"arguments"`
}

// DynamicToolResponse is the result of a dynamic tool call.
type DynamicToolResponse struct {
	ContentItems []DynamicToolCallOutputContentItem `json:"contentItems"`
	Success      bool                               `json:"success"`
}

// DynamicToolCallOutputContentItemKind is the discriminator for a dynamic tool
// output content item. Rust: `#[serde(tag = "type", rename_all = "camelCase")]`.
type DynamicToolCallOutputContentItemKind string

const (
	DynamicToolCallOutputContentItemKindInputText  DynamicToolCallOutputContentItemKind = "inputText"
	DynamicToolCallOutputContentItemKindInputImage DynamicToolCallOutputContentItemKind = "inputImage"
)

// DynamicToolCallOutputContentItem is an internally-tagged content item that a
// dynamic tool can return.
type DynamicToolCallOutputContentItem struct {
	Type DynamicToolCallOutputContentItemKind

	// InputText variant.
	Text string
	// InputImage variant.
	ImageURL string
}

// MarshalJSON emits the internally-tagged form.
func (d DynamicToolCallOutputContentItem) MarshalJSON() ([]byte, error) {
	switch d.Type {
	case DynamicToolCallOutputContentItemKindInputText:
		return json.Marshal(struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{string(d.Type), d.Text})
	case DynamicToolCallOutputContentItemKindInputImage:
		return json.Marshal(struct {
			Type     string `json:"type"`
			ImageURL string `json:"imageUrl"`
		}{string(d.Type), d.ImageURL})
	default:
		return json.Marshal(map[string]any{"type": string(d.Type)})
	}
}

// UnmarshalJSON decodes the internally-tagged form.
func (d *DynamicToolCallOutputContentItem) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type     DynamicToolCallOutputContentItemKind `json:"type"`
		Text     string                               `json:"text"`
		ImageURL string                               `json:"imageUrl"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*d = DynamicToolCallOutputContentItem{
		Type:     raw.Type,
		Text:     raw.Text,
		ImageURL: raw.ImageURL,
	}
	return nil
}
