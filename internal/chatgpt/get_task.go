package chatgpt

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/sqlrush/codexgo/internal/gitutils"
)

// GetTaskResponse is the relevant subset of a task fetch. It mirrors the Rust
// `GetTaskResponse`.
type GetTaskResponse struct {
	CurrentDiffTaskTurn *AssistantTurn `json:"current_diff_task_turn"`
}

// AssistantTurn holds the output items of a diff turn. It mirrors the Rust
// `AssistantTurn`.
type AssistantTurn struct {
	OutputItems []OutputItem `json:"output_items"`
}

// OutputItem is a tagged union over the "type" field. Only the "pr" variant is
// retained; everything else decodes to the Other form. It mirrors the Rust
// `OutputItem` enum (`#[serde(tag = "type")]` with `#[serde(other)]`).
type OutputItem struct {
	// PR is non-nil when the item type is "pr".
	PR *PrOutputItem
}

// UnmarshalJSON decodes an OutputItem, capturing only the "pr" variant.
func (o *OutputItem) UnmarshalJSON(data []byte) error {
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return fmt.Errorf("output item type: %w", err)
	}
	if probe.Type == "pr" {
		var pr PrOutputItem
		if err := json.Unmarshal(data, &pr); err != nil {
			return fmt.Errorf("pr output item: %w", err)
		}
		o.PR = &pr
		return nil
	}
	o.PR = nil
	return nil
}

// PrOutputItem holds the output diff of a PR output item. It mirrors the Rust
// `PrOutputItem`.
type PrOutputItem struct {
	OutputDiff OutputDiff `json:"output_diff"`
}

// OutputDiff holds the diff string. It mirrors the Rust `OutputDiff`.
type OutputDiff struct {
	Diff string `json:"diff"`
}

// GetTask fetches a task from the ChatGPT backend, mirroring the Rust `get_task`.
func GetTask(ctx context.Context, session Session, taskID string) (GetTaskResponse, error) {
	var resp GetTaskResponse
	path := fmt.Sprintf("/wham/tasks/%s", taskID)
	if err := session.Get(ctx, path, &resp); err != nil {
		return GetTaskResponse{}, err
	}
	return resp, nil
}

// ApplyDiffFromTask applies the PR diff from a task response to cwd. It mirrors
// the Rust `apply_diff_from_task` + `apply_diff`. An empty cwd defaults to the
// current working directory.
func ApplyDiffFromTask(resp GetTaskResponse, cwd string) error {
	if resp.CurrentDiffTaskTurn == nil {
		return fmt.Errorf("No diff turn found")
	}
	var outputDiff *OutputDiff
	for i := range resp.CurrentDiffTaskTurn.OutputItems {
		if pr := resp.CurrentDiffTaskTurn.OutputItems[i].PR; pr != nil {
			outputDiff = &pr.OutputDiff
			break
		}
	}
	if outputDiff == nil {
		return fmt.Errorf("No PR output item found")
	}
	return applyDiff(outputDiff.Diff, cwd)
}

func applyDiff(diff, cwd string) error {
	if cwd == "" {
		wd, err := os.Getwd()
		if err != nil {
			wd = os.TempDir()
		}
		cwd = wd
	}
	res, err := gitutils.ApplyGitPatch(gitutils.ApplyGitRequest{
		Cwd:       cwd,
		Diff:      diff,
		Revert:    false,
		Preflight: false,
	})
	if err != nil {
		return fmt.Errorf("git apply failed to run: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf(
			"Git apply failed (applied=%d, skipped=%d, conflicts=%d)\nstdout:\n%s\nstderr:\n%s",
			len(res.AppliedPaths), len(res.SkippedPaths), len(res.ConflictedPaths), res.Stdout, res.Stderr)
	}
	return nil
}
