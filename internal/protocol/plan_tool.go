package protocol

// This file ports `plan_tool.rs`: argument types for the update_plan
// todo/checklist tool.

// StepStatus is the status of a single plan step. Rust: snake_case.
type StepStatus string

const (
	StepStatusPending    StepStatus = "pending"
	StepStatusInProgress StepStatus = "in_progress"
	StepStatusCompleted  StepStatus = "completed"
)

// PlanItemArg is one step in an update_plan call.
type PlanItemArg struct {
	Step   string     `json:"step"`
	Status StepStatus `json:"status"`
}

// UpdatePlanArgs is the argument payload for the update_plan tool. `explanation`
// is optional (`#[serde(default)]`, an `Option<String>`).
type UpdatePlanArgs struct {
	Explanation *string       `json:"explanation"`
	Plan        []PlanItemArg `json:"plan"`
}
