package tools

import (
	_ "embed"
	"encoding/json"
)

// applyPatchLarkGrammar is the lark grammar codex serves for the freeform
// apply_patch tool, embedded byte-for-byte from codex's apply_patch.lark.
//
//go:embed apply_patch.lark
var applyPatchLarkGrammar string

// CreateApplyPatchFreeformTool builds the `apply_patch` ToolSpec as codex does for
// gpt-5.5: a freeform (custom) grammar tool, not a function. Mirrors Rust
// `create_apply_patch_freeform_tool(include_environment_id=false)`.
func CreateApplyPatchFreeformTool() ToolSpec {
	return FreeformToolSpec(FreeformTool{
		Name:        "apply_patch",
		Description: "Use the `apply_patch` tool to edit files. This is a FREEFORM tool, so do not wrap the patch in JSON.",
		Format: FreeformToolFormat{
			Type:       "grammar",
			Syntax:     "lark",
			Definition: applyPatchLarkGrammar,
		},
	})
}

// This file ports the model-visible specs for the always-available built-in tools
// view_image and update_plan from codex-core's tools/handlers/view_image_spec.rs
// and plan_spec.rs. The field set, descriptions, enums, and strict:false are kept
// byte-faithful to codex so the tool the model sees is identical.

// ViewImageToolOptions mirrors the relevant subset of Rust `ViewImageToolOptions`.
type ViewImageToolOptions struct {
	// CanRequestOriginalImageDetail adds the `detail` enum parameter. Every model
	// in the bundled 0.136.0 catalog sets `supports_image_detail_original = true`.
	CanRequestOriginalImageDetail bool
}

// CreateViewImageTool builds the `view_image` ToolSpec, mirroring Rust
// `create_view_image_tool`: a required `path` string plus, when the model supports
// it, an optional `detail` enum (high|original). strict is false.
func CreateViewImageTool(opts ViewImageToolOptions) ToolSpec {
	properties := map[string]JsonSchema{
		"path": StringSchema(strPtr("Local filesystem path to an image file.")),
	}
	if opts.CanRequestOriginalImageDetail {
		properties["detail"] = StringEnumSchema(
			[]json.RawMessage{json.RawMessage(`"high"`), json.RawMessage(`"original"`)},
			strPtr("Image detail level. Defaults to `high`; use `original` to preserve exact resolution."),
		)
	}
	return FunctionToolSpec(ResponsesApiTool{
		Name:        "view_image",
		Description: "View a local image file from the filesystem when visual inspection is needed. Use this for images already available on disk.",
		Strict:      false,
		Parameters: ObjectSchema(
			properties,
			[]string{"path"},
			BoolAdditionalProperties(false),
		),
	})
}

// CreateUpdatePlanTool builds the `update_plan` ToolSpec, mirroring Rust
// `create_plan_tool` (plan_spec.rs): an optional `explanation` string and a
// required `plan` array of {step, status} objects. strict is false.
func CreateUpdatePlanTool() ToolSpec {
	planItem := ObjectSchema(
		map[string]JsonSchema{
			"step": StringSchema(strPtr("Task step text.")),
			"status": StringEnumSchema(
				[]json.RawMessage{
					json.RawMessage(`"pending"`),
					json.RawMessage(`"in_progress"`),
					json.RawMessage(`"completed"`),
				},
				strPtr("Step status."),
			),
		},
		// Declaration order (not sorted): codex emits required as ["step","status"].
		[]string{"step", "status"},
		BoolAdditionalProperties(false),
	)
	return FunctionToolSpec(ResponsesApiTool{
		Name:        "update_plan",
		Description: "Updates the task plan.\nProvide an optional explanation and a list of plan items, each with a step and status.\nAt most one step can be in_progress at a time.\n",
		Strict:      false,
		Parameters: ObjectSchema(
			map[string]JsonSchema{
				"explanation": StringSchema(strPtr("Optional explanation for this plan update.")),
				"plan":        ArraySchema(planItem, strPtr("The list of steps")),
			},
			[]string{"plan"},
			BoolAdditionalProperties(false),
		),
	})
}
