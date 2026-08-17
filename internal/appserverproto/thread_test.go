package appserverproto

import (
	"encoding/json"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// TestThreadMethodsRegistered asserts every thread/* method and notification the
// task ports is present in the registry under the exact wire name.
func TestThreadMethodsRegistered(t *testing.T) {
	wantMethods := []string{
		"thread/start",
		"thread/resume",
		"thread/fork",
		"thread/archive",
		"thread/unarchive",
		"thread/list",
		"thread/read",
		"thread/name/set",
		"thread/turns/list",
		"thread/turns/items/list",
		"thread/inject_items",
		"thread/rollback",
		"thread/settings/update",
		"thread/goal/set",
		"thread/goal/get",
		"thread/goal/clear",
		"thread/metadata/update",
		"thread/memoryMode/set",
		"thread/unsubscribe",
	}
	for _, m := range wantMethods {
		if _, ok := Lookup(m); !ok {
			t.Errorf("client request method %q not registered", m)
		}
	}

	// Spot-check a couple of experimental flags match the Rust attributes.
	if spec, ok := Lookup("thread/settings/update"); !ok || !spec.Experimental {
		t.Errorf("thread/settings/update should be experimental (ok=%v)", ok)
	}
	if spec, ok := Lookup("thread/start"); !ok || spec.Experimental {
		t.Errorf("thread/start should not be experimental (ok=%v)", ok)
	}
}

// TestThreadParamsDecodeFresh confirms each registered thread method decodes its
// params into a freshly constructed, typed pointer of the expected concrete type.
func TestThreadParamsDecodeFresh(t *testing.T) {
	cases := []struct {
		method string
		raw    string
		want   any
	}{
		{"thread/archive", `{"threadId":"t1"}`, new(ThreadArchiveParams)},
		{"thread/read", `{"threadId":"t1","includeTurns":true}`, new(ThreadReadParams)},
		{"thread/name/set", `{"threadId":"t1","name":"My thread"}`, new(ThreadSetNameParams)},
		{"thread/rollback", `{"threadId":"t1","numTurns":2}`, new(ThreadRollbackParams)},
		{"thread/memoryMode/set", `{"threadId":"t1","mode":"enabled"}`, new(ThreadMemoryModeSetParams)},
		{"thread/unsubscribe", `{"threadId":"t1"}`, new(ThreadUnsubscribeParams)},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			req := JSONRPCRequest{
				ID:     NewIntegerRequestId(1),
				Method: tc.method,
				Params: json.RawMessage(tc.raw),
			}
			got, err := DecodeClientRequestParams(req)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			b, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("marshal decoded: %v", err)
			}
			if !jsonEqual(t, b, []byte(tc.raw)) {
				t.Fatalf("round-trip = %s, want %s", b, tc.raw)
			}
		})
	}
}

