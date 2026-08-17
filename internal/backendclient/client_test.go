package backendclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

type stubAuth struct {
	token string
}

func (s stubAuth) Token() (string, bool) {
	if s.token == "" {
		return "", false
	}
	return s.token, true
}

func TestNormalizeBaseURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want string
	}{
		{"https://chatgpt.com", "https://chatgpt.com/backend-api"},
		{"https://chatgpt.com/", "https://chatgpt.com/backend-api"},
		{"https://chatgpt.com/backend-api", "https://chatgpt.com/backend-api"},
		{"https://chat.openai.com", "https://chat.openai.com/backend-api"},
		{"https://example.test/", "https://example.test"},
		{"https://example.test/api/codex", "https://example.test/api/codex"},
	}
	for _, tt := range tests {
		if got := NormalizeBaseURL(tt.in); got != tt.want {
			t.Errorf("NormalizeBaseURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPathStyleFromBaseURL(t *testing.T) {
	t.Parallel()
	if PathStyleFromBaseURL("https://chatgpt.com/backend-api") != PathStyleChatGptAPI {
		t.Error("backend-api should be ChatGptAPI")
	}
	if PathStyleFromBaseURL("https://example.test") != PathStyleCodexAPI {
		t.Error("non-backend-api should be CodexAPI")
	}
}

func TestClientHeadersAndPaths(t *testing.T) {
	t.Parallel()
	var gotAuth, gotAccount, gotUA, gotPath, gotFedramp string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccount = r.Header.Get("ChatGPT-Account-Id")
		gotUA = r.Header.Get("User-Agent")
		gotFedramp = r.Header.Get("X-OpenAI-Fedramp")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[],"cursor":null}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL).
		WithUserAgent("codex-go/1.0").
		WithAuthSource(stubAuth{token: "tok"}).
		WithChatGptAccountID("acct").
		WithFedrampRoutingHeader().
		WithPathStyle(PathStyleChatGptAPI)

	if _, err := c.ListTasks(context.Background(), nil, nil, nil, nil); err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotAccount != "acct" {
		t.Errorf("ChatGPT-Account-Id = %q", gotAccount)
	}
	if gotUA != "codex-go/1.0" {
		t.Errorf("User-Agent = %q", gotUA)
	}
	if gotFedramp != "true" {
		t.Errorf("X-OpenAI-Fedramp = %q", gotFedramp)
	}
	if gotPath != "/wham/tasks/list" {
		t.Errorf("path = %q, want /wham/tasks/list", gotPath)
	}
}

func TestListTasksQueryParams(t *testing.T) {
	t.Parallel()
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":"T1","title":"Task 1"}],"cursor":"next"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL) // CodexAPI style (no backend-api)
	limit := int32(5)
	filter := "current"
	env := "env-A"
	page, err := c.ListTasks(context.Background(), &limit, &filter, &env, nil)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != "T1" {
		t.Errorf("items = %+v", page.Items)
	}
	if page.Cursor == nil || *page.Cursor != "next" {
		t.Errorf("cursor = %v", page.Cursor)
	}
	for _, want := range []string{"limit=5", "task_filter=current", "environment_id=env-A"} {
		if !contains(gotQuery, want) {
			t.Errorf("query %q missing %q", gotQuery, want)
		}
	}
}

func TestCreateTaskIDExtraction(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		resp string
		want string
		err  bool
	}{
		{name: "nested_task_id", resp: `{"task":{"id":"task-123"}}`, want: "task-123"},
		{name: "top_level_id", resp: `{"id":"task-456"}`, want: "task-456"},
		{name: "no_id", resp: `{"foo":"bar"}`, err: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("method = %s", r.Method)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.resp))
			}))
			defer srv.Close()
			c := NewClient(srv.URL)
			id, err := c.CreateTask(context.Background(), json.RawMessage(`{}`))
			if tt.err {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("CreateTask: %v", err)
			}
			if id != tt.want {
				t.Errorf("id = %q, want %q", id, tt.want)
			}
		})
	}
}

func TestGetConfigRequirementsUnauthorized(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("nope"))
	}))
	defer srv.Close()
	c := NewClient(srv.URL)
	_, reqErr := c.GetConfigRequirementsFile(context.Background())
	if reqErr == nil {
		t.Fatal("expected request error")
	}
	if !reqErr.IsUnauthorized() {
		t.Errorf("IsUnauthorized() = false, status = %d", reqErr.Status())
	}
}

