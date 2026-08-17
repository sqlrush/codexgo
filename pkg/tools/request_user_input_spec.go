package tools

import (
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// This file ports the request_user_input tool spec from codex-core's
// tools/handlers/request_user_input_spec.rs. The schema text and structure are
// kept byte-faithful so the advertised spec matches the reference binary.

// RequestUserInputToolName mirrors REQUEST_USER_INPUT_TOOL_NAME.
const RequestUserInputToolName = "request_user_input"

// CreateRequestUserInputTool builds the `request_user_input` ToolSpec with the
// given description. Mirrors Rust `create_request_user_input_tool`: an array of
// 1-3 question objects (id/header/question/options), each option carrying a
// label + description pair.
func CreateRequestUserInputTool(description string) ToolSpec {
	optionProps := map[string]JsonSchema{
		"label": StringSchema(strPtr("User-facing label (1-5 words).")),
		"description": StringSchema(strPtr(
			"One short sentence explaining impact/tradeoff if selected.")),
	}

	optionsSchema := ArraySchema(
		ObjectSchema(
			optionProps,
			[]string{"label", "description"},
			BoolAdditionalProperties(false),
		),
		strPtr(`Provide 2-3 mutually exclusive choices. Put the recommended option first and suffix its label with "(Recommended)". Do not include an "Other" option in this list; the client will add a free-form "Other" option automatically.`),
	)

	questionProps := map[string]JsonSchema{
		"id": StringSchema(strPtr(
			"Stable identifier for mapping answers (snake_case).")),
		"header": StringSchema(strPtr(
			"Short header label shown in the UI (12 or fewer chars).")),
		"question": StringSchema(strPtr(
			"Single-sentence prompt shown to the user.")),
		"options": optionsSchema,
	}

	questionsSchema := ArraySchema(
		ObjectSchema(
			questionProps,
			[]string{"id", "header", "question", "options"},
			BoolAdditionalProperties(false),
		),
		strPtr("Questions to show the user. Prefer 1 and do not exceed 3"),
	)

	properties := map[string]JsonSchema{"questions": questionsSchema}

	return FunctionToolSpec(ResponsesApiTool{
		Name:        RequestUserInputToolName,
		Description: description,
		Strict:      false,
		Parameters: ObjectSchema(
			properties,
			[]string{"questions"},
			BoolAdditionalProperties(false),
		),
	})
}

// RequestUserInputToolDescription renders the tool description for the
// available collaboration modes. Mirrors Rust
// `request_user_input_tool_description`.
func RequestUserInputToolDescription(availableModes []protocol.ModeKind) string {
	return fmt.Sprintf(
		"Request user input for one to three short questions and wait for the response. This tool is only available in %s.",
		formatAllowedModes(availableModes))
}

// formatAllowedModes mirrors Rust `format_allowed_modes`.
func formatAllowedModes(availableModes []protocol.ModeKind) string {
	names := make([]string, 0, len(availableModes))
	for _, mode := range availableModes {
		names = append(names, mode.DisplayName())
	}
	switch len(names) {
	case 0:
		return "no modes"
	case 1:
		return fmt.Sprintf("%s mode", names[0])
	case 2:
		return fmt.Sprintf("%s or %s mode", names[0], names[1])
	default:
		return fmt.Sprintf("modes: %s", strings.Join(names, ","))
	}
}
