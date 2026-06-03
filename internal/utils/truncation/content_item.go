package truncation

import (
	"encoding/json"
	"fmt"
)

// ImageDetail mirrors codex_protocol::models::ImageDetail. It is serialized in
// lowercase to match the Responses API wire format.
type ImageDetail string

const (
	// ImageDetailAuto corresponds to ImageDetail::Auto.
	ImageDetailAuto ImageDetail = "auto"
	// ImageDetailLow corresponds to ImageDetail::Low.
	ImageDetailLow ImageDetail = "low"
	// ImageDetailHigh corresponds to ImageDetail::High.
	ImageDetailHigh ImageDetail = "high"
	// ImageDetailOriginal corresponds to ImageDetail::Original.
	ImageDetailOriginal ImageDetail = "original"
)

// DefaultImageDetail mirrors codex_protocol::models::DEFAULT_IMAGE_DETAIL.
const DefaultImageDetail = ImageDetailHigh

// ItemKind identifies the variant of a FunctionCallOutputContentItem.
type ItemKind int

const (
	// KindInputText is the input_text variant.
	KindInputText ItemKind = iota
	// KindInputImage is the input_image variant.
	KindInputImage
	// KindEncryptedContent is the encrypted_content variant.
	KindEncryptedContent
)

// FunctionCallOutputContentItem mirrors
// codex_protocol::models::FunctionCallOutputContentItem, a tagged union of the
// content items a tool call may return. Exactly one variant's fields are
// meaningful, selected by Kind.
//
// The struct is treated immutably by this package: helper functions never
// mutate caller-provided items, and the constructors return new values.
type FunctionCallOutputContentItem struct {
	// Kind selects which variant this item represents.
	Kind ItemKind

	// Text holds the body for the input_text variant.
	Text string

	// ImageURL holds the image reference for the input_image variant.
	ImageURL string

	// Detail holds the optional image detail for the input_image variant.
	// A nil pointer represents the absence of the field (serde skips it).
	Detail *ImageDetail

	// EncryptedContent holds the opaque payload for the encrypted_content
	// variant.
	EncryptedContent string
}

// NewInputText returns an input_text content item.
func NewInputText(text string) FunctionCallOutputContentItem {
	return FunctionCallOutputContentItem{Kind: KindInputText, Text: text}
}

// NewInputImage returns an input_image content item. A nil detail omits the
// optional detail field, matching serde's skip_serializing_if behavior.
func NewInputImage(imageURL string, detail *ImageDetail) FunctionCallOutputContentItem {
	return FunctionCallOutputContentItem{Kind: KindInputImage, ImageURL: imageURL, Detail: cloneDetail(detail)}
}

// NewEncryptedContent returns an encrypted_content content item.
func NewEncryptedContent(encrypted string) FunctionCallOutputContentItem {
	return FunctionCallOutputContentItem{Kind: KindEncryptedContent, EncryptedContent: encrypted}
}

// cloneDetail returns a deep copy of a detail pointer so that callers and
// callees never share the same underlying value.
func cloneDetail(d *ImageDetail) *ImageDetail {
	if d == nil {
		return nil
	}
	v := *d
	return &v
}

// Equal reports whether two items are equal across all meaningful fields for
// their kind, treating nil and non-nil detail pointers correctly.
func (it FunctionCallOutputContentItem) Equal(other FunctionCallOutputContentItem) bool {
	if it.Kind != other.Kind {
		return false
	}
	switch it.Kind {
	case KindInputText:
		return it.Text == other.Text
	case KindInputImage:
		if it.ImageURL != other.ImageURL {
			return false
		}
		switch {
		case it.Detail == nil && other.Detail == nil:
			return true
		case it.Detail == nil || other.Detail == nil:
			return false
		default:
			return *it.Detail == *other.Detail
		}
	case KindEncryptedContent:
		return it.EncryptedContent == other.EncryptedContent
	default:
		return false
	}
}

// itemJSON is the on-the-wire shape used for (de)serialization. It mirrors the
// serde representation: an internally tagged enum with a snake_case "type"
// discriminator and the input_image "detail" field omitted when absent.
type itemJSON struct {
	Type             string       `json:"type"`
	Text             *string      `json:"text,omitempty"`
	ImageURL         *string      `json:"image_url,omitempty"`
	Detail           *ImageDetail `json:"detail,omitempty"`
	EncryptedContent *string      `json:"encrypted_content,omitempty"`
}

// MarshalJSON serializes the item using the Responses API wire format that the
// upstream Rust types produce.
func (it FunctionCallOutputContentItem) MarshalJSON() ([]byte, error) {
	switch it.Kind {
	case KindInputText:
		text := it.Text
		return json.Marshal(itemJSON{Type: "input_text", Text: &text})
	case KindInputImage:
		url := it.ImageURL
		return json.Marshal(itemJSON{Type: "input_image", ImageURL: &url, Detail: cloneDetail(it.Detail)})
	case KindEncryptedContent:
		enc := it.EncryptedContent
		return json.Marshal(itemJSON{Type: "encrypted_content", EncryptedContent: &enc})
	default:
		return nil, fmt.Errorf("truncation: unknown FunctionCallOutputContentItem kind %d", int(it.Kind))
	}
}

// UnmarshalJSON parses an item from the Responses API wire format and validates
// that the discriminator and required fields are present.
func (it *FunctionCallOutputContentItem) UnmarshalJSON(data []byte) error {
	var raw itemJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("truncation: invalid content item: %w", err)
	}
	switch raw.Type {
	case "input_text":
		if raw.Text == nil {
			return fmt.Errorf("truncation: input_text item missing \"text\" field")
		}
		*it = NewInputText(*raw.Text)
		return nil
	case "input_image":
		if raw.ImageURL == nil {
			return fmt.Errorf("truncation: input_image item missing \"image_url\" field")
		}
		*it = NewInputImage(*raw.ImageURL, cloneDetail(raw.Detail))
		return nil
	case "encrypted_content":
		if raw.EncryptedContent == nil {
			return fmt.Errorf("truncation: encrypted_content item missing \"encrypted_content\" field")
		}
		*it = NewEncryptedContent(*raw.EncryptedContent)
		return nil
	case "":
		return fmt.Errorf("truncation: content item missing \"type\" discriminator")
	default:
		return fmt.Errorf("truncation: unknown content item type %q", raw.Type)
	}
}

// cloneItems returns a deep copy of an item slice so callers never share state
// with the helpers that produce truncated output.
func cloneItems(items []FunctionCallOutputContentItem) []FunctionCallOutputContentItem {
	out := make([]FunctionCallOutputContentItem, len(items))
	for i, it := range items {
		clone := it
		clone.Detail = cloneDetail(it.Detail)
		out[i] = clone
	}
	return out
}

// itemsEqual reports whether two item slices are element-wise equal.
func itemsEqual(a, b []FunctionCallOutputContentItem) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !a[i].Equal(b[i]) {
			return false
		}
	}
	return true
}
