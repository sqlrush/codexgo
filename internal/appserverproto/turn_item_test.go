package appserverproto

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// ptr is a tiny generic helper for building pointer fields in test fixtures.
func ptr[T any](v T) *T { return &v }

// roundTripJSON marshals v, asserts the wire bytes equal wantJSON, then unmarshals
// back into a fresh value of the same dynamic type and asserts it equals v. This
// proves the v2 turn/item types are byte-for-byte faithful and lossless.
func roundTripJSON(t *testing.T, v any, wantJSON string) {
	t.Helper()

	got, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("Marshal(%T) error: %v", v, err)
	}
	if string(got) != wantJSON {
		t.Fatalf("Marshal(%T) =\n  %s\nwant\n  %s", v, got, wantJSON)
	}

	// Decode back into a fresh value of the same type for an equality check.
	fresh := reflect.New(reflect.TypeOf(v))
	if err := json.Unmarshal(got, fresh.Interface()); err != nil {
		t.Fatalf("Unmarshal(%T) error: %v", v, err)
	}
	// A faithful round-trip must re-encode to the same JSON. Comparing structs
	// with reflect.DeepEqual is brittle here because codex emits [] / {} for
	// empty collections, which decode to non-nil empty values that DeepEqual
	// treats as != a nil input.
	back := fresh.Elem().Interface()
	reGot, err := json.Marshal(back)
	if err != nil {
		t.Fatalf("re-Marshal(%T) error: %v", v, err)
	}
	if string(reGot) != wantJSON {
		t.Fatalf("round-trip mismatch for %T:\n got  %s\n want %s", v, reGot, wantJSON)
	}
}

// --- UserInput union ----------------------------------------------------------

func TestUserInputRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		in   UserInput
		json string
	}{
		{
			name: "text empty elements",
			in:   UserInput{Kind: UserInputKindText, Text: "hello"},
			json: `{"type":"text","text":"hello","textElements":[]}`,
		},
		{
			name: "text with elements",
			in: UserInput{
				Kind: UserInputKindText,
				Text: "hi @foo",
				TextElements: []TextElement{
					{ByteRange: ByteRange{Start: 3, End: 7}, Placeholder: ptr("@foo")},
				},
			},
			json: `{"type":"text","text":"hi @foo","textElements":[{"byteRange":{"start":3,"end":7},"placeholder":"@foo"}]}`,
		},
		{
			name: "image with detail",
			in:   UserInput{Kind: UserInputKindImage, Detail: ptr(protocol.ImageDetailHigh), URL: "https://x/y.png"},
			json: `{"type":"image","detail":"high","url":"https://x/y.png"}`,
		},
		{
			name: "image null detail",
			in:   UserInput{Kind: UserInputKindImage, URL: "https://x/y.png"},
			json: `{"type":"image","detail":null,"url":"https://x/y.png"}`,
		},
		{
			name: "local image",
			in:   UserInput{Kind: UserInputKindLocalImage, Detail: ptr(protocol.ImageDetailLow), Path: "/tmp/a.png"},
			json: `{"type":"localImage","detail":"low","path":"/tmp/a.png"}`,
		},
		{
			name: "skill",
			in:   UserInput{Kind: UserInputKindSkill, Name: "build", Path: "/skills/build"},
			json: `{"type":"skill","name":"build","path":"/skills/build"}`,
		},
		{
			name: "mention",
			in:   UserInput{Kind: UserInputKindMention, Name: "file", Path: "src/main.rs"},
			json: `{"type":"mention","name":"file","path":"src/main.rs"}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			roundTripJSON(t, tc.in, tc.json)
		})
	}
}

func TestUserInputUnknownKindErrors(t *testing.T) {
	_, err := json.Marshal(UserInput{Kind: "bogus"})
	if err == nil {
		t.Fatal("expected error marshalling unknown UserInput kind")
	}
	var u UserInput
	if err := json.Unmarshal([]byte(`{"type":"bogus"}`), &u); err == nil {
		t.Fatal("expected error unmarshalling unknown UserInput type tag")
	}
}

// --- Turn request params / responses -----------------------------------------

func TestTurnStartParamsDefaultEmission(t *testing.T) {
	// A default-constructed params (only threadId set) must emit every non-skipped
	// field. input -> [], the option fields -> null (no skip_serializing_if), and
	// serviceTier is omitted entirely (default + double_option + skip).
	p := TurnStartParams{ThreadID: "t1"}
	want := `{"threadId":"t1","clientUserMessageId":null,"input":[],` +
		`"responsesapiClientMetadata":null,"additionalContext":null,"environments":null,` +
		`"cwd":null,"runtimeWorkspaceRoots":null,"approvalPolicy":null,"approvalsReviewer":null,` +
		`"sandboxPolicy":null,"permissions":null,"model":null,"effort":null,"summary":null,` +
		`"personality":null,"outputSchema":null,"collaborationMode":null}`
	got, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	if string(got) != want {
		t.Fatalf("TurnStartParams default =\n  %s\nwant\n  %s", got, want)
	}
}

func TestTurnStartParamsServiceTierThreeStates(t *testing.T) {
	tests := []struct {
		name        string
		serviceTier DoubleOption[string]
		wantHas     bool   // whether serviceTier key should appear
		wantValue   string // expected JSON value for the key when present
	}{
		{name: "absent", serviceTier: DoubleOption[string]{}, wantHas: false},
		{name: "null", serviceTier: NewDoubleOptionNull[string](), wantHas: true, wantValue: "null"},
		{name: "value", serviceTier: NewDoubleOptionValue("flex"), wantHas: true, wantValue: `"flex"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := TurnStartParams{ThreadID: "t1", ServiceTier: tc.serviceTier}
			b, err := json.Marshal(p)
			if err != nil {
				t.Fatalf("Marshal error: %v", err)
			}
			var m map[string]json.RawMessage
			if err := json.Unmarshal(b, &m); err != nil {
				t.Fatalf("Unmarshal into map error: %v", err)
			}
			raw, has := m["serviceTier"]
			if has != tc.wantHas {
				t.Fatalf("serviceTier present=%v want %v (json=%s)", has, tc.wantHas, b)
			}
			if tc.wantHas && string(raw) != tc.wantValue {
				t.Fatalf("serviceTier=%s want %s", raw, tc.wantValue)
			}
		})
	}
}

