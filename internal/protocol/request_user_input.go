package protocol

// This file ports `request_user_input.rs`: the request_user_input tool's
// argument, question, answer, and event types. These structs use the default
// (verbatim) serde field naming except for the explicitly-renamed `isOther` and
// `isSecret` boolean fields.

// RequestUserInputQuestionOption is a selectable option for a question.
type RequestUserInputQuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// RequestUserInputQuestion is a single question posed to the user.
//
// `is_other` / `is_secret` are renamed to "isOther" / "isSecret" on the wire
// and both default to false (`#[serde(default)]`). `options` is omitted when
// absent (`skip_serializing_if = "Option::is_none"`).
type RequestUserInputQuestion struct {
	ID       string                            `json:"id"`
	Header   string                            `json:"header"`
	Question string                            `json:"question"`
	IsOther  bool                              `json:"isOther"`
	IsSecret bool                              `json:"isSecret"`
	Options  *[]RequestUserInputQuestionOption `json:"options,omitempty"`
}

// RequestUserInputArgs is the argument payload for the request_user_input tool.
type RequestUserInputArgs struct {
	Questions []RequestUserInputQuestion `json:"questions"`
}

// RequestUserInputAnswer holds the answers for a single question id.
type RequestUserInputAnswer struct {
	Answers []string `json:"answers"`
}

// RequestUserInputResponse maps question id to its answer.
type RequestUserInputResponse struct {
	Answers map[string]RequestUserInputAnswer `json:"answers"`
}

// RequestUserInputEvent is emitted to request input from the user.
//
// `turn_id` uses `#[serde(default)]` for backwards compatibility (missing keys
// decode to "").
type RequestUserInputEvent struct {
	CallID    string                     `json:"call_id"`
	TurnID    string                     `json:"turn_id"`
	Questions []RequestUserInputQuestion `json:"questions"`
}