// TestSimpleThreadParamsRoundTrip exercises straightforward param/response shapes.
func TestSimpleThreadParamsRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		val  any
		want string
	}{
		{
			"archive-params",
			ThreadArchiveParams{ThreadID: "t1"},
			`{"threadId":"t1"}`,
		},
		{
			"set-name-params",
			ThreadSetNameParams{ThreadID: "t1", Name: "hello"},
			`{"threadId":"t1","name":"hello"}`,
		},
		{
			"rollback-params",
			ThreadRollbackParams{ThreadID: "t1", NumTurns: 3},
			`{"threadId":"t1","numTurns":3}`,
		},
		{
			"unsubscribe-response",
			ThreadUnsubscribeResponse{Status: ThreadUnsubscribeStatusUnsubscribed},
			`{"status":"unsubscribed"}`,
		},
		{
			"memory-mode-set-params",
			ThreadMemoryModeSetParams{ThreadID: "t1", Mode: ThreadMemoryModeDisabled},
			`{"threadId":"t1","mode":"disabled"}`,
		},
		{
			"read-params-include-turns-omitted",
			ThreadReadParams{ThreadID: "t1"},
			`{"threadId":"t1"}`,
		},
		{
			"read-params-include-turns-true",
			ThreadReadParams{ThreadID: "t1", IncludeTurns: true},
			`{"threadId":"t1","includeTurns":true}`,
		},
		{
			"empty-responses-archive",
			ThreadArchiveResponse{},
			`{}`,
		},
		{
			"empty-responses-set-name",
			ThreadSetNameResponse{},
			`{}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertJSON(t, tc.val, tc.want)
		})
	}
}

// TestThreadInjectItemsArray confirms `items` always serializes as a JSON array,
// even when nil, matching the Rust Vec<JsonValue> field.
func TestThreadInjectItemsArray(t *testing.T) {
	assertJSON(t, ThreadInjectItemsParams{ThreadID: "t1"}, `{"threadId":"t1","items":[]}`)
	assertJSON(t,
		ThreadInjectItemsParams{ThreadID: "t1", Items: []json.RawMessage{json.RawMessage(`{"a":1}`)}},
		`{"threadId":"t1","items":[{"a":1}]}`,
	)
}

// TestThreadGoalSetTokenBudgetDoubleOption verifies the three-state double-option
// encoding for tokenBudget: absent (omitted), explicit null, and a value.
func TestThreadGoalSetTokenBudgetDoubleOption(t *testing.T) {
	cases := []struct {
		name string
		val  ThreadGoalSetParams
		want string
	}{
		{
			"absent",
			ThreadGoalSetParams{ThreadID: "t1"},
			`{"threadId":"t1","objective":null,"status":null}`,
		},
		{
			"null-clears",
			func() ThreadGoalSetParams {
				d := NewDoubleOptionNull[int64]()
				return ThreadGoalSetParams{ThreadID: "t1", TokenBudget: &d}
			}(),
			`{"threadId":"t1","objective":null,"status":null,"tokenBudget":null}`,
		},
		{
			"value",
			func() ThreadGoalSetParams {
				d := NewDoubleOptionValue[int64](4096)
				return ThreadGoalSetParams{
					ThreadID:    "t1",
					Objective:   strPtr("ship it"),
					Status:      func() *ThreadGoalStatus { s := ThreadGoalStatusActive; return &s }(),
					TokenBudget: &d,
				}
			}(),
			`{"threadId":"t1","objective":"ship it","status":"active","tokenBudget":4096}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertJSON(t, tc.val, tc.want)
		})
	}
}

// TestThreadGoalRoundTrip exercises the ThreadGoal aggregate with its i64 fields
// and the optional tokenBudget pointer.
func TestThreadGoalRoundTrip(t *testing.T) {
	goal := ThreadGoal{
		ThreadID:        "t1",
		Objective:       "do work",
		Status:          ThreadGoalStatusComplete,
		TokenBudget:     i64Ptr(1000),
		TokensUsed:      250,
		TimeUsedSeconds: 42,
		CreatedAt:       100,
		UpdatedAt:       200,
	}
	want := `{"threadId":"t1","objective":"do work","status":"complete","tokenBudget":1000,"tokensUsed":250,"timeUsedSeconds":42,"createdAt":100,"updatedAt":200}`
	assertJSON(t, ThreadGoalSetResponse{Goal: goal}, `{"goal":`+want+`}`)

	// Get response with null goal.
	assertJSON(t, ThreadGoalGetResponse{}, `{"goal":null}`)
	assertJSON(t, ThreadGoalClearResponse{Cleared: true}, `{"cleared":true}`)
}

// TestThreadMetadataGitInfoDoubleOption verifies each git-info field independently
// honors the absent/null/value three-state encoding.
func TestThreadMetadataGitInfoDoubleOption(t *testing.T) {
	sha := NewDoubleOptionValue[string]("abc123")
	branch := NewDoubleOptionNull[string]()
	params := ThreadMetadataUpdateParams{
		ThreadID: "t1",
		GitInfo: &ThreadMetadataGitInfoUpdateParams{
			Sha:    &sha,
			Branch: &branch,
			// OriginURL absent -> omitted entirely.
		},
	}
	assertJSON(t, params,
		`{"threadId":"t1","gitInfo":{"sha":"abc123","branch":null}}`,
	)

	// All absent -> gitInfo is an empty object; gitInfo itself nil -> null.
	assertJSON(t,
		ThreadMetadataUpdateParams{ThreadID: "t1", GitInfo: &ThreadMetadataGitInfoUpdateParams{}},
		`{"threadId":"t1","gitInfo":{}}`,
	)
	assertJSON(t,
		ThreadMetadataUpdateParams{ThreadID: "t1"},
		`{"threadId":"t1","gitInfo":null}`,
	)
}

