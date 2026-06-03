package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo/internal/backendclient"
	"github.com/sqlrush/codexgo/internal/login"
)

// HTTPClient is the HTTP-backed CloudBackend implementation. It mirrors the Rust
// `HttpClient`, delegating REST calls to the backend client.
type HTTPClient struct {
	baseURL string
	backend *backendclient.Client
}

// compile-time assertion that HTTPClient satisfies CloudBackend.
var _ CloudBackend = (*HTTPClient)(nil)

// NewHTTPClient builds an HTTP-backed cloud-tasks client for the given base URL.
// It mirrors the Rust `HttpClient::new`.
func NewHTTPClient(baseURL string) *HTTPClient {
	return &HTTPClient{
		baseURL: NormalizeBaseURL(baseURL),
		backend: backendclient.NewClient(baseURL),
	}
}

// NewHTTPClientFromAuth builds an authenticated HTTP-backed client from a
// CodexAuth, mirroring how the reference codex wires auth into the backend: the
// auth provider supplies the bearer token, and the account-id / FedRAMP headers
// are derived from the auth.
func NewHTTPClientFromAuth(baseURL string, auth *login.CodexAuth, userAgent string) *HTTPClient {
	backend := backendclient.FromAuth(baseURL, auth, userAgent)
	if auth != nil {
		if id := auth.GetAccountID(); id != nil && *id != "" {
			backend = backend.WithChatGptAccountID(*id)
		}
		if auth.IsFedrampAccount() {
			backend = backend.WithFedrampRoutingHeader()
		}
	}
	return &HTTPClient{
		baseURL: NormalizeBaseURL(baseURL),
		backend: backend,
	}
}

// WithUserAgent returns a copy with the given User-Agent, mirroring
// `with_user_agent`.
func (c *HTTPClient) WithUserAgent(ua string) *HTTPClient {
	return &HTTPClient{baseURL: c.baseURL, backend: c.backend.WithUserAgent(ua)}
}

// WithAuthSource returns a copy with the given auth source, mirroring
// `with_auth_provider`.
func (c *HTTPClient) WithAuthSource(auth backendclient.AuthSource) *HTTPClient {
	return &HTTPClient{baseURL: c.baseURL, backend: c.backend.WithAuthSource(auth)}
}

// WithChatGptAccountID returns a copy with the given account id header,
// mirroring `with_chatgpt_account_id`.
func (c *HTTPClient) WithChatGptAccountID(id string) *HTTPClient {
	return &HTTPClient{baseURL: c.baseURL, backend: c.backend.WithChatGptAccountID(id)}
}

// Backend exposes the underlying backend client (used by tests).
func (c *HTTPClient) Backend() *backendclient.Client { return c.backend }

// ListTasks lists tasks for an environment with paging.
func (c *HTTPClient) ListTasks(ctx context.Context, env *string, limit *int64, cursor *string) (TaskListPage, error) {
	var limitI32 *int32
	if limit != nil {
		v := int32(*limit)
		if int64(v) == *limit {
			limitI32 = &v
		}
	}
	current := "current"
	resp, err := c.backend.ListTasks(ctx, limitI32, &current, env, cursor)
	if err != nil {
		return TaskListPage{}, newHTTPError(fmt.Sprintf("list_tasks failed: %v", err))
	}
	tasks := make([]TaskSummary, 0, len(resp.Items))
	for _, item := range resp.Items {
		tasks = append(tasks, mapTaskListItemToSummary(item))
	}
	return TaskListPage{Tasks: tasks, Cursor: resp.Cursor}, nil
}

