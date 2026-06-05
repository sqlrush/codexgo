package goal

import (
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/internal/tools"
)

// Goal tool names. Mirror the Rust `*_GOAL_TOOL_NAME` constants byte-for-byte.
const (
	GetGoalToolName    = "get_goal"
	CreateGoalToolName = "create_goal"
	UpdateGoalToolName = "update_goal"
)

// createGetGoalTool builds the get_goal tool spec. Mirrors Rust
// `create_get_goal_tool`.
func createGetGoalTool() tools.ToolSpec {
	return tools.ToolSpec{
		Kind: tools.ToolSpecKindFunction,
		Function: &tools.ResponsesApiTool{
			Name:        GetGoalToolName,
			Description: "Get the current goal for this thread, including status, budgets, token and elapsed-time usage, and remaining token budget.",
			Strict:      false,
			Parameters: tools.ObjectSchema(
				map[string]tools.JsonSchema{},
				[]string{},
				tools.BoolAdditionalProperties(false),
			),
		},
	}
}

// createCreateGoalTool builds the create_goal tool spec. Mirrors Rust
// `create_create_goal_tool`.
func createCreateGoalTool() tools.ToolSpec {
	objectiveDesc := "Required. The concrete objective to start pursuing. This starts a new active goal only when no goal is currently defined; if a goal already exists, this tool fails."
	tokenBudgetDesc := "Positive token budget for the new goal. Omit unless explicitly requested."
	properties := map[string]tools.JsonSchema{
		"objective":    tools.StringSchema(&objectiveDesc),
		"token_budget": tools.IntegerSchema(&tokenBudgetDesc),
	}
	description := fmt.Sprintf(
		"Create a goal only when explicitly requested by the user or system/developer instructions; do not infer goals from ordinary tasks.\nSet token_budget only when an explicit token budget is requested. Fails if a goal exists; use %s only for status.",
		UpdateGoalToolName,
	)
	return tools.ToolSpec{
		Kind: tools.ToolSpecKindFunction,
		Function: &tools.ResponsesApiTool{
			Name:        CreateGoalToolName,
			Description: description,
			Strict:      false,
			Parameters: tools.ObjectSchema(
				properties,
				[]string{"objective"},
				tools.BoolAdditionalProperties(false),
			),
		},
	}
}

// createUpdateGoalTool builds the update_goal tool spec. Mirrors Rust
// `create_update_goal_tool`.
func createUpdateGoalTool() tools.ToolSpec {
	statusDesc := "Required. Set to `complete` only when the objective is achieved and no required work remains. Set to `blocked` only after the same blocking condition has recurred for at least three consecutive goal turns and the agent is at an impasse. After a previously blocked goal is resumed, the resumed run starts a fresh blocked audit."
	properties := map[string]tools.JsonSchema{
		"status": tools.StringEnumSchema(
			[]json.RawMessage{json.RawMessage(`"complete"`), json.RawMessage(`"blocked"`)},
			&statusDesc,
		),
	}
	description := "Update the existing goal.\nUse this tool only to mark the goal achieved or genuinely blocked.\nSet status to `complete` only when the objective has actually been achieved and no required work remains.\nSet status to `blocked` only when the same blocking condition has repeated for at least three consecutive goal turns, counting the original/user-triggered turn and any automatic continuations, and the agent cannot make meaningful progress without user input or an external-state change.\nIf the user resumes a goal that was previously marked `blocked`, treat the resumed run as a fresh blocked audit. If the same blocking condition then repeats for at least three consecutive resumed goal turns, set status to `blocked` again.\nOnce the blocked threshold is satisfied, do not keep reporting that you are still blocked while leaving the goal active; set status to `blocked`.\nDo not use `blocked` merely because the work is hard, slow, uncertain, incomplete, or would benefit from clarification.\nDo not mark a goal complete merely because its budget is nearly exhausted or because you are stopping work.\nYou cannot use this tool to pause, resume, budget-limit, or usage-limit a goal; those status changes are controlled by the user or system.\nWhen marking a budgeted goal achieved with status `complete`, report the final token usage from the tool result to the user."
	return tools.ToolSpec{
		Kind: tools.ToolSpecKindFunction,
		Function: &tools.ResponsesApiTool{
			Name:        UpdateGoalToolName,
			Description: description,
			Strict:      false,
			Parameters: tools.ObjectSchema(
				properties,
				[]string{"status"},
				tools.BoolAdditionalProperties(false),
			),
		},
	}
}
