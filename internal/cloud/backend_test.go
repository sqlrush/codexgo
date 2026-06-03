package cloud

import (
	"context"
	"encoding/json"
	"testing"
)

func TestBaseURLFromEnv(t *testing.T) {
	t.Setenv(EnvBaseURL, "")
	if got := BaseURLFromEnv(); got != DefaultBaseURL {
		t.Errorf("BaseURLFromEnv (unset) = %q, want %q", got, DefaultBaseURL)
	}
	t.Setenv(EnvBaseURL, "https://example.test/api/codex")
	if got := BaseURLFromEnv(); got != "https://example.test/api/codex" {
		t.Errorf("BaseURLFromEnv (set) = %q", got)
	}
}

func TestMockModeEnabled(t *testing.T) {
	tests := []struct {
		val  string
		want bool
	}{
		{"mock", true},
		{"MOCK", true},
		{"", false},
		{"real", false},
	}
	for _, tt := range tests {
		t.Setenv(EnvMode, tt.val)
		if got := MockModeEnabled(); got != tt.want {
			t.Errorf("MockModeEnabled(%q) = %v, want %v", tt.val, got, tt.want)
		}
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want string
	}{
		{"https://chatgpt.com", "https://chatgpt.com/backend-api"},
		{"https://chatgpt.com/", "https://chatgpt.com/backend-api"},
		{"https://chatgpt.com/backend-api", "https://chatgpt.com/backend-api"},
		{"https://example.test/", "https://example.test"},
	}
	for _, tt := range tests {
		if got := NormalizeBaseURL(tt.in); got != tt.want {
			t.Errorf("NormalizeBaseURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestTaskURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		base, id, want string
	}{
		{"https://chatgpt.com/backend-api", "T1", "https://chatgpt.com/codex/tasks/T1"},
		{"https://example.test/api/codex", "T2", "https://example.test/codex/tasks/T2"},
		{"https://example.test/codex", "T3", "https://example.test/codex/tasks/T3"},
		{"https://example.test", "T4", "https://example.test/codex/tasks/T4"},
	}
	for _, tt := range tests {
		if got := TaskURL(tt.base, tt.id); got != tt.want {
			t.Errorf("TaskURL(%q,%q) = %q, want %q", tt.base, tt.id, got, tt.want)
		}
	}
}

func TestApplyOutcomeMarshalsEmptySlices(t *testing.T) {
	t.Parallel()
	out := ApplyOutcome{Applied: true, Status: ApplyStatusSuccess, Message: "ok"}
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(decoded["skipped_paths"]) != "[]" {
		t.Errorf("skipped_paths = %s, want []", decoded["skipped_paths"])
	}
	if string(decoded["conflict_paths"]) != "[]" {
		t.Errorf("conflict_paths = %s, want []", decoded["conflict_paths"])
	}
}

func TestCloudTaskErrorMessages(t *testing.T) {
	t.Parallel()
	tests := []struct {
		err  *CloudTaskError
		want string
	}{
		{newUnimplemented("x"), "unimplemented: x"},
		{newHTTPError("boom"), "http error: boom"},
		{newIOError("disk"), "io error: disk"},
		{newMsgError("just a message"), "just a message"},
	}
	for _, tt := range tests {
		if got := tt.err.Error(); got != tt.want {
			t.Errorf("Error() = %q, want %q", got, tt.want)
		}
	}
}

func TestBuildCreateTaskBody(t *testing.T) {
	t.Setenv(EnvStartingDiff, "")
	raw, err := buildCreateTaskBody("env-1", "do the thing", "main", true, 3)
	if err != nil {
		t.Fatalf("buildCreateTaskBody: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	nt, ok := body["new_task"].(map[string]any)
	if !ok {
		t.Fatalf("new_task missing: %v", body)
	}
	if nt["environment_id"] != "env-1" || nt["branch"] != "main" || nt["run_environment_in_qa_mode"] != true {
		t.Errorf("new_task = %+v", nt)
	}
	if _, ok := body["metadata"]; !ok {
		t.Errorf("metadata should be present for best_of_n>1")
	}
	items, ok := body["input_items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("input_items = %v", body["input_items"])
	}

	// With CODEX_STARTING_DIFF set, a pre_apply_patch item is appended.
	t.Setenv(EnvStartingDiff, "diff --git a/x b/x\n")
	raw2, err := buildCreateTaskBody("env-1", "p", "main", false, 1)
	if err != nil {
		t.Fatalf("buildCreateTaskBody (diff): %v", err)
	}
	var body2 map[string]any
	if err := json.Unmarshal(raw2, &body2); err != nil {
		t.Fatalf("unmarshal body2: %v", err)
	}
	items2 := body2["input_items"].([]any)
	if len(items2) != 2 {
		t.Fatalf("expected 2 input items with diff, got %d", len(items2))
	}
	if _, ok := body2["metadata"]; ok {
		t.Errorf("metadata should be absent for best_of_n=1")
	}
}

func TestMockClientListAndApply(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := MockClient{}

	page, err := m.ListTasks(ctx, nil, nil, nil)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(page.Tasks) != 3 {
		t.Fatalf("default rows = %d, want 3", len(page.Tasks))
	}
	if page.Tasks[0].ID != "T-1000" || page.Tasks[0].AttemptTotal == nil || *page.Tasks[0].AttemptTotal != 2 {
		t.Errorf("first task = %+v", page.Tasks[0])
	}

	envA := "env-A"
	pageA, err := m.ListTasks(ctx, &envA, nil, nil)
	if err != nil {
		t.Fatalf("ListTasks env-A: %v", err)
	}
	if len(pageA.Tasks) != 1 || pageA.Tasks[0].ID != "T-2000" {
		t.Errorf("env-A rows = %+v", pageA.Tasks)
	}
	if pageA.Tasks[0].EnvironmentLabel == nil || *pageA.Tasks[0].EnvironmentLabel != "Env A" {
		t.Errorf("env-A label = %v", pageA.Tasks[0].EnvironmentLabel)
	}

	outcome, err := m.ApplyTask(ctx, "T-1000", nil)
	if err != nil {
		t.Fatalf("ApplyTask: %v", err)
	}
	if !outcome.Applied || outcome.Status != ApplyStatusSuccess {
		t.Errorf("apply outcome = %+v", outcome)
	}

	pre, err := m.ApplyTaskPreflight(ctx, "T-1000", nil)
	if err != nil {
		t.Fatalf("ApplyTaskPreflight: %v", err)
	}
	if pre.Applied || pre.Status != ApplyStatusSuccess {
		t.Errorf("preflight outcome = %+v", pre)
	}

	siblings, err := m.ListSiblingAttempts(ctx, "T-1000", "turn")
	if err != nil {
		t.Fatalf("ListSiblingAttempts: %v", err)
	}
	if len(siblings) != 1 || siblings[0].TurnID != "T-1000-attempt-2" {
		t.Errorf("siblings = %+v", siblings)
	}
	none, err := m.ListSiblingAttempts(ctx, "T-1001", "turn")
	if err != nil {
		t.Fatalf("ListSiblingAttempts T-1001: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("expected no siblings for T-1001, got %d", len(none))
	}

	if _, err := m.GetTaskSummary(ctx, "missing"); err == nil {
		t.Errorf("expected error for missing task")
	}
}

func TestCountFromUnified(t *testing.T) {
	t.Parallel()
	added, removed := countFromUnified(mockDiffFor("T-1000"))
	if added != 2 || removed != 1 {
		t.Errorf("counts = (%d,%d), want (2,1)", added, removed)
	}
}
