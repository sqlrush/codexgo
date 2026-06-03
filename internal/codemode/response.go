// Package codemode is a faithful Go port of codex's code-mode crate. It runs
// model-authored JavaScript that orchestrates nested tool calls, capturing
// text/image output into a RuntimeResponse.
//
// The reference implementation embeds a V8 isolate. This port uses the pure-Go
// goja engine instead. JavaScript-feature differences between goja and V8 are
// documented in deviations.go.
package codemode

import (
	"encoding/json"
	"fmt"
)

// ImageDetail mirrors codex's `ImageDetail` enum. It serializes lowercase to
// match serde's `#[serde(rename_all = "lowercase")]`.
type ImageDetail int

// ImageDetail variants.
const (
	ImageDetailAuto ImageDetail = iota
	ImageDetailLow
	ImageDetailHigh
	ImageDetailOriginal
)

// DefaultImageDetail mirrors codex's `DEFAULT_IMAGE_DETAIL`.
const DefaultImageDetail = ImageDetailHigh

// String renders the lowercase serde representation.
func (d ImageDetail) String() string {
	switch d {
	case ImageDetailAuto:
		return "auto"
	case ImageDetailLow:
		return "low"
	case ImageDetailHigh:
		return "high"
	case ImageDetailOriginal:
		return "original"
	default:
		return "high"
	}
}

// MarshalJSON emits the lowercase variant name.
func (d ImageDetail) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

// UnmarshalJSON parses the lowercase variant name.
func (d *ImageDetail) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("decode image detail: %w", err)
	}
	parsed, ok := ParseImageDetail(raw)
	if !ok {
		return fmt.Errorf("invalid image detail %q", raw)
	}
	*d = parsed
	return nil
}

// ParseImageDetail converts the lowercase variant name into an ImageDetail.
func ParseImageDetail(raw string) (ImageDetail, bool) {
	switch raw {
	case "auto":
		return ImageDetailAuto, true
	case "low":
		return ImageDetailLow, true
	case "high":
		return ImageDetailHigh, true
	case "original":
		return ImageDetailOriginal, true
	default:
		return 0, false
	}
}

// FunctionCallOutputContentItem mirrors codex's internally-tagged
// `FunctionCallOutputContentItem` enum. The discriminant is carried in `Kind`
// and serialized via the `type` field ("input_text" / "input_image"), matching
// serde's `#[serde(tag = "type", rename_all = "snake_case")]`.
type FunctionCallOutputContentItem struct {
	// Kind selects which variant the item represents.
	Kind ContentItemKind
	// Text is populated for InputText items.
	Text string
	// ImageURL is populated for InputImage items.
	ImageURL string
	// Detail is the optional image detail for InputImage items. When nil it is
	// omitted from JSON to match serde's skip_serializing_if = "Option::is_none".
	Detail *ImageDetail
}

// ContentItemKind enumerates FunctionCallOutputContentItem variants.
type ContentItemKind int

// ContentItemKind variants.
const (
	ContentItemInputText ContentItemKind = iota
	ContentItemInputImage
)

// NewTextItem constructs an input_text content item.
func NewTextItem(text string) FunctionCallOutputContentItem {
	return FunctionCallOutputContentItem{Kind: ContentItemInputText, Text: text}
}

// NewImageItem constructs an input_image content item.
func NewImageItem(imageURL string, detail *ImageDetail) FunctionCallOutputContentItem {
	return FunctionCallOutputContentItem{Kind: ContentItemInputImage, ImageURL: imageURL, Detail: detail}
}

// MarshalJSON renders the internally-tagged JSON shape codex produces.
func (item FunctionCallOutputContentItem) MarshalJSON() ([]byte, error) {
	switch item.Kind {
	case ContentItemInputText:
		return json.Marshal(struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{Type: "input_text", Text: item.Text})
	case ContentItemInputImage:
		payload := struct {
			Type     string       `json:"type"`
			ImageURL string       `json:"image_url"`
			Detail   *ImageDetail `json:"detail,omitempty"`
		}{Type: "input_image", ImageURL: item.ImageURL, Detail: item.Detail}
		return json.Marshal(payload)
	default:
		return nil, fmt.Errorf("unknown content item kind %d", item.Kind)
	}
}

// UnmarshalJSON parses the internally-tagged JSON shape.
func (item *FunctionCallOutputContentItem) UnmarshalJSON(data []byte) error {
	var probe struct {
		Type     string       `json:"type"`
		Text     string       `json:"text"`
		ImageURL string       `json:"image_url"`
		Detail   *ImageDetail `json:"detail"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return fmt.Errorf("decode content item: %w", err)
	}
	switch probe.Type {
	case "input_text":
		*item = FunctionCallOutputContentItem{Kind: ContentItemInputText, Text: probe.Text}
	case "input_image":
		*item = FunctionCallOutputContentItem{Kind: ContentItemInputImage, ImageURL: probe.ImageURL, Detail: probe.Detail}
	default:
		return fmt.Errorf("unknown content item type %q", probe.Type)
	}
	return nil
}
