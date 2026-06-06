package modelproviderinfo

import (
	"encoding/json"
	"fmt"
)

// WireApi is the wire protocol that a provider speaks.
//
// Rust: an enum with serde rename_all = "lowercase" whose only variant is
// Responses (the default) — upstream codex 0.136 REMOVED the chat variant and
// turned "chat" into a migration error. codexgo deliberately diverges here:
// as a separate product targeting non-OpenAI backends (GLM, DeepSeek, …) it
// re-supports the OpenAI chat-completions wire protocol, implemented natively
// in internal/api (chat_completions.go) + internal/core (client_chat.go). See
// the DEVIATIONS.md "wire_api chat" row.
type WireApi string

const (
	// WireApiResponses is the Responses API exposed by OpenAI at /v1/responses.
	// It is the default wire protocol.
	WireApiResponses WireApi = "responses"
	// WireApiChat is the OpenAI-compatible chat-completions API at
	// /chat/completions, spoken by most third-party model vendors.
	WireApiChat WireApi = "chat"
)

// DefaultWireApi returns the default wire API (Responses), mirroring the Rust
// #[default] on WireApi::Responses.
func DefaultWireApi() WireApi { return WireApiResponses }

// String implements fmt.Stringer, mirroring the Rust Display impl.
func (w WireApi) String() string {
	switch w {
	case WireApiResponses:
		return "responses"
	case WireApiChat:
		return "chat"
	default:
		return string(w)
	}
}

// MarshalJSON encodes the wire API as a lowercase JSON string.
func (w WireApi) MarshalJSON() ([]byte, error) {
	return json.Marshal(w.String())
}

// UnmarshalJSON decodes a wire API string: "responses" and "chat" succeed, any
// other value is rejected as an unknown variant.
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
		*w = WireApiChat
		return nil
	default:
		return fmt.Errorf("unknown variant `%s`, expected `responses` or `chat`", value)
	}
}
