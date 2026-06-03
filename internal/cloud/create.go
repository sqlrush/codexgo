package cloud

import (
	"encoding/json"
	"os"
)

// EnvStartingDiff is the env var carrying a pre-apply patch diff included when
// creating a task. It matches the reference codex name.
const EnvStartingDiff = "CODEX_STARTING_DIFF"

// buildCreateTaskBody constructs the create-task request body. It mirrors the
// JSON shape built by the Rust `Tasks::create`, including the optional
// pre_apply_patch (from CODEX_STARTING_DIFF) and best_of_n metadata.
func buildCreateTaskBody(envID, prompt, gitRef string, qaMode bool, bestOfN int) (json.RawMessage, error) {
	inputItems := []any{
		map[string]any{
			"type": "message",
			"role": "user",
			"content": []any{
				map[string]any{"content_type": "text", "text": prompt},
			},
		},
	}

	if diff := os.Getenv(EnvStartingDiff); diff != "" {
		inputItems = append(inputItems, map[string]any{
			"type":        "pre_apply_patch",
			"output_diff": map[string]any{"diff": diff},
		})
	}

	body := map[string]any{
		"new_task": map[string]any{
			"environment_id":             envID,
			"branch":                     gitRef,
			"run_environment_in_qa_mode": qaMode,
		},
		"input_items": inputItems,
	}

	if bestOfN > 1 {
		body["metadata"] = map[string]any{"best_of_n": bestOfN}
	}

	return json.Marshal(body)
}