// TestThreadStartServiceTierDoubleOption verifies the serviceTier double-option
// is omitted when absent, emitted as null when set-null, and as a value otherwise.
func TestThreadStartServiceTierDoubleOption(t *testing.T) {
	// Absent: serviceTier key omitted; other ts-nullable fields rendered as null.
	b, err := json.Marshal(ThreadStartParams{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, present := fields["serviceTier"]; present {
		t.Fatalf("serviceTier should be omitted when absent, got %s", b)
	}
	if got, ok := fields["model"]; !ok || string(got) != "null" {
		t.Fatalf("model should serialize as null when absent, got %s", b)
	}

	// Explicit null.
	dn := NewDoubleOptionNull[string]()
	b2, err := json.Marshal(ThreadStartParams{ServiceTier: &dn})
	if err != nil {
		t.Fatalf("marshal null: %v", err)
	}
	if err := json.Unmarshal(b2, &fields); err != nil {
		t.Fatalf("unmarshal null: %v", err)
	}
	if got, ok := fields["serviceTier"]; !ok || string(got) != "null" {
		t.Fatalf("serviceTier should be null, got %s", b2)
	}

	// Concrete value.
	dv := NewDoubleOptionValue[string]("flex")
	b3, err := json.Marshal(ThreadStartParams{ServiceTier: &dv})
	if err != nil {
		t.Fatalf("marshal value: %v", err)
	}
	if err := json.Unmarshal(b3, &fields); err != nil {
		t.Fatalf("unmarshal value: %v", err)
	}
	if got, ok := fields["serviceTier"]; !ok || string(got) != `"flex"` {
		t.Fatalf("serviceTier should be \"flex\", got %s", b3)
	}
}

// TestThreadResumeForkEmptyPath verifies that an empty `path` string normalizes
// to absent both on decode and on encode for resume and fork params.
func TestThreadResumeForkEmptyPath(t *testing.T) {
	t.Run("resume-decode-empty-path", func(t *testing.T) {
		var p ThreadResumeParams
		if err := json.Unmarshal([]byte(`{"threadId":"t1","path":""}`), &p); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if p.Path != nil {
			t.Fatalf("empty path should decode as nil, got %q", *p.Path)
		}
	})
	t.Run("fork-decode-real-path", func(t *testing.T) {
		var p ThreadForkParams
		if err := json.Unmarshal([]byte(`{"threadId":"t1","path":"/tmp/x.jsonl"}`), &p); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if p.Path == nil || *p.Path != "/tmp/x.jsonl" {
			t.Fatalf("non-empty path should survive, got %v", p.Path)
		}
	})
	t.Run("resume-encode-empty-path-omitted", func(t *testing.T) {
		b, err := json.Marshal(ThreadResumeParams{ThreadID: "t1", Path: strPtr("")})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(b, &fields); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got, ok := fields["path"]; !ok || string(got) != "null" {
			t.Fatalf("empty path should serialize as null, got %s", b)
		}
	})
}

// TestThreadStatusInternalTag exercises the internally-tagged ThreadStatus enum,
// including the Active variant that carries activeFlags.
func TestThreadStatusInternalTag(t *testing.T) {
	cases := []struct {
		name string
		val  ThreadStatus
		want string
	}{
		{"idle", ThreadStatus{Kind: ThreadStatusKindIdle}, `{"type":"idle"}`},
		{"not-loaded", ThreadStatus{Kind: ThreadStatusKindNotLoaded}, `{"type":"notLoaded"}`},
		{"system-error", ThreadStatus{Kind: ThreadStatusKindSystemError}, `{"type":"systemError"}`},
		{
			"active-empty-flags",
			ThreadStatus{Kind: ThreadStatusKindActive},
			`{"type":"active","activeFlags":[]}`,
		},
		{
			"active-with-flags",
			ThreadStatus{
				Kind:        ThreadStatusKindActive,
				ActiveFlags: []ThreadActiveFlag{ThreadActiveFlagWaitingOnApproval, ThreadActiveFlagWaitingOnUserInput},
			},
			`{"type":"active","activeFlags":["waitingOnApproval","waitingOnUserInput"]}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.val)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if !jsonEqual(t, b, []byte(tc.want)) {
				t.Fatalf("marshal = %s, want %s", b, tc.want)
			}
			var back ThreadStatus
			if err := json.Unmarshal(b, &back); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			reb, err := json.Marshal(back)
			if err != nil {
				t.Fatalf("re-marshal: %v", err)
			}
			if !jsonEqual(t, reb, []byte(tc.want)) {
				t.Fatalf("round-trip = %s, want %s", reb, tc.want)
			}
		})
	}
}

// TestThreadListCwdFilterUntagged exercises the untagged string | []string filter.
func TestThreadListCwdFilterUntagged(t *testing.T) {
	cases := []struct {
		name string
		val  ThreadListCwdFilter
		want string
	}{
		{"one", ThreadListCwdFilter{One: strPtr("/repo")}, `"/repo"`},
		{"many", ThreadListCwdFilter{Many: []string{"/a", "/b"}}, `["/a","/b"]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.val)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if !jsonEqual(t, b, []byte(tc.want)) {
				t.Fatalf("marshal = %s, want %s", b, tc.want)
			}
			var back ThreadListCwdFilter
			if err := json.Unmarshal(b, &back); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			reb, err := json.Marshal(back)
			if err != nil {
				t.Fatalf("re-marshal: %v", err)
			}
			if !jsonEqual(t, reb, []byte(tc.want)) {
				t.Fatalf("round-trip = %s, want %s", reb, tc.want)
			}
		})
	}
}