func TestGetRateLimitsMany(t *testing.T) {
	t.Parallel()
	payload := RateLimitStatusPayload{
		PlanType: protocol.PlanTypePro,
		RateLimit: &RateLimitStatusDetails{
			PrimaryWindow: &RateLimitWindowSnapshot{UsedPercent: 42, LimitWindowSeconds: 300, ResetAt: 123},
		},
		AdditionalRateLimits: []AdditionalRateLimitDetails{
			{LimitName: "codex_other", MeteredFeature: "codex_other"},
		},
		Credits:              &CreditStatusDetails{HasCredits: true, Balance: strptr("9.99")},
		RateLimitReachedType: &RateLimitReachedTypePayload{Kind: RateLimitReachedKindWorkspaceMemberCreditsDepleted},
	}
	body, _ := json.Marshal(payload)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	snapshots, err := c.GetRateLimitsMany(context.Background())
	if err != nil {
		t.Fatalf("GetRateLimitsMany: %v", err)
	}
	if len(snapshots) != 2 {
		t.Fatalf("len(snapshots) = %d, want 2", len(snapshots))
	}
	if snapshots[0].LimitID == nil || *snapshots[0].LimitID != "codex" {
		t.Errorf("snapshot[0].LimitID = %v", snapshots[0].LimitID)
	}
	if snapshots[0].Primary == nil || snapshots[0].Primary.UsedPercent != 42.0 {
		t.Errorf("snapshot[0].Primary = %+v", snapshots[0].Primary)
	}
	if snapshots[0].Primary.WindowMinutes == nil || *snapshots[0].Primary.WindowMinutes != 5 {
		t.Errorf("WindowMinutes = %v, want 5", snapshots[0].Primary.WindowMinutes)
	}
	if snapshots[0].RateLimitReachedType == nil ||
		*snapshots[0].RateLimitReachedType != protocol.RateLimitReachedTypeWorkspaceMemberCreditsDepleted {
		t.Errorf("RateLimitReachedType = %v", snapshots[0].RateLimitReachedType)
	}
	if snapshots[1].LimitID == nil || *snapshots[1].LimitID != "codex_other" {
		t.Errorf("snapshot[1].LimitID = %v", snapshots[1].LimitID)
	}
	if snapshots[1].Credits != nil {
		t.Errorf("snapshot[1].Credits = %v, want nil", snapshots[1].Credits)
	}
}

func TestRateLimitReachedTypeMapping(t *testing.T) {
	t.Parallel()
	cases := []struct {
		kind RateLimitReachedKind
		want *protocol.RateLimitReachedType
	}{
		{RateLimitReachedKindRateLimitReached, ptr(protocol.RateLimitReachedTypeRateLimitReached)},
		{RateLimitReachedKindWorkspaceOwnerCreditsDepleted, ptr(protocol.RateLimitReachedTypeWorkspaceOwnerCreditsDepleted)},
		{RateLimitReachedKindUnknown, nil},
	}
	for _, tc := range cases {
		got := mapRateLimitReachedType(tc.kind)
		switch {
		case tc.want == nil && got != nil:
			t.Errorf("kind %q: got %v, want nil", tc.kind, *got)
		case tc.want != nil && (got == nil || *got != *tc.want):
			t.Errorf("kind %q: got %v, want %v", tc.kind, got, *tc.want)
		}
	}
}

func TestWindowMinutesFromSeconds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		secs int32
		want *int64
	}{
		{0, nil},
		{-5, nil},
		{60, ptrI64(1)},
		{61, ptrI64(2)},
		{300, ptrI64(5)},
	}
	for _, tt := range tests {
		got := windowMinutesFromSeconds(tt.secs)
		if tt.want == nil {
			if got != nil {
				t.Errorf("secs=%d got %v, want nil", tt.secs, *got)
			}
			continue
		}
		if got == nil || *got != *tt.want {
			t.Errorf("secs=%d got %v, want %d", tt.secs, got, *tt.want)
		}
	}
}

func strptr(s string) *string { return &s }
func ptr(v protocol.RateLimitReachedType) *protocol.RateLimitReachedType {
	return &v
}
func ptrI64(v int64) *int64 { return &v }
