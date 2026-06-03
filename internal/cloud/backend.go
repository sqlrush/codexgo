package cloud

import (
	"context"
	"os"
	"strings"
)

// DefaultBaseURL is the fallback cloud-tasks base URL when
// CODEX_CLOUD_TASKS_BASE_URL is unset. It matches the reference codex default.
const DefaultBaseURL = "https://chatgpt.com/backend-api"

// EnvBaseURL is the env var used to override the cloud-tasks base URL.
const EnvBaseURL = "CODEX_CLOUD_TASKS_BASE_URL"

// EnvMode is the env var used to select the mock backend (value "mock"/"MOCK").
const EnvMode = "CODEX_CLOUD_TASKS_MODE"

// CloudBackend is the interface implemented by the HTTP and mock backends. It
// mirrors the Rust `CloudBackend` trait. All methods take a context for
// cancellation/timeout.
type CloudBackend interface {
	// ListTasks lists tasks, optionally filtered by environment, with paging.
	ListTasks(ctx context.Context, env *string, limit *int64, cursor *string) (TaskListPage, error)
	// GetTaskSummary fetches the summary for a task.
	GetTaskSummary(ctx context.Context, id TaskID) (TaskSummary, error)
	// GetTaskDiff fetches the unified diff for a task, when available.
	GetTaskDiff(ctx context.Context, id TaskID) (*string, error)
	// GetTaskMessages fetches assistant output messages (no diff).
	GetTaskMessages(ctx context.Context, id TaskID) ([]string, error)
	// GetTaskText fetches the prompt and assistant messages for a task.
	GetTaskText(ctx context.Context, id TaskID) (TaskText, error)
	// ListSiblingAttempts lists best-of-N attempts for an assistant turn.
	ListSiblingAttempts(ctx context.Context, task TaskID, turnID string) ([]TurnAttempt, error)
	// ApplyTaskPreflight validates whether a patch would apply cleanly.
	ApplyTaskPreflight(ctx context.Context, id TaskID, diffOverride *string) (ApplyOutcome, error)
	// ApplyTask applies a task's diff to the working tree.
	ApplyTask(ctx context.Context, id TaskID, diffOverride *string) (ApplyOutcome, error)
	// CreateTask creates a new task and returns its id.
	CreateTask(ctx context.Context, envID, prompt, gitRef string, qaMode bool, bestOfN int) (CreatedTask, error)
}

// BaseURLFromEnv returns the configured cloud-tasks base URL, falling back to
// DefaultBaseURL. It mirrors the reference codex env handling.
func BaseURLFromEnv() string {
	if v, ok := os.LookupEnv(EnvBaseURL); ok && v != "" {
		return v
	}
	return DefaultBaseURL
}

// MockModeEnabled reports whether the mock backend is selected via the env var,
// mirroring the reference `CODEX_CLOUD_TASKS_MODE in {mock, MOCK}` check.
func MockModeEnabled() bool {
	switch os.Getenv(EnvMode) {
	case "mock", "MOCK":
		return true
	default:
		return false
	}
}

// NormalizeBaseURL trims trailing slashes and appends /backend-api for ChatGPT
// hosts when missing. It mirrors the Rust `normalize_base_url`.
func NormalizeBaseURL(input string) string {
	baseURL := strings.TrimRight(input, "/")
	if (strings.HasPrefix(baseURL, "https://chatgpt.com") ||
		strings.HasPrefix(baseURL, "https://chat.openai.com")) &&
		!strings.Contains(baseURL, "/backend-api") {
		baseURL = baseURL + "/backend-api"
	}
	return baseURL
}

// TaskURL constructs a browser-friendly task URL for the given base URL. It
// mirrors the Rust `task_url`.
func TaskURL(baseURL, taskID string) string {
	normalized := NormalizeBaseURL(baseURL)
	if root, ok := strings.CutSuffix(normalized, "/backend-api"); ok {
		return root + "/codex/tasks/" + taskID
	}
	if root, ok := strings.CutSuffix(normalized, "/api/codex"); ok {
		return root + "/codex/tasks/" + taskID
	}
	if strings.HasSuffix(normalized, "/codex") {
		return normalized + "/tasks/" + taskID
	}
	return normalized + "/codex/tasks/" + taskID
}
