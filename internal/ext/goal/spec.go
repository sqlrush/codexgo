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
	tokenBudgetDesc := "Optional positive token budget for the new active goal."
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
	statusDesc := "Required. Set to complete only when the objective is achieved and no required work remains. Set to blocked only when the goal cannot currently proceed without a user decision, missing dependency, or external unblock."
	properties := map[string]tools.JsonSchema{
		"status": tools.StringEnumSchema(
			[]json.RawMessage{json.RawMessage(`"complete"`), json.RawMessage(`"blocked"`)},
			&statusDesc,
		),
	}
	description := "Update the existing goal.\nUse this tool only to mark the goal achieved or blocked.\nSet status to `complete` only when the objective has actually been achieved and no required work remains.\nSet status to `blocked` only when the goal cannot currently proceed until something external changes.\nDo not mark a goal complete merely because its budget is nearly exhausted or because you are stopping work.\nYou cannot use this tool to pause, resume, or budget-limit a goal; those status changes are controlled by the user or system.\nWhen marking a budgeted goal achieved with status `complete`, report the final token usage from the tool result to the user."
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