// GetTaskSummary fetches and assembles a task summary.
func (c *HTTPClient) GetTaskSummary(ctx context.Context, id TaskID) (TaskSummary, error) {
	details, body, ct, err := c.backend.GetTaskDetailsWithBody(ctx, string(id))
	if err != nil {
		return TaskSummary{}, newHTTPError(fmt.Sprintf("get_task_details failed: %v", err))
	}
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return TaskSummary{}, newHTTPError(fmt.Sprintf("Decode error for %s: %v; content-type=%s; body=%s", id, err, ct, body))
	}
	taskObj := objectField(parsed, "task")
	if taskObj == nil {
		return TaskSummary{}, newHTTPError(fmt.Sprintf("Task metadata missing from details for %s", id))
	}

	sd := statusDisplay(objectField(parsed, "task_status_display"))
	if sd == nil {
		sd = statusDisplay(objectField(taskObj, "task_status_display"))
	}

	status := mapStatus(sd)
	summary := diffSummaryFromStatusDisplay(sd)
	if summary.FilesChanged == 0 && summary.LinesAdded == 0 && summary.LinesRemoved == 0 {
		if diff, ok := details.UnifiedDiff(); ok {
			summary = diffSummaryFromDiff(diff)
		}
	}

	updatedAtRaw := floatField(taskObj, "updated_at")
	if updatedAtRaw == nil {
		updatedAtRaw = floatField(taskObj, "created_at")
	}
	if updatedAtRaw == nil {
		updatedAtRaw = latestTurnTimestamp(sd)
	}

	environmentID := strFieldOpt(taskObj, "environment_id")

	title := "<untitled>"
	if t := strFieldOpt(taskObj, "title"); t != nil {
		title = *t
	}
	isReview := boolField(taskObj, "is_review")

	return TaskSummary{
		ID:               id,
		Title:            title,
		Status:           status,
		UpdatedAt:        parseUpdatedAt(updatedAtRaw),
		EnvironmentID:    environmentID,
		EnvironmentLabel: envLabelFromStatusDisplay(sd),
		Summary:          summary,
		IsReview:         isReview,
		AttemptTotal:     attemptTotalFromStatusDisplay(sd),
	}, nil
}

// GetTaskDiff fetches the unified diff for a task.
func (c *HTTPClient) GetTaskDiff(ctx context.Context, id TaskID) (*string, error) {
	details, _, _, err := c.backend.GetTaskDetailsWithBody(ctx, string(id))
	if err != nil {
		return nil, newHTTPError(fmt.Sprintf("get_task_details failed: %v", err))
	}
	if diff, ok := details.UnifiedDiff(); ok {
		return &diff, nil
	}
	return nil, nil
}

// GetTaskMessages fetches assistant output messages.
func (c *HTTPClient) GetTaskMessages(ctx context.Context, id TaskID) ([]string, error) {
	details, body, ct, err := c.backend.GetTaskDetailsWithBody(ctx, string(id))
	if err != nil {
		return nil, newHTTPError(fmt.Sprintf("get_task_details failed: %v", err))
	}
	msgs := details.AssistantTextMessages()
	if len(msgs) == 0 {
		msgs = append(msgs, extractAssistantMessagesFromBody(body)...)
	}
	if len(msgs) > 0 {
		return msgs, nil
	}
	if errMsg, ok := details.AssistantErrorMessage(); ok {
		return []string{fmt.Sprintf("Task failed: %s", errMsg)}, nil
	}
	urlStr := detailsPath(c.baseURL, string(id))
	if urlStr == "" {
		urlStr = fmt.Sprintf("%s/api/codex/tasks/%s", c.baseURL, id)
	}
	return nil, newHTTPError(fmt.Sprintf(
		"No assistant text messages in response. GET %s; content-type=%s; body=%s", urlStr, ct, body))
}

// GetTaskText fetches the prompt and assistant messages for a task.
func (c *HTTPClient) GetTaskText(ctx context.Context, id TaskID) (TaskText, error) {
	details, body, _, err := c.backend.GetTaskDetailsWithBody(ctx, string(id))
	if err != nil {
		return TaskText{}, newHTTPError(fmt.Sprintf("get_task_details failed: %v", err))
	}
	var prompt *string
	if p, ok := details.UserTextPrompt(); ok {
		prompt = &p
	}
	messages := details.AssistantTextMessages()
	if len(messages) == 0 {
		messages = append(messages, extractAssistantMessagesFromBody(body)...)
	}
	turn := details.CurrentAssistantTurn
	var turnID *string
	var siblingTurnIDs []string
	var attemptPlacement *int64
	attemptStatus := attemptStatusFromStr("")
	if turn != nil {
		turnID = turn.ID
		siblingTurnIDs = turn.SiblingTurnIDs
		attemptPlacement = turn.AttemptPlacement
		raw := ""
		if turn.TurnStatus != nil {
			raw = *turn.TurnStatus
		}
		attemptStatus = attemptStatusFromStr(raw)
	}
	return TaskText{
		Prompt:           prompt,
		Messages:         messages,
		TurnID:           turnID,
		SiblingTurnIDs:   siblingTurnIDs,
		AttemptPlacement: attemptPlacement,
		AttemptStatus:    attemptStatus,
	}, nil
}

