package protocol

import (
	"encoding/json"
	"fmt"
)

// MaxUserInputTextChars is the conservative cap on a single user message so it
// cannot monopolize a large context window.
const MaxUserInputTextChars = 1 << 20

// UserInputKind is the discriminator for UserInput. Rust:
// `#[serde(tag = "type", rename_all = "snake_case")]`.
type UserInputKind string

const (
	UserInputKindText       UserInputKind = "text"
	UserInputKindImage      UserInputKind = "image"
	UserInputKindLocalImage UserInputKind = "local_image"
	UserInputKindSkill      UserInputKind = "skill"
	UserInputKindMention    UserInputKind = "mention"
)

// UserInput is a single piece of user input. It is an internally-tagged enum
// (discriminator "type"); Rust marks it `#[non_exhaustive]`, so unknown
// variants must be tolerated. Unknown variants are preserved verbatim in Extra.
type UserInput struct {
	Type UserInputKind

	// Text variant.
	Text         string        `json:"-"`
	TextElements []TextElement `json:"-"`

	// Image variant: pre-encoded data: URI image.
	ImageURL string `json:"-"`

	// LocalImage variant: local image path provided by the user.
	Path string `json:"-"`

	// Image / LocalImage shared optional detail.
	Detail *ImageDetail `json:"-"`

	// Skill / Mention variant name.
	Name string `json:"-"`

	// Skill variant uses Path (above); Mention variant uses MentionPath.
	MentionPath string `json:"-"`

	// Extra captures fields of unknown (forward-compatible) variants.
	Extra map[string]json.RawMessage `json:"-"`
}

// MarshalJSON emits the internally-tagged form for the active variant.
func (u UserInput) MarshalJSON() ([]byte, error) {
	m := map[string]any{"type": string(u.Type)}
	switch u.Type {
	case UserInputKindText:
		m["text"] = u.Text
		// text_elements has #[serde(default)] but no skip_serializing_if, so it
		// is always emitted (as [] when empty).
		if u.TextElements == nil {
			m["text_elements"] = []TextElement{}
		} else {
			m["text_elements"] = u.TextElements
		}
	case UserInputKindImage:
		m["image_url"] = u.ImageURL
		if u.Detail != nil {
			m["detail"] = *u.Detail
		}
	case UserInputKindLocalImage:
		m["path"] = u.Path
		if u.Detail != nil {
			m["detail"] = *u.Detail
		}
	case UserInputKindSkill:
		m["name"] = u.Name
		m["path"] = u.Path
	case UserInputKindMention:
		m["name"] = u.Name
		m["path"] = u.MentionPath
	default:
		// Forward-compatible unknown variant: re-emit captured fields.
		for k, v := range u.Extra {
			m[k] = v
		}
	}
	return json.Marshal(m)
}

// UnmarshalJSON decodes the internally-tagged form, tolerating unknown
// variants (the Rust enum is #[non_exhaustive]).
func (u *UserInput) UnmarshalJSON(data []byte) error {
	var probe struct {
		Type UserInputKind `json:"type"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	out := UserInput{Type: probe.Type}
	switch probe.Type {
	case UserInputKindText:
		var v struct {
			Text         string        `json:"text"`
			TextElements []TextElement `json:"text_elements"`
		}
		if err := json.Unmarshal(data, &v); err != nil {
			return err
		}
		out.Text = v.Text
		out.TextElements = v.TextElements
	case UserInputKindImage:
		var v struct {
			ImageURL string       `json:"image_url"`
			Detail   *ImageDetail `json:"detail"`
		}
		if err := json.Unmarshal(data, &v); err != nil {
			return err
		}
		out.ImageURL = v.ImageURL
		out.Detail = v.Detail
	case UserInputKindLocalImage:
		var v struct {
			Path   string       `json:"path"`
			Detail *ImageDetail `json:"detail"`
		}
		if err := json.Unmarshal(data, &v); err != nil {
			return err
		}
		out.Path = v.Path
		out.Detail = v.Detail
	case UserInputKindSkill:
		var v struct {
			Name string `json:"name"`
			Path string `json:"path"`
		}
		if err := json.Unmarshal(data, &v); err != nil {
			return err
		}
		out.Name = v.Name
		out.Path = v.Path
	case UserInputKindMention:
		var v struct {
			Name string `json:"name"`
			Path string `json:"path"`
		}
		if err := json.Unmarshal(data, &v); err != nil {
			return err
		}
		out.Name = v.Name
		out.MentionPath = v.Path
	default:
		var extra map[string]json.RawMessage
		if err := json.Unmarshal(data, &extra); err != nil {
			return err
		}
		delete(extra, "type")
		out.Extra = extra
	}
	*u = out
	return nil
}

// TextElement marks a UI-defined span within a text buffer. The Rust struct has
// a private `placeholder` field which serde still serializes under the name
// "placeholder".
type TextElement struct {
	ByteRange   ByteRange `json:"byte_range"`
	Placeholder *string   `json:"placeholder"`
}

// NewTextElement constructs a TextElement.
func NewTextElement(byteRange ByteRange, placeholder *string) TextElement {
	return TextElement{ByteRange: byteRange, Placeholder: placeholder}
}

// PlaceholderOrText returns the stored placeholder, falling back to the slice of
// text covered by the byte range when no placeholder is set, mirroring the Rust
// `placeholder(&self, text)` helper.
func (t TextElement) PlaceholderOrText(text string) (string, bool) {
	if t.Placeholder != nil {
		return *t.Placeholder, true
	}
	if t.ByteRange.Start <= t.ByteRange.End && t.ByteRange.End <= len(text) {
		return text[t.ByteRange.Start:t.ByteRange.End], true
	}
	return "", false
}

// ByteRange is a half-open [start, end) byte range into a UTF-8 text buffer.
type ByteRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// Validate reports whether the range is well-formed (start <= end).
func (b ByteRange) Validate() error {
	if b.Start > b.End {
		return fmt.Errorf("byte range start %d exceeds end %d", b.Start, b.End)
	}
	return nil
}
