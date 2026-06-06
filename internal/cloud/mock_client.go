package cloud

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// MockClient is a deterministic in-memory CloudBackend used for tests and the
// CODEXGO_CLOUD_TASKS_MODE=mock path. It mirrors the Rust `MockClient`.
type MockClient struct{}

// compile-time assertion that MockClient satisfies CloudBackend.
var _ CloudBackend = MockClient{}

type mockRow struct {
	id     string
	title  string
	status TaskStatus
}

// ListTasks returns env-varying mock rows, mirroring the Rust mock.
func (MockClient) ListTasks(_ context.Context, env *string, _ *int64, _ *string) (TaskListPage, error) {
	var rows []mockRow
	switch derefOr(env, "") {
	case "env-A":
		rows = []mockRow{{"T-2000", "A: First", TaskStatusReady}}
	case "env-B":
		rows = []mockRow{
			{"T-3000", "B: One", TaskStatusReady},
			{"T-3001", "B: Two", TaskStatusPending},
		}
	default:
		rows = []mockRow{
			{"T-1000", "Update README formatting", TaskStatusReady},
			{"T-1001", "Fix clippy warnings in core", TaskStatusPending},
			{"T-1002", "Add contributing guide", TaskStatusReady},
		}
	}

	var environmentID *string
	if env != nil {
		v := *env
		environmentID = &v
	}
	var environmentLabel *string
	switch {
	case env == nil:
		environmentLabel = strPtr("Global")
	case *env == "env-A":
		environmentLabel = strPtr("Env A")
	case *env == "env-B":
		environmentLabel = strPtr("Env B")
	default:
		environmentLabel = strPtr(*env)
	}

	now := time.Now().UTC()
	out := make([]TaskSummary, 0, len(rows))
	for _, row := range rows {
		id := TaskID(row.id)
		diff := mockDiffFor(id)
		added, removed := countFromUnified(diff)
		attemptTotal := 1
		if row.id == "T-1000" {
			attemptTotal = 2
		}
		out = append(out, TaskSummary{
			ID:               id,
			Title:            row.title,
			Status:           row.status,
			UpdatedAt:        now,
			EnvironmentID:    cloneStrPtr(environmentID),
			EnvironmentLabel: cloneStrPtr(environmentLabel),
			Summary:          DiffSummary{FilesChanged: 1, LinesAdded: added, LinesRemoved: removed},
			IsReview:         false,
			AttemptTotal:     &attemptTotal,
		})
	}
	return TaskListPage{Tasks: out, Cursor: nil}, nil
}

// GetTaskSummary finds a task in the default list, mirroring the Rust mock.
func (m MockClient) GetTaskSummary(ctx context.Context, id TaskID) (TaskSummary, error) {
	page, err := m.ListTasks(ctx, nil, nil, nil)
	if err != nil {
		return TaskSummary{}, err
	}
	for _, t := range page.Tasks {
		if t.ID == id {
			return t, nil
		}
	}
	return TaskSummary{}, newMsgError(fmt.Sprintf("Task %s not found (mock)", id))
}

// GetTaskDiff returns the mock diff for a task.
func (MockClient) GetTaskDiff(_ context.Context, id TaskID) (*string, error) {
	diff := mockDiffFor(id)
	return &diff, nil
}

// GetTaskMessages returns a fixed mock message.
func (MockClient) GetTaskMessages(_ context.Context, _ TaskID) ([]string, error) {
	return []string{"Mock assistant output: this task contains no diff."}, nil
}

// GetTaskText returns fixed mock task text.
func (MockClient) GetTaskText(_ context.Context, _ TaskID) (TaskText, error) {
	prompt := "Why is there no diff?"
	turnID := "mock-turn"
	placement := int64(0)
	return TaskText{
		Prompt:           &prompt,
		Messages:         []string{"Mock assistant output: this task contains no diff."},
		TurnID:           &turnID,
		SiblingTurnIDs:   nil,
		AttemptPlacement: &placement,
		AttemptStatus:    AttemptStatusCompleted,
	}, nil
}