// ListSiblingAttempts lists sibling attempts for a turn.
func (c *HTTPClient) ListSiblingAttempts(ctx context.Context, task TaskID, turnID string) ([]TurnAttempt, error) {
	resp, err := c.backend.ListSiblingTurns(ctx, string(task), turnID)
	if err != nil {
		return nil, newHTTPError(fmt.Sprintf("list_sibling_turns failed: %v", err))
	}
	attempts := make([]TurnAttempt, 0, len(resp.SiblingTurns))
	for _, turn := range resp.SiblingTurns {
		if attempt := turnAttemptFromMap(turn); attempt != nil {
			attempts = append(attempts, *attempt)
		}
	}
	sortAttempts(attempts)
	return attempts, nil
}

// CreateTask creates a new task.
func (c *HTTPClient) CreateTask(ctx context.Context, envID, prompt, gitRef string, qaMode bool, bestOfN int) (CreatedTask, error) {
	requestBody, err := buildCreateTaskBody(envID, prompt, gitRef, qaMode, bestOfN)
	if err != nil {
		return CreatedTask{}, newHTTPError(fmt.Sprintf("create_task failed: %v", err))
	}
	id, err := c.backend.CreateTask(ctx, requestBody)
	if err != nil {
		return CreatedTask{}, newHTTPError(fmt.Sprintf("create_task failed: %v", err))
	}
	return CreatedTask{ID: TaskID(id)}, nil
}

// mapTaskListItemToSummary converts a backend list item, mirroring the Rust
// `map_task_list_item_to_summary`.
func mapTaskListItemToSummary(src backendclient.TaskListItem) TaskSummary {
	sd := statusDisplay(src.TaskStatusDisplay)
	return TaskSummary{
		ID:               TaskID(src.ID),
		Title:            src.Title,
		Status:           mapStatus(sd),
		UpdatedAt:        parseUpdatedAt(src.UpdatedAt),
		EnvironmentID:    nil,
		EnvironmentLabel: envLabelFromStatusDisplay(sd),
		Summary:          diffSummaryFromStatusDisplay(sd),
		IsReview:         len(src.PullRequests) > 0,
		AttemptTotal:     attemptTotalFromStatusDisplay(sd),
	}
}

// detailsPath builds a browser-style details URL, mirroring the Rust
// `details_path` (returns "" when no known prefix matches).
func detailsPath(baseURL, id string) string {
	switch {
	case strings.Contains(baseURL, "/backend-api"):
		return fmt.Sprintf("%s/wham/tasks/%s", baseURL, id)
	case strings.Contains(baseURL, "/api/codex"):
		return fmt.Sprintf("%s/tasks/%s", baseURL, id)
	default:
		return ""
	}
}

func floatField(m map[string]json.RawMessage, key string) *float64 {
	raw, ok := m[key]
	if !ok {
		return nil
	}
	var v float64
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	return &v
}

func boolField(m map[string]json.RawMessage, key string) bool {
	raw, ok := m[key]
	if !ok {
		return false
	}
	var v bool
	if json.Unmarshal(raw, &v) != nil {
		return false
	}
	return v
}

// strFieldOpt returns the field value only when present and a JSON string,
// matching serde's `Value::as_str` semantics (None for missing/non-string).
func strFieldOpt(m map[string]json.RawMessage, key string) *string {
	raw, ok := m[key]
	if !ok {
		return nil
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return nil
	}
	return &s
}
