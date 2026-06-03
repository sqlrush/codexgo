package modelproviderinfo

import (
	"encoding/json"
	"errors"
	"fmt"
)

// WireApi is the wire protocol that a provider speaks.
//
// Rust: an enum with serde rename_all = "lowercase" whose only variant is
// Responses (the default). Deserialization is hand-written: "responses" maps to
// Responses, "chat" returns a helpful migration error, and any other value is
// an unknown-variant error. It is encoded as a bare JSON string ("responses").
type WireApi string

const (
	// WireApiResponses is the Responses API exposed by OpenAI at /v1/responses.
	// It is the default (and currently only) wire protocol.
	WireApiResponses WireApi = "responses"
)

// DefaultWireApi returns the default wire API (Responses), mirroring the Rust
// #[default] on WireApi::Responses.
func DefaultWireApi() WireApi { return WireApiResponses }

// String implements fmt.Stringer, mirroring the Rust Display impl.
func (w WireApi) String() string {
	switch w {
	case WireApiResponses:
		return "responses"
	default:
		return string(w)
	}
}

// MarshalJSON encodes the wire API as a lowercase JSON string.
func (w WireApi) MarshalJSON() ([]byte, error) {
	return json.Marshal(w.String())
}

// UnmarshalJSON decodes a wire API string, reproducing the Rust custom
// Deserialize: "responses" succeeds, "chat" returns the migration error, and
// any other value is rejected as an unknown variant.
func (w *WireApi) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("decode wire_api: %w", err)
	}
	return w.fromString(value)
}

func (w *WireApi) fromString(value string) error {
	switch value {
	case "responses":
		*w = WireApiResponses
		return nil
	case "chat":
		return errors.New(chatWireAPIRemovedError)
	default:
		return fmt.Errorf("unknown variant `%s`, expected `responses`", value)
	}
}