// ApplyTask reports a successful mock apply.
func (MockClient) ApplyTask(_ context.Context, id TaskID, _ *string) (ApplyOutcome, error) {
	return ApplyOutcome{
		Applied:       true,
		Status:        ApplyStatusSuccess,
		Message:       fmt.Sprintf("Applied task %s locally (mock)", id),
		SkippedPaths:  nil,
		ConflictPaths: nil,
	}, nil
}

// ApplyTaskPreflight reports a successful mock preflight.
func (MockClient) ApplyTaskPreflight(_ context.Context, id TaskID, _ *string) (ApplyOutcome, error) {
	return ApplyOutcome{
		Applied:       false,
		Status:        ApplyStatusSuccess,
		Message:       fmt.Sprintf("Preflight passed for task %s (mock)", id),
		SkippedPaths:  nil,
		ConflictPaths: nil,
	}, nil
}

// ListSiblingAttempts returns a single alternate attempt for T-1000.
func (MockClient) ListSiblingAttempts(_ context.Context, task TaskID, _ string) ([]TurnAttempt, error) {
	if task == "T-1000" {
		now := time.Now().UTC()
		placement := int64(1)
		diff := mockDiffFor(task)
		return []TurnAttempt{{
			TurnID:           "T-1000-attempt-2",
			AttemptPlacement: &placement,
			CreatedAt:        &now,
			Status:           AttemptStatusCompleted,
			Diff:             &diff,
			Messages:         []string{"Mock alternate attempt"},
		}}, nil
	}
	return nil, nil
}

// CreateTask returns a synthetic local task id.
func (MockClient) CreateTask(_ context.Context, _ /*envID*/ string, _ /*prompt*/ string, _ /*gitRef*/ string, _ bool, _ int) (CreatedTask, error) {
	id := fmt.Sprintf("task_local_%d", time.Now().UnixMilli())
	return CreatedTask{ID: TaskID(id)}, nil
}

// mockDiffFor returns the canned diff for a mock task, mirroring the Rust
// `mock_diff_for`.
func mockDiffFor(id TaskID) string {
	switch id {
	case "T-1000":
		return "diff --git a/README.md b/README.md\nindex 000000..111111 100644\n--- a/README.md\n+++ b/README.md\n@@ -1,2 +1,3 @@\n Intro\n-Hello\n+Hello, world!\n+Task: T-1000\n"
	case "T-1001":
		return "diff --git a/core/src/lib.rs b/core/src/lib.rs\nindex 000000..111111 100644\n--- a/core/src/lib.rs\n+++ b/core/src/lib.rs\n@@ -1,2 +1,1 @@\n-use foo;\n use bar;\n"
	default:
		return "diff --git a/CONTRIBUTING.md b/CONTRIBUTING.md\nindex 000000..111111 100644\n--- /dev/null\n+++ b/CONTRIBUTING.md\n@@ -0,0 +1,3 @@\n+## Contributing\n+Please open PRs.\n+Thanks!\n"
	}
}

// countFromUnified counts added/removed lines in a unified diff, mirroring the
// Rust `count_from_unified` fallback (the diffy fast-path produces the same
// counts for these well-formed diffs).
func countFromUnified(diff string) (added, removed int) {
	for _, l := range strings.Split(diff, "\n") {
		if strings.HasPrefix(l, "+++") || strings.HasPrefix(l, "---") || strings.HasPrefix(l, "@@") {
			continue
		}
		if len(l) > 0 {
			switch l[0] {
			case '+':
				added++
			case '-':
				removed++
			}
		}
	}
	return added, removed
}

func derefOr(p *string, fallback string) string {
	if p == nil {
		return fallback
	}
	return *p
}

func strPtr(s string) *string { return &s }

func cloneStrPtr(p *string) *string {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
