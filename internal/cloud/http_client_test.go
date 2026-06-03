package cloud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestHTTPClient returns an HTTPClient pointed at srv using the CodexAPI path
// style (server URL has no /backend-api).
func newTestHTTPClient(url string) *HTTPClient {
	return NewHTTPClient(url)
}

func TestHTTPClientListTasks(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/tasks/list") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"items": [
				{"id":"T1","title":"First","updated_at":1700000000.5,
				 "task_status_display":{"latest_turn_status_display":{"turn_status":"completed","diff_stats":{"files_modified":2,"lines_added":5,"lines_removed":1}}},
				 "pull_requests":[{}]}
			],
			"cursor":"c2"
		}`))
	}))
	defer srv.Close()

	page, err := newTestHTTPClient(srv.URL).ListTasks(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(page.Tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(page.Tasks))
	}
	task := page.Tasks[0]
	if task.ID != "T1" || task.Title != "First" {
		t.Errorf("task id/title = %q/%q", task.ID, task.Title)
	}
	if task.Status != TaskStatusReady {
		t.Errorf("status = %v, want ready", task.Status)
	}
	if task.Summary != (DiffSummary{FilesChanged: 2, LinesAdded: 5, LinesRemoved: 1}) {
		t.Errorf("summary = %+v", task.Summary)
	}
	if !task.IsReview {
		t.Errorf("is_review = false, want true (has pull request)")
	}
	if page.Cursor == nil || *page.Cursor != "c2" {
		t.Errorf("cursor = %v", page.Cursor)
	}
}

func TestHTTPClientSummaryDiffMessagesText(t *testing.T) {
	t.Parallel()
	const details = `{
		"task": {"title":"My Task","updated_at":1700000000.0,"environment_id":"env-9","is_review":false},
		"task_status_display": {"environment_label":"Prod","latest_turn_status_display":{"turn_status":"completed"}},
		"current_user_turn": {"input_items":[{"type":"message","role":"user","content":[{"content_type":"text","text":"the prompt"}]}]},
		"current_diff_task_turn": {"output_items":[{"type":"output_diff","diff":"diff --git a/x b/x\n@@ -1 +1 @@\n-a\n+b\n"}]},
		"current_assistant_turn": {"id":"turn-7","attempt_placement":0,"turn_status":"completed","sibling_turn_ids":["s1"],"output_items":[{"type":"message","content":[{"content_type":"text","text":"assistant says hi"}]}]}
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(details))
	}))
	defer srv.Close()

	c := newTestHTTPClient(srv.URL)
	ctx := context.Background()

	summary, err := c.GetTaskSummary(ctx, "T9")
	if err != nil {
		t.Fatalf("GetTaskSummary: %v", err)
	}
	if summary.Title != "My Task" {
		t.Errorf("title = %q", summary.Title)
	}
	if summary.EnvironmentID == nil || *summary.EnvironmentID != "env-9" {
		t.Errorf("environment_id = %v", summary.EnvironmentID)
	}
	if summary.EnvironmentLabel == nil || *summary.EnvironmentLabel != "Prod" {
		t.Errorf("environment_label = %v", summary.EnvironmentLabel)
	}
	// diff_stats absent -> fall back to counting the diff: 1 file, +1, -1.
	if summary.Summary != (DiffSummary{FilesChanged: 1, LinesAdded: 1, LinesRemoved: 1}) {
		t.Errorf("summary = %+v", summary.Summary)
	}

	diff, err := c.GetTaskDiff(ctx, "T9")
	if err != nil {
		t.Fatalf("GetTaskDiff: %v", err)
	}
	if diff == nil || !strings.Contains(*diff, "diff --git") {
		t.Errorf("diff = %v", diff)
	}

	msgs, err := c.GetTaskMessages(ctx, "T9")
	if err != nil {
		t.Fatalf("GetTaskMessages: %v", err)
	}
	if len(msgs) != 1 || msgs[0] != "assistant says hi" {
		t.Errorf("messages = %v", msgs)
	}

	text, err := c.GetTaskText(ctx, "T9")
	if err != nil {
		t.Fatalf("GetTaskText: %v", err)
	}
	if text.Prompt == nil || *text.Prompt != "the prompt" {
		t.Errorf("prompt = %v", text.Prompt)
	}
	if text.TurnID == nil || *text.TurnID != "turn-7" {
		t.Errorf("turn_id = %v", text.TurnID)
	}
	if len(text.SiblingTurnIDs) != 1 || text.SiblingTurnIDs[0] != "s1" {
		t.Errorf("sibling_turn_ids = %v", text.SiblingTurnIDs)
	}
	if text.AttemptStatus != AttemptStatusCompleted {
		t.Errorf("attempt_status = %v", text.AttemptStatus)
	}
}

func TestHTTPClientCreateTask(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"task":{"id":"created-1"}}`))
	}))
	defer srv.Close()
	t.Setenv(EnvStartingDiff, "")
	created, err := newTestHTTPClient(srv.URL).CreateTask(context.Background(), "env", "prompt", "main", false, 1)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if created.ID != "created-1" {
		t.Errorf("created id = %q", created.ID)
	}
}

func TestHTTPClientListSiblingAttempts(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"sibling_turns": [
				{"id":"t2","attempt_placement":2,"turn_status":"completed","output_items":[{"type":"output_diff","diff":"d2"}]},
				{"id":"t1","attempt_placement":1,"turn_status":"failed","output_items":[{"type":"message","content":[{"content_type":"text","text":"m1"}]}]}
			]
		}`))
	}))
	defer srv.Close()
	attempts, err := newTestHTTPClient(srv.URL).ListSiblingAttempts(context.Background(), "T1", "turn")
	if err != nil {
		t.Fatalf("ListSiblingAttempts: %v", err)
	}
	if len(attempts) != 2 {
		t.Fatalf("attempts = %d, want 2", len(attempts))
	}
	// Sorted by attempt_placement ascending: t1 (1) then t2 (2).
	if attempts[0].TurnID != "t1" || attempts[1].TurnID != "t2" {
		t.Errorf("order = %s,%s want t1,t2", attempts[0].TurnID, attempts[1].TurnID)
	}
	if attempts[1].Diff == nil || *attempts[1].Diff != "d2" {
		t.Errorf("t2 diff = %v", attempts[1].Diff)
	}
	if len(attempts[0].Messages) != 1 || attempts[0].Messages[0] != "m1" {
		t.Errorf("t1 messages = %v", attempts[0].Messages)
	}
}

func TestHTTPClientApplyNonUnifiedDiff(t *testing.T) {
	// Run in a temp dir so the appended error.log does not litter the package.
	t.Chdir(t.TempDir())
	// No server needed: a diff override that isn't a unified diff short-circuits.
	c := NewHTTPClient("https://example.test")
	override := "this is not a diff"
	outcome, err := c.ApplyTaskPreflight(context.Background(), "T1", &override)
	if err != nil {
		t.Fatalf("ApplyTaskPreflight: %v", err)
	}
	if outcome.Status != ApplyStatusError || outcome.Applied {
		t.Errorf("outcome = %+v", outcome)
	}
	if !strings.Contains(outcome.Message, "Expected unified git diff") {
		t.Errorf("message = %q", outcome.Message)
	}
}