// TestDynamicToolSpecLegacyDerivation verifies deferLoading defaults from the
// legacy exposeToContext field when deferLoading is absent on decode.
func TestDynamicToolSpecLegacyDerivation(t *testing.T) {
	cases := []struct {
		name      string
		raw       string
		wantDefer bool
	}{
		{"explicit-defer-true", `{"name":"t","description":"d","inputSchema":{},"deferLoading":true}`, true},
		{"explicit-defer-false", `{"name":"t","description":"d","inputSchema":{},"deferLoading":false}`, false},
		{"legacy-expose-true", `{"name":"t","description":"d","inputSchema":{},"exposeToContext":true}`, false},
		{"legacy-expose-false", `{"name":"t","description":"d","inputSchema":{},"exposeToContext":false}`, true},
		{"neither", `{"name":"t","description":"d","inputSchema":{}}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var spec DynamicToolSpec
			if err := json.Unmarshal([]byte(tc.raw), &spec); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if spec.DeferLoading != tc.wantDefer {
				t.Fatalf("deferLoading = %v, want %v", spec.DeferLoading, tc.wantDefer)
			}
		})
	}

	// On encode, namespace is omitted when nil and deferLoading omitted when false.
	assertJSON(t,
		DynamicToolSpec{Name: "t", Description: "d", InputSchema: json.RawMessage(`{}`)},
		`{"name":"t","description":"d","inputSchema":{}}`,
	)
	assertJSON(t,
		DynamicToolSpec{Name: "t", Description: "d", InputSchema: json.RawMessage(`{}`), Namespace: strPtr("ns"), DeferLoading: true},
		`{"name":"t","description":"d","inputSchema":{},"namespace":"ns","deferLoading":true}`,
	)
}

// TestThreadListResponseDefaultArrays confirms list-style responses always emit
// `data` as an array, never null.
func TestThreadListResponseDefaultArrays(t *testing.T) {
	assertJSON(t, ThreadListResponse{}, `{"data":[],"nextCursor":null,"backwardsCursor":null}`)
	assertJSON(t, ThreadTurnsListResponse{}, `{"data":[],"nextCursor":null,"backwardsCursor":null}`)
	assertJSON(t, ThreadTurnsItemsListResponse{}, `{"data":[],"nextCursor":null,"backwardsCursor":null}`)
	assertJSON(t, ThreadLoadedListResponse{}, `{"data":[],"nextCursor":null}`)
	assertJSON(t, TurnsPage{}, `{"data":[],"nextCursor":null,"backwardsCursor":null}`)
	assertJSON(t, ThreadSearchResponse{}, `{"data":[],"nextCursor":null,"backwardsCursor":null}`)
}

// TestThreadStartResponseDefaultVecs confirms the #[serde(default)] Vec fields on
// the start response always serialize as arrays.
func TestThreadStartResponseDefaultVecs(t *testing.T) {
	resp := ThreadStartResponse{
		Thread:            json.RawMessage(`{"id":"t1"}`),
		Model:             "gpt",
		ModelProvider:     "openai",
		Cwd:               protocol.AbsolutePath("/repo"),
		ApprovalPolicy:    AskForApprovalV2{Kind: AskForApprovalV2OnRequest},
		ApprovalsReviewer: ApprovalsReviewerV2User,
		Sandbox:           protocol.NewReadOnlySandboxPolicy(false),
	}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"runtimeWorkspaceRoots", "instructionSources"} {
		got, ok := fields[key]
		if !ok || string(got) != "[]" {
			t.Fatalf("%s should serialize as empty array, got %s", key, b)
		}
	}
}

// TestThreadResumeResponseInitialTurnsPageNull confirms the optional inline page
// renders as JSON null when absent (Rust serde(default) Option with no skip).
func TestThreadResumeResponseInitialTurnsPageNull(t *testing.T) {
	resp := ThreadResumeResponse{
		Thread:            json.RawMessage(`{"id":"t1"}`),
		Model:             "gpt",
		ModelProvider:     "openai",
		Cwd:               protocol.AbsolutePath("/repo"),
		ApprovalPolicy:    AskForApprovalV2{Kind: AskForApprovalV2OnRequest},
		ApprovalsReviewer: ApprovalsReviewerV2User,
		Sandbox:           protocol.NewReadOnlySandboxPolicy(false),
	}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got, ok := fields["initialTurnsPage"]; !ok || string(got) != "null" {
		t.Fatalf("initialTurnsPage should be null when absent, got %s", b)
	}
}

// TestThreadEnumWireValues locks the on-wire string for each thread enum value.
func TestThreadEnumWireValues(t *testing.T) {
	check := func(name string, v any, want string) {
		t.Helper()
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("%s marshal: %v", name, err)
		}
		if string(b) != want {
			t.Fatalf("%s = %s, want %s", name, b, want)
		}
	}
	check("start-source-startup", ThreadStartSourceStartup, `"startup"`)
	check("unsub-not-loaded", ThreadUnsubscribeStatusNotLoaded, `"notLoaded"`)
	check("goal-budget-limited", ThreadGoalStatusBudgetLimited, `"budgetLimited"`)
	check("memory-enabled", ThreadMemoryModeEnabled, `"enabled"`)
	check("source-kind-vscode", ThreadSourceKindVsCode, `"vscode"`)
	check("source-kind-app-server", ThreadSourceKindAppServer, `"appServer"`)
	check("sort-key-created", ThreadSortKeyCreatedAt, `"created_at"`)
	check("sort-dir-desc", SortDirectionDesc, `"desc"`)
	check("active-flag-approval", ThreadActiveFlagWaitingOnApproval, `"waitingOnApproval"`)
}

// TestThreadNotificationsRoundTrip exercises the thread notification payloads,
// including the skip-when-absent threadName field.
func TestThreadNotificationsRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		val  any
		want string
	}{
		{"archived", ThreadArchivedNotification{ThreadID: "t1"}, `{"threadId":"t1"}`},
		{"closed", ThreadClosedNotification{ThreadID: "t1"}, `{"threadId":"t1"}`},
		{"goal-cleared", ThreadGoalClearedNotification{ThreadID: "t1"}, `{"threadId":"t1"}`},
		{
			"name-updated-absent",
			ThreadNameUpdatedNotification{ThreadID: "t1"},
			`{"threadId":"t1"}`,
		},
		{
			"name-updated-present",
			ThreadNameUpdatedNotification{ThreadID: "t1", ThreadName: strPtr("renamed")},
			`{"threadId":"t1","threadName":"renamed"}`,
		},
		{
			"status-changed",
			ThreadStatusChangedNotification{ThreadID: "t1", Status: ThreadStatus{Kind: ThreadStatusKindIdle}},
			`{"threadId":"t1","status":{"type":"idle"}}`,
		},
		{
			"token-usage-updated",
			ThreadTokenUsageUpdatedNotification{
				ThreadID: "t1",
				TurnID:   "turn-1",
				TokenUsage: ThreadTokenUsage{
					Total: TokenUsageBreakdown{TotalTokens: 10, InputTokens: 4, CachedInputTokens: 1, OutputTokens: 5, ReasoningOutputTokens: 0},
					Last:  TokenUsageBreakdown{TotalTokens: 2, InputTokens: 1, CachedInputTokens: 0, OutputTokens: 1, ReasoningOutputTokens: 0},
				},
			},
			`{"threadId":"t1","turnId":"turn-1","tokenUsage":{"total":{"totalTokens":10,"inputTokens":4,"cachedInputTokens":1,"outputTokens":5,"reasoningOutputTokens":0},"last":{"totalTokens":2,"inputTokens":1,"cachedInputTokens":0,"outputTokens":1,"reasoningOutputTokens":0},"modelContextWindow":null}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertJSON(t, tc.val, tc.want)
		})
	}
}
