package chatgpt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	appserverproto "github.com/sqlrush/codexgo/internal/appserverproto"
	"github.com/sqlrush/codexgo/internal/config"
	"github.com/sqlrush/codexgo/internal/login"
	"github.com/sqlrush/codexgo/internal/protocol"
)

func TestEncodePathSegment(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want string
	}{
		{"account-123_ABC.~", "account-123_ABC.~"},
		{"account/123 with space", "account%2F123%20with%20space"},
	}
	for _, tt := range tests {
		if got := encodePathSegment(tt.in); got != tt.want {
			t.Errorf("encodePathSegment(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestOutputItemUnmarshalAndApply(t *testing.T) {
	t.Parallel()
	const body = `{
		"current_diff_task_turn": {
			"output_items": [
				{"type": "other_thing"},
				{"type": "pr", "output_diff": {"diff": "diff --git a/x b/x\n@@ -1 +1 @@\n-a\n+b\n"}}
			]
		}
	}`
	var resp GetTaskResponse
	if err := unmarshalJSON(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.CurrentDiffTaskTurn == nil {
		t.Fatal("diff turn missing")
	}
	items := resp.CurrentDiffTaskTurn.OutputItems
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	if items[0].PR != nil {
		t.Errorf("non-pr item should have nil PR")
	}
	if items[1].PR == nil || items[1].PR.OutputDiff.Diff == "" {
		t.Errorf("pr item missing diff")
	}
}

func TestApplyDiffFromTaskErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		resp GetTaskResponse
		want string
	}{
		{name: "no_diff_turn", resp: GetTaskResponse{}, want: "No diff turn found"},
		{
			name: "no_pr_item",
			resp: GetTaskResponse{CurrentDiffTaskTurn: &AssistantTurn{OutputItems: []OutputItem{{}}}},
			want: "No PR output item found",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ApplyDiffFromTask(tt.resp, t.TempDir())
			if err == nil || err.Error() != tt.want {
				t.Errorf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestSessionAuthGuards(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Nil auth.
	var out any
	if err := (Session{ChatgptBaseURL: "https://x"}).Get(ctx, "/p", &out); err == nil {
		t.Error("expected error for nil auth")
	}

	// API-key auth does not use the Codex backend.
	apiAuth := login.FromAPIKey("sk-test")
	err := (Session{ChatgptBaseURL: "https://x", Auth: &apiAuth}).Get(ctx, "/p", &out)
	if err == nil {
		t.Error("expected error for non-backend auth")
	}
}

func TestSessionGetSuccess(t *testing.T) {
	t.Parallel()
	var gotAuth, gotSku, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotSku = r.Header.Get(oaiProductSkuHeader)
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"value":"ok"}`))
	}))
	defer srv.Close()

	auth := workspaceAuth(t, protocol.KnownPlanBusiness, "acct-1")
	session := Session{ChatgptBaseURL: srv.URL, Auth: auth, HTTPClient: srv.Client()}
	var out struct {
		Value string `json:"value"`
	}
	if err := session.Get(context.Background(), "/wham/tasks/T1", &out); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if out.Value != "ok" {
		t.Errorf("value = %q", out.Value)
	}
	if gotAuth != "Bearer access-token" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotSku != codexProductSku {
		t.Errorf("OAI-Product-Sku = %q", gotSku)
	}
	if gotPath != "/wham/tasks/T1" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestGetTask(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wham/tasks/T7" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"current_diff_task_turn":{"output_items":[{"type":"pr","output_diff":{"diff":"d"}}]}}`))
	}))
	defer srv.Close()
	auth := workspaceAuth(t, protocol.KnownPlanBusiness, "acct-1")
	resp, err := GetTask(context.Background(), Session{ChatgptBaseURL: srv.URL, Auth: auth, HTTPClient: srv.Client()}, "T7")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if resp.CurrentDiffTaskTurn == nil || len(resp.CurrentDiffTaskTurn.OutputItems) != 1 {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestCodexPluginsEnabledForWorkspace(t *testing.T) {
	t.Parallel()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"beta_settings":{"enable_plugins":false}}`))
	}))
	defer srv.Close()

	auth := workspaceAuth(t, protocol.KnownPlanBusiness, "acct-1")
	session := Session{ChatgptBaseURL: srv.URL, Auth: auth, HTTPClient: srv.Client()}
	cache := NewWorkspaceSettingsCache()

	enabled, err := CodexPluginsEnabledForWorkspace(context.Background(), session, cache)
	if err != nil {
		t.Fatalf("CodexPluginsEnabledForWorkspace: %v", err)
	}
	if enabled {
		t.Errorf("enabled = true, want false")
	}
	// Second call should be served from cache (no extra request).
	if _, err := CodexPluginsEnabledForWorkspace(context.Background(), session, cache); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if calls != 1 {
		t.Errorf("server calls = %d, want 1 (cached)", calls)
	}
}

func TestCodexPluginsEnabledForNonWorkspaceDefaultsTrue(t *testing.T) {
	t.Parallel()
	// Pro plan is not a workspace account -> defaults to true without a request.
	auth := workspaceAuth(t, protocol.KnownPlanPro, "acct-1")
	enabled, err := CodexPluginsEnabledForWorkspace(
		context.Background(),
		Session{ChatgptBaseURL: "https://unused", Auth: auth},
		nil,
	)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !enabled {
		t.Errorf("enabled = false, want true for non-workspace account")
	}
}

// workspaceAuth builds a managed-ChatGPT auth with the given plan + account id.
func workspaceAuth(t *testing.T, plan protocol.KnownPlan, accountID string) *login.CodexAuth {
	t.Helper()
	mode := appserverproto.AuthModeChatgpt
	planType := protocol.KnownAuthPlanType(plan)
	uid := "user-1"
	acct := accountID
	lastRefresh := time.Now().UTC()
	authJSON := &login.AuthDotJson{
		AuthMode: &mode,
		Tokens: &login.TokenData{
			IDToken: login.IdTokenInfo{
				ChatgptPlanType: &planType,
				ChatgptUserID:   &uid,
			},
			AccessToken: "access-token",
			AccountID:   &acct,
		},
		LastRefresh: &lastRefresh,
	}
	auth, err := login.FromAuthDotJson(context.Background(), nil, t.TempDir(), authJSON, config.AuthCredentialsStoreFile, nil)
	if err != nil {
		t.Fatalf("FromAuthDotJson: %v", err)
	}
	return &auth
}

func unmarshalJSON(s string, v any) error {
	return json.Unmarshal([]byte(s), v)
}
