// Package imagegen ports the codex ext/image-generation crate: the standalone
// image-generation tool's request shaping and result handling. The OpenAI image
// API request/response shapes are reproduced byte-for-byte from codex_api so the
// wire form matches codex exactly.
package imagegen

import (
	"encoding/json"
	"fmt"
)

// ImageBackground is the requested image background. Rust: ImageBackground
// (rename_all = "lowercase").
type ImageBackground string

// ImageBackground variants.
const (
	ImageBackgroundTransparent ImageBackground = "transparent"
	ImageBackgroundOpaque      ImageBackground = "opaque"
	ImageBackgroundAuto        ImageBackground = "auto"
)

// ImageQuality is the requested image quality. Rust: ImageQuality (rename_all =
// "lowercase").
type ImageQuality string

// ImageQuality variants.
const (
	ImageQualityLow    ImageQuality = "low"
	ImageQualityMedium ImageQuality = "medium"
	ImageQualityHigh   ImageQuality = "high"
	ImageQualityAuto   ImageQuality = "auto"
)

// ImageURL wraps an image data/URL string for an edit request. Rust: ImageUrl.
type ImageURL struct {
	ImageURL string `json:"image_url"`
}

// ImageGenerationRequest is a standalone image generation request. Rust:
// ImageGenerationRequest. background, n, quality, size use
// skip_serializing_if = Option::is_none.
type ImageGenerationRequest struct {
	Prompt     string
	Background *ImageBackground
	Model      string
	N          *uint64
	Quality    *ImageQuality
	Size       *string
}

// MarshalJSON emits the request with the Rust field order and skip rules.
func (r ImageGenerationRequest) MarshalJSON() ([]byte, error) {
	m := newOrderedJSON()
	m.set("prompt", r.Prompt)
	if r.Background != nil {
		m.set("background", *r.Background)
	}
	m.set("model", r.Model)
	if r.N != nil {
		m.set("n", *r.N)
	}
	if r.Quality != nil {
		m.set("quality", *r.Quality)
	}
	if r.Size != nil {
		m.set("size", *r.Size)
	}
	return m.marshal()
}

// ImageEditRequest is a standalone image edit request. Rust: ImageEditRequest.
type ImageEditRequest struct {
	Images     []ImageURL
	Prompt     string
	Background *ImageBackground
	Model      string
	N          *uint64
	Quality    *ImageQuality
	Size       *string
}

// MarshalJSON emits the request with the Rust field order and skip rules.
func (r ImageEditRequest) MarshalJSON() ([]byte, error) {
	m := newOrderedJSON()
	m.set("images", r.Images)
	m.set("prompt", r.Prompt)
	if r.Background != nil {
		m.set("background", *r.Background)
	}
	m.set("model", r.Model)
	if r.N != nil {
		m.set("n", *r.N)
	}
	if r.Quality != nil {
		m.set("quality", *r.Quality)
	}
	if r.Size != nil {
		m.set("size", *r.Size)
	}
	return m.marshal()
}

// ImageData is one returned image. Rust: ImageData.
type ImageData struct {
	B64JSON string `json:"b64_json"`
}

// ImageResponse is the image API response. Rust: ImageResponse; background,
// quality, size are #[serde(default)] (deserialize-optional).
type ImageResponse struct {
	Created    uint64           `json:"created"`
	Data       []ImageData      `json:"data"`
	Background *ImageBackground `json:"background,omitempty"`
	Quality    *ImageQuality    `json:"quality,omitempty"`
	Size       *string          `json:"size,omitempty"`
}

// orderedJSON preserves insertion order for serializing struct-like JSON
// objects, matching serde's field-declaration order.
type orderedJSON struct {
	keys   []string
	values map[string]any
}

func newOrderedJSON() *orderedJSON {
	return &orderedJSON{values: make(map[string]any)}
}

func (o *orderedJSON) set(key string, value any) {
	o.keys = append(o.keys, key)
	o.values[key] = value
}

func (o *orderedJSON) marshal() ([]byte, error) {
	var buf []byte
	buf = append(buf, '{')
	for i, key := range o.keys {
		if i > 0 {
			buf = append(buf, ',')
		}
		keyJSON, err := json.Marshal(key)
		if err != nil {
			return nil, fmt.Errorf("imagegen: marshal key %q: %w", key, err)
		}
		buf = append(buf, keyJSON...)
		buf = append(buf, ':')
		valJSON, err := json.Marshal(o.values[key])
		if err != nil {
			return nil, fmt.Errorf("imagegen: marshal value for %q: %w", key, err)
		}
		buf = append(buf, valJSON...)
	}
	buf = append(buf, '}')
	return buf, nil
}