func TestTurnStartParamsPassthroughFields(t *testing.T) {
	// Cross-area passthrough fields must render their literal JSON, not base64.
	p := TurnStartParams{
		ThreadID:          "t1",
		ApprovalPolicy:    json.RawMessage(`"on-request"`),
		SandboxPolicy:     json.RawMessage(`{"mode":"read-only"}`),
		CollaborationMode: json.RawMessage(`{"id":"pair"}`),
		OutputSchema:      json.RawMessage(`{"type":"object"}`),
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	for k, want := range map[string]string{
		"approvalPolicy":    `"on-request"`,
		"sandboxPolicy":     `{"mode":"read-only"}`,
		"collaborationMode": `{"id":"pair"}`,
		"outputSchema":      `{"type":"object"}`,
	} {
		if string(m[k]) != want {
			t.Fatalf("%s=%s want %s", k, m[k], want)
		}
	}
}

func TestTurnStartParamsRichRoundTrip(t *testing.T) {
	p := TurnStartParams{
		ThreadID:            "t1",
		ClientUserMessageID: ptr("c-1"),
		Input:               []UserInput{{Kind: UserInputKindText, Text: "go"}},
		Effort:              ptr(protocol.ReasoningEffortHigh),
		Summary:             ptr(protocol.ReasoningSummaryConcise),
		Personality:         ptr(protocol.PersonalityFriendly),
		ServiceTier:         NewDoubleOptionValue("flex"),
	}
	// A faithful round-trip must re-encode to the same JSON. Comparing decoded
	// structs with reflect.DeepEqual is brittle here: codex emits [] for the
	// always-array textElements and null for the json.RawMessage passthrough
	// fields, both of which decode to non-nil values that DeepEqual treats as !=
	// the nil inputs. Assert against the exact wire bytes instead.
	want := `{"threadId":"t1","clientUserMessageId":"c-1",` +
		`"input":[{"type":"text","text":"go","textElements":[]}],` +
		`"responsesapiClientMetadata":null,"additionalContext":null,"environments":null,` +
		`"cwd":null,"runtimeWorkspaceRoots":null,"approvalPolicy":null,"approvalsReviewer":null,` +
		`"sandboxPolicy":null,"permissions":null,"model":null,"serviceTier":"flex",` +
		`"effort":"high","summary":"concise","personality":"friendly",` +
		`"outputSchema":null,"collaborationMode":null}`
	roundTripJSON(t, p, want)
}

func TestTurnStartResponseNullTurn(t *testing.T) {
	roundTripJSON(t, TurnStartResponse{}, `{"turn":null}`)
	roundTripJSON(t, TurnStartResponse{Turn: json.RawMessage(`{"id":"u1"}`)}, `{"turn":{"id":"u1"}}`)
}

func TestTurnSteerRoundTrip(t *testing.T) {
	p := TurnSteerParams{
		ThreadID:       "t1",
		Input:          []UserInput{{Kind: UserInputKindText, Text: "more"}},
		ExpectedTurnID: "u1",
	}
	want := `{"threadId":"t1","clientUserMessageId":null,"input":[{"type":"text","text":"more","textElements":[]}],` +
		`"responsesapiClientMetadata":null,"additionalContext":null,"expectedTurnId":"u1"}`
	roundTripJSON(t, p, want)
	roundTripJSON(t, TurnSteerResponse{TurnID: "u9"}, `{"turnId":"u9"}`)
}

func TestTurnInterruptRoundTrip(t *testing.T) {
	roundTripJSON(t, TurnInterruptParams{ThreadID: "t1", TurnID: "u1"}, `{"threadId":"t1","turnId":"u1"}`)
	roundTripJSON(t, TurnInterruptResponse{}, `{}`)
}

// --- Turn notifications -------------------------------------------------------

func TestTurnNotificationsRoundTrip(t *testing.T) {
	roundTripJSON(t, TurnStartedNotification{ThreadID: "t1"}, `{"threadId":"t1","turn":null}`)
	roundTripJSON(t,
		TurnStartedNotification{ThreadID: "t1", Turn: json.RawMessage(`{"id":"u1"}`)},
		`{"threadId":"t1","turn":{"id":"u1"}}`)
	roundTripJSON(t, TurnCompletedNotification{ThreadID: "t1"}, `{"threadId":"t1","turn":null}`)
}

func TestUsageRoundTrip(t *testing.T) {
	roundTripJSON(t,
		Usage{InputTokens: 10, CachedInputTokens: 4, OutputTokens: 20},
		`{"inputTokens":10,"cachedInputTokens":4,"outputTokens":20}`)
}

func TestAdditionalContextEntryRoundTrip(t *testing.T) {
	roundTripJSON(t,
		AdditionalContextEntry{Value: "ctx", Kind: AdditionalContextKindApplication},
		`{"value":"ctx","kind":"application"}`)
}

func TestTurnEnvironmentParamsRoundTrip(t *testing.T) {
	roundTripJSON(t,
		TurnEnvironmentParams{EnvironmentID: "env1", Cwd: protocol.AbsolutePath("/work")},
		`{"environmentId":"env1","cwd":"/work"}`)
}

// --- ThreadItem variants ------------------------------------------------------

func TestThreadItemRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		in   ThreadItem
		json string
	}{
		{
			name: "userMessage",
			in: ThreadItem{Kind: ThreadItemKindUserMessage, UserMessage: &ThreadItemUserMessage{
				ID: "i1", Content: []UserInput{{Kind: UserInputKindText, Text: "hi"}},
			}},
			json: `{"type":"userMessage","id":"i1","clientId":null,"content":[{"type":"text","text":"hi","textElements":[]}]}`,
		},
		{
			name: "hookPrompt",
			in: ThreadItem{Kind: ThreadItemKindHookPrompt, HookPrompt: &ThreadItemHookPrompt{
				ID: "i2", Fragments: []HookPromptFragment{{Text: "x", HookRunID: "r1"}},
			}},
			json: `{"type":"hookPrompt","id":"i2","fragments":[{"text":"x","hookRunId":"r1"}]}`,
		},
		{
			name: "agentMessage minimal",
			in:   ThreadItem{Kind: ThreadItemKindAgentMessage, AgentMessage: &ThreadItemAgentMessage{ID: "i3", Text: "ok"}},
			json: `{"type":"agentMessage","id":"i3","text":"ok","phase":null,"memoryCitation":null}`,
		},
		{
			name: "agentMessage full",
			in: ThreadItem{Kind: ThreadItemKindAgentMessage, AgentMessage: &ThreadItemAgentMessage{
				ID: "i3", Text: "ok", Phase: ptr(protocol.MessagePhaseFinalAnswer),
				MemoryCitation: &MemoryCitation{
					Entries:   []MemoryCitationEntry{{Path: "a", LineStart: 1, LineEnd: 2, Note: "n"}},
					ThreadIDs: []string{"t9"},
				},
			}},
			json: `{"type":"agentMessage","id":"i3","text":"ok","phase":"final_answer","memoryCitation":{"entries":[{"path":"a","lineStart":1,"lineEnd":2,"note":"n"}],"threadIds":["t9"]}}`,
		},
		{
			name: "plan",
			in:   ThreadItem{Kind: ThreadItemKindPlan, Plan: &ThreadItemPlan{ID: "i4", Text: "do x"}},
			json: `{"type":"plan","id":"i4","text":"do x"}`,
		},
		{
			name: "reasoning empty",
			in:   ThreadItem{Kind: ThreadItemKindReasoning, Reasoning: &ThreadItemReasoning{ID: "i5"}},
			json: `{"type":"reasoning","id":"i5","summary":[],"content":[]}`,
		},
		{
			name: "commandExecution",
			in: ThreadItem{Kind: ThreadItemKindCommandExecution, CommandExecution: &ThreadItemCommandExecution{
				ID: "i6", Command: "ls", Cwd: protocol.AbsolutePath("/w"),
				Source: CommandExecutionSourceAgent, Status: CommandExecutionStatusCompleted,
				CommandActions: []CommandAction{{Kind: CommandActionKindUnknown, Command: "ls"}},
				ExitCode:       ptr(int32(0)), DurationMs: ptr(int64(5)),
			}},
			json: `{"type":"commandExecution","id":"i6","command":"ls","cwd":"/w","processId":null,"source":"agent","status":"completed","commandActions":[{"type":"unknown","command":"ls"}],"aggregatedOutput":null,"exitCode":0,"durationMs":5}`,
		},
		{
			name: "fileChange",
			in: ThreadItem{Kind: ThreadItemKindFileChange, FileChange: &ThreadItemFileChange{
				ID: "i7", Status: PatchApplyStatusCompleted,
				Changes: []FileUpdateChange{{Path: "a.go", Kind: PatchChangeKind{Kind: PatchChangeKindAdd}, Diff: "+x"}},
			}},
			json: `{"type":"fileChange","id":"i7","changes":[{"path":"a.go","kind":{"type":"add"},"diff":"+x"}],"status":"completed"}`,
		},
		{
			name: "fileChange update with movePath",
			in: ThreadItem{Kind: ThreadItemKindFileChange, FileChange: &ThreadItemFileChange{
				ID: "i7b", Status: PatchApplyStatusInProgress,
				Changes: []FileUpdateChange{{Path: "b.go", Kind: PatchChangeKind{Kind: PatchChangeKindUpdate, MovePath: ptr("c.go")}, Diff: "d"}},
			}},
			json: `{"type":"fileChange","id":"i7b","changes":[{"path":"b.go","kind":{"type":"update","movePath":"c.go"},"diff":"d"}],"status":"inProgress"}`,
		},
		{
			name: "mcpToolCall",
			in: ThreadItem{Kind: ThreadItemKindMcpToolCall, McpToolCall: &ThreadItemMcpToolCall{
				ID: "i8", Server: "s", Tool: "t", Status: McpToolCallStatusInProgress,
				Arguments: json.RawMessage(`{"a":1}`),
			}},
			json: `{"type":"mcpToolCall","id":"i8","server":"s","tool":"t","status":"inProgress","arguments":{"a":1},"pluginId":null,"result":null,"error":null,"durationMs":null}`,
		},
		{
			name: "dynamicToolCall",
			in: ThreadItem{Kind: ThreadItemKindDynamicToolCall, DynamicToolCall: &ThreadItemDynamicToolCall{
				ID: "i9", Tool: "t", Arguments: json.RawMessage(`{}`), Status: DynamicToolCallStatusCompleted,
				Success: ptr(true),
			}},
			json: `{"type":"dynamicToolCall","id":"i9","namespace":null,"tool":"t","arguments":{},"status":"completed","contentItems":null,"success":true,"durationMs":null}`,
		},
		{
			name: "collabAgentToolCall",
			in: ThreadItem{Kind: ThreadItemKindCollabAgentToolCall, CollabAgentToolCall: &ThreadItemCollabAgentToolCall{
				ID: "i10", Tool: CollabAgentToolSpawnAgent, Status: CollabAgentToolCallStatusInProgress,
				SenderThreadID: "t1",
			}},
			json: `{"type":"collabAgentToolCall","id":"i10","tool":"spawnAgent","status":"inProgress","senderThreadId":"t1","receiverThreadIds":[],"prompt":null,"model":null,"reasoningEffort":null,"agentsStates":{}}`,
		},
		{
			name: "webSearch",
			in: ThreadItem{Kind: ThreadItemKindWebSearch, WebSearch: &ThreadItemWebSearch{
				ID: "i11", Query: "q", Action: &WebSearchAction{Kind: WebSearchActionKindSearch, Query: ptr("q")},
			}},
			json: `{"type":"webSearch","id":"i11","query":"q","action":{"type":"search","query":"q","queries":null}}`,
		},
		{
			name: "imageView",
			in:   ThreadItem{Kind: ThreadItemKindImageView, ImageView: &ThreadItemImageView{ID: "i12", Path: protocol.AbsolutePath("/x.png")}},
			json: `{"type":"imageView","id":"i12","path":"/x.png"}`,
		},
		{
			name: "imageGeneration",
			in: ThreadItem{Kind: ThreadItemKindImageGeneration, ImageGeneration: &ThreadItemImageGeneration{
				ID: "i13", Status: "completed", Result: "data",
			}},
			json: `{"type":"imageGeneration","id":"i13","status":"completed","revisedPrompt":null,"result":"data"}`,
		},
		{
			name: "enteredReviewMode",
			in:   ThreadItem{Kind: ThreadItemKindEnteredReviewMode, EnteredReviewMode: &ThreadItemReviewMode{ID: "i14", Review: "r"}},
			json: `{"type":"enteredReviewMode","id":"i14","review":"r"}`,
		},
		{
			name: "exitedReviewMode",
			in:   ThreadItem{Kind: ThreadItemKindExitedReviewMode, ExitedReviewMode: &ThreadItemReviewMode{ID: "i15", Review: "r"}},
			json: `{"type":"exitedReviewMode","id":"i15","review":"r"}`,
		},
		{
			name: "contextCompaction",
			in:   ThreadItem{Kind: ThreadItemKindContextCompaction, ContextCompaction: &ThreadItemContextCompaction{ID: "i16"}},
			json: `{"type":"contextCompaction","id":"i16"}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			roundTripJSON(t, tc.in, tc.json)
			if got := tc.in.ID(); got == "" {
				t.Fatalf("ID() returned empty for %s", tc.name)
			}
		})
	}
}

func TestThreadItemUnknownKindErrors(t *testing.T) {
	_, err := json.Marshal(ThreadItem{Kind: "bogus"})
	if err == nil {
		t.Fatal("expected error marshalling unknown ThreadItem kind")
	}
	var ti ThreadItem
	if err := json.Unmarshal([]byte(`{"type":"bogus"}`), &ti); err == nil {
		t.Fatal("expected error unmarshalling unknown ThreadItem tag")
	}
}

// --- Nested union helpers -----------------------------------------------------

func TestCommandActionRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		in   CommandAction
		json string
	}{
		{
			name: "read",
			in:   CommandAction{Kind: CommandActionKindRead, Command: "cat a", Name: "a", Path: ptr("/a")},
			json: `{"type":"read","command":"cat a","name":"a","path":"/a"}`,
		},
		{
			name: "listFiles",
			in:   CommandAction{Kind: CommandActionKindListFiles, Command: "ls", Path: ptr("/x")},
			json: `{"type":"listFiles","command":"ls","path":"/x"}`,
		},
		{
			name: "search null fields",
			in:   CommandAction{Kind: CommandActionKindSearch, Command: "grep"},
			json: `{"type":"search","command":"grep","query":null,"path":null}`,
		},
		{
			name: "unknown",
			in:   CommandAction{Kind: CommandActionKindUnknown, Command: "weird"},
			json: `{"type":"unknown","command":"weird"}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			roundTripJSON(t, tc.in, tc.json)
		})
	}
}

func TestWebSearchActionRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		in   WebSearchAction
		json string
	}{
		{
			name: "search",
			in:   WebSearchAction{Kind: WebSearchActionKindSearch, Queries: []string{"a", "b"}},
			json: `{"type":"search","query":null,"queries":["a","b"]}`,
		},
		{
			name: "openPage",
			in:   WebSearchAction{Kind: WebSearchActionKindOpenPage, URL: ptr("http://x")},
			json: `{"type":"openPage","url":"http://x"}`,
		},
		{
			name: "findInPage",
			in:   WebSearchAction{Kind: WebSearchActionKindFindInPage, URL: ptr("http://x"), Pattern: ptr("p")},
			json: `{"type":"findInPage","url":"http://x","pattern":"p"}`,
		},
		{
			name: "other",
			in:   WebSearchAction{Kind: WebSearchActionKindOther},
			json: `{"type":"other"}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			roundTripJSON(t, tc.in, tc.json)
		})
	}
}

func TestWebSearchActionUnknownTagMapsToOther(t *testing.T) {
	var w WebSearchAction
	if err := json.Unmarshal([]byte(`{"type":"futureKind","foo":1}`), &w); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if w.Kind != WebSearchActionKindOther {
		t.Fatalf("unknown tag mapped to %q, want %q", w.Kind, WebSearchActionKindOther)
	}
}

func TestDynamicToolCallOutputContentItemRoundTrip(t *testing.T) {
	roundTripJSON(t,
		DynamicToolCallOutputContentItem{Kind: DynamicToolCallOutputContentItemKindInputText, Text: "hi"},
		`{"type":"inputText","text":"hi"}`)
	roundTripJSON(t,
		DynamicToolCallOutputContentItem{Kind: DynamicToolCallOutputContentItemKindInputImage, ImageURL: "u"},
		`{"type":"inputImage","imageUrl":"u"}`)
}

func TestPatchChangeKindRoundTrip(t *testing.T) {
	roundTripJSON(t, PatchChangeKind{Kind: PatchChangeKindAdd}, `{"type":"add"}`)
	roundTripJSON(t, PatchChangeKind{Kind: PatchChangeKindDelete}, `{"type":"delete"}`)
	roundTripJSON(t, PatchChangeKind{Kind: PatchChangeKindUpdate}, `{"type":"update","movePath":null}`)
	roundTripJSON(t, PatchChangeKind{Kind: PatchChangeKindUpdate, MovePath: ptr("dst")}, `{"type":"update","movePath":"dst"}`)
}

func TestCollabAgentStateRoundTrip(t *testing.T) {
	roundTripJSON(t,
		CollabAgentState{Status: CollabAgentStatusRunning},
		`{"status":"running","message":null}`)
	roundTripJSON(t,
		CollabAgentState{Status: CollabAgentStatusCompleted, Message: ptr("done")},
		`{"status":"completed","message":"done"}`)
}

// --- Item lifecycle notifications --------------------------------------------

func TestItemNotificationsRoundTrip(t *testing.T) {
	item := ThreadItem{Kind: ThreadItemKindAgentMessage, AgentMessage: &ThreadItemAgentMessage{ID: "i1", Text: "hi"}}

	roundTripJSON(t,
		ItemStartedNotification{Item: item, ThreadID: "t1", TurnID: "u1", StartedAtMs: 100},
		`{"item":{"type":"agentMessage","id":"i1","text":"hi","phase":null,"memoryCitation":null},"threadId":"t1","turnId":"u1","startedAtMs":100}`)

	roundTripJSON(t,
		ItemCompletedNotification{Item: item, ThreadID: "t1", TurnID: "u1", CompletedAtMs: 200},
		`{"item":{"type":"agentMessage","id":"i1","text":"hi","phase":null,"memoryCitation":null},"threadId":"t1","turnId":"u1","completedAtMs":200}`)
}

// --- Registry registration ----------------------------------------------------

func TestTurnItemMethodsRegistered(t *testing.T) {
	methods := []struct {
		name  string
		wantP any
		wantR any
	}{
		{"turn/start", &TurnStartParams{}, &TurnStartResponse{}},
		{"turn/steer", &TurnSteerParams{}, &TurnSteerResponse{}},
		{"turn/interrupt", &TurnInterruptParams{}, &TurnInterruptResponse{}},
	}
	for _, m := range methods {
		t.Run(m.name, func(t *testing.T) {
			spec, ok := Lookup(m.name)
			if !ok {
				t.Fatalf("method %q not registered", m.name)
			}
			if got := spec.NewParams(); reflect.TypeOf(got) != reflect.TypeOf(m.wantP) {
				t.Fatalf("%q NewParams = %T, want %T", m.name, got, m.wantP)
			}
			if got := spec.NewResult(); reflect.TypeOf(got) != reflect.TypeOf(m.wantR) {
				t.Fatalf("%q NewResult = %T, want %T", m.name, got, m.wantR)
			}
		})
	}
}

func TestTurnItemNotificationsRegistered(t *testing.T) {
	notes := []struct {
		name  string
		wantP any
	}{
		{"turn/started", &TurnStartedNotification{}},
		{"turn/completed", &TurnCompletedNotification{}},
		{"item/started", &ItemStartedNotification{}},
		{"item/completed", &ItemCompletedNotification{}},
	}
	for _, n := range notes {
		t.Run(n.name, func(t *testing.T) {
			spec, ok := LookupNotification(n.name)
			if !ok {
				t.Fatalf("notification %q not registered", n.name)
			}
			if got := spec.NewParams(); reflect.TypeOf(got) != reflect.TypeOf(n.wantP) {
				t.Fatalf("%q NewParams = %T, want %T", n.name, got, n.wantP)
			}
		})
	}
}

// --- Decode through the registry ---------------------------------------------

func TestDecodeTurnStartThroughRegistry(t *testing.T) {
	req := JSONRPCRequest{
		ID:     NewIntegerRequestId(1),
		Method: "turn/start",
		Params: json.RawMessage(`{"threadId":"t1","input":[{"type":"text","text":"hi"}]}`),
	}
	v, err := DecodeClientRequestParams(req)
	if err != nil {
		t.Fatalf("DecodeClientRequestParams error: %v", err)
	}
	p, ok := v.(*TurnStartParams)
	if !ok {
		t.Fatalf("decoded type = %T, want *TurnStartParams", v)
	}
	if p.ThreadID != "t1" || len(p.Input) != 1 || p.Input[0].Text != "hi" {
		t.Fatalf("decoded params = %#v", p)
	}
}

func TestDecodeItemStartedThroughRegistry(t *testing.T) {
	note := JSONRPCNotification{
		Method: "item/started",
		Params: json.RawMessage(`{"item":{"type":"plan","id":"i1","text":"x"},"threadId":"t1","turnId":"u1","startedAtMs":5}`),
	}
	v, err := DecodeServerNotificationParams(note)
	if err != nil {
		t.Fatalf("DecodeServerNotificationParams error: %v", err)
	}
	n, ok := v.(*ItemStartedNotification)
	if !ok {
		t.Fatalf("decoded type = %T, want *ItemStartedNotification", v)
	}
	if n.Item.Kind != ThreadItemKindPlan || n.Item.Plan == nil || n.Item.Plan.Text != "x" {
		t.Fatalf("decoded notification = %#v", n)
	}
}
