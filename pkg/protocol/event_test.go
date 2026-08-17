package protocol

import (
	"encoding/json"
	"reflect"
	"testing"
)

// roundTripEvent marshals an Event, unmarshals it back, and re-marshals to
// confirm the JSON is stable across a full encode/decode cycle.
func roundTripEvent(t *testing.T, in Event) []byte {
	t.Helper()
	first, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded Event
	if err := json.Unmarshal(first, &decoded); err != nil {
		t.Fatalf("unmarshal %s: %v", first, err)
	}
	second, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("round-trip mismatch:\n first: %s\nsecond: %s", first, second)
	}
	return first
}

func ptrStr(s string) *string { return &s }
func ptrI64(v int64) *int64   { return &v }

func TestEventEnvelopeRoundTrip(t *testing.T) {
	ev := Event{
		ID: "sub-1",
		Msg: EventMsg{
			Type:         EventMsgKindAgentMessage,
			AgentMessage: &AgentMessageEvent{Message: "hello"},
		},
	}
	got := roundTripEvent(t, ev)

	// The envelope must carry id and msg, and the msg must carry the tag.
	var probe struct {
		ID  string `json:"id"`
		Msg struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"msg"`
	}
	if err := json.Unmarshal(got, &probe); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if probe.ID != "sub-1" {
		t.Errorf("id = %q, want sub-1", probe.ID)
	}
	if probe.Msg.Type != "agent_message" {
		t.Errorf("type = %q, want agent_message", probe.Msg.Type)
	}
	if probe.Msg.Message != "hello" {
		t.Errorf("message = %q, want hello", probe.Msg.Message)
	}
}

func TestTurnStartedEmitsV1Dialect(t *testing.T) {
	ev := Event{
		ID: "x",
		Msg: EventMsg{
			Type: EventMsgKindTurnStarted,
			TurnStarted: &TurnStartedEvent{
				TurnID:             "t1",
				ModelContextWindow: ptrI64(128000),
				CollaborationMode:  ModeKind("default"),
			},
		},
	}
	out, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var probe struct {
		Msg struct {
			Type string `json:"type"`
		} `json:"msg"`
	}
	if err := json.Unmarshal(out, &probe); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if probe.Msg.Type != "task_started" {
		t.Errorf("emitted type = %q, want task_started (v1 dialect)", probe.Msg.Type)
	}
}

func TestTurnCompleteEmitsV1Dialect(t *testing.T) {
	ev := Event{
		ID: "x",
		Msg: EventMsg{
			Type:         EventMsgKindTurnComplete,
			TurnComplete: &TurnCompleteEvent{TurnID: "t1", LastAgentMessage: ptrStr("done")},
		},
	}
	out, _ := json.Marshal(ev)
	var probe struct {
		Msg struct {
			Type string `json:"type"`
		} `json:"msg"`
	}
	if err := json.Unmarshal(out, &probe); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if probe.Msg.Type != "task_complete" {
		t.Errorf("emitted type = %q, want task_complete (v1 dialect)", probe.Msg.Type)
	}
}

func TestTurnStartedAcceptsV2Alias(t *testing.T) {
	// v2 wire: turn_started.
	in := []byte(`{"type":"turn_started","turn_id":"t9","model_context_window":null,"collaboration_mode_kind":"default"}`)
	var msg EventMsg
	if err := json.Unmarshal(in, &msg); err != nil {
		t.Fatalf("unmarshal v2 alias: %v", err)
	}
	if msg.Type != EventMsgKindTurnStarted {
		t.Fatalf("type = %q, want task_started (normalized)", msg.Type)
	}
	if msg.TurnStarted == nil || msg.TurnStarted.TurnID != "t9" {
		t.Fatalf("payload not decoded: %+v", msg.TurnStarted)
	}
	// Re-emit must use the canonical v1 dialect.
	out, _ := json.Marshal(msg)
	var probe struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(out, &probe)
	if probe.Type != "task_started" {
		t.Errorf("re-emitted type = %q, want task_started", probe.Type)
	}
}

func TestTurnCompleteAcceptsV2Alias(t *testing.T) {
	in := []byte(`{"type":"turn_complete","turn_id":"t9","last_agent_message":null}`)
	var msg EventMsg
	if err := json.Unmarshal(in, &msg); err != nil {
		t.Fatalf("unmarshal v2 alias: %v", err)
	}
	if msg.Type != EventMsgKindTurnComplete {
		t.Fatalf("type = %q, want task_complete (normalized)", msg.Type)
	}
	if msg.TurnComplete == nil || msg.TurnComplete.TurnID != "t9" {
		t.Fatalf("payload not decoded: %+v", msg.TurnComplete)
	}
}

func TestShutdownCompleteRoundTrip(t *testing.T) {
	ev := Event{ID: "s", Msg: EventMsg{Type: EventMsgKindShutdownComplete}}
	got := roundTripEvent(t, ev)
	var probe struct {
		Msg map[string]any `json:"msg"`
	}
	if err := json.Unmarshal(got, &probe); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if len(probe.Msg) != 1 || probe.Msg["type"] != "shutdown_complete" {
		t.Errorf("shutdown_complete payload = %v, want only the type tag", probe.Msg)
	}
}

func TestContextCompactedSerializesAsNullPayload(t *testing.T) {
	ev := Event{ID: "c", Msg: EventMsg{Type: EventMsgKindContextCompacted, ContextCompacted: &ContextCompactedEvent{}}}
	roundTripEvent(t, ev)
}

func TestUnknownVariantTolerated(t *testing.T) {
	in := []byte(`{"type":"some_future_event","foo":"bar","n":42}`)
	var msg EventMsg
	if err := json.Unmarshal(in, &msg); err != nil {
		t.Fatalf("unmarshal unknown: %v", err)
	}
	if msg.Type != EventMsgKind("some_future_event") {
		t.Fatalf("type = %q", msg.Type)
	}
	out, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal unknown: %v", err)
	}
	// Must re-emit verbatim.
	var a, b map[string]any
	_ = json.Unmarshal(in, &a)
	_ = json.Unmarshal(out, &b)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("unknown variant not round-tripped:\n in: %s\nout: %s", in, out)
	}
}

func TestCodexErrorInfoUnitVariant(t *testing.T) {
	in := []byte(`{"message":"boom","codex_error_info":"context_window_exceeded"}`)
	var e ErrorEvent
	if err := json.Unmarshal(in, &e); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if e.CodexErrorInfo == nil || e.CodexErrorInfo.Kind != CodexErrorInfoContextWindowExceeded {
		t.Fatalf("kind = %v", e.CodexErrorInfo)
	}
	out, _ := json.Marshal(e)
	if string(out) != string(in) {
		t.Errorf("round-trip: in %s, out %s", in, out)
	}
}

func TestCodexErrorInfoStructVariant(t *testing.T) {
	in := []byte(`{"message":"x","codex_error_info":{"http_connection_failed":{"http_status_code":503}}}`)
	var e ErrorEvent
	if err := json.Unmarshal(in, &e); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if e.CodexErrorInfo == nil || e.CodexErrorInfo.Kind != CodexErrorInfoHTTPConnectionFailed {
		t.Fatalf("kind = %v", e.CodexErrorInfo)
	}
	out, _ := json.Marshal(e)
	if string(out) != string(in) {
		t.Errorf("round-trip: in %s, out %s", in, out)
	}
}

func TestAgentStatusExternalTagging(t *testing.T) {
	cases := []struct {
		name string
		in   string
		kind AgentStatusKind
	}{
		{"unit", `"running"`, AgentStatusRunning},
		{"completed_some", `{"completed":"final"}`, AgentStatusCompleted},
		{"completed_none", `{"completed":null}`, AgentStatusCompleted},
		{"errored", `{"errored":"oops"}`, AgentStatusErrored},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var s AgentStatus
			if err := json.Unmarshal([]byte(c.in), &s); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if s.Kind != c.kind {
				t.Fatalf("kind = %q, want %q", s.Kind, c.kind)
			}
			out, _ := json.Marshal(s)
			if string(out) != c.in {
				t.Errorf("round-trip: in %s, out %s", c.in, out)
			}
		})
	}
}

func TestFileChangeRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"add", `{"type":"add","content":"x"}`},
		{"delete", `{"type":"delete","content":"y"}`},
		{"update_no_move", `{"type":"update","unified_diff":"@@","move_path":null}`},
		{"update_move", `{"type":"update","unified_diff":"@@","move_path":"/new"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var fc FileChange
			if err := json.Unmarshal([]byte(c.in), &fc); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			out, err := json.Marshal(fc)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(out) != c.in {
				t.Errorf("round-trip: in %s, out %s", c.in, out)
			}
		})
	}
}

func TestMcpStartupStatusInternalTag(t *testing.T) {
	in := []byte(`{"error":"nope","state":"failed"}`)
	var s McpStartupStatus
	if err := json.Unmarshal(in, &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s.State != McpStartupStatusStateFailed || s.Error == nil || *s.Error != "nope" {
		t.Fatalf("decoded = %+v", s)
	}
	out, _ := json.Marshal(s)
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	if got["state"] != "failed" || got["error"] != "nope" {
		t.Errorf("re-emit = %s", out)
	}
}

func TestExecCommandOutputDeltaBase64Chunk(t *testing.T) {
	ev := EventMsg{
		Type: EventMsgKindExecCommandOutputDelta,
		ExecCommandOutputDelta: &ExecCommandOutputDeltaEvent{
			CallID: "c1",
			Stream: ExecOutputStreamStdout,
			Chunk:  []byte("hi"),
		},
	}
	out, _ := json.Marshal(ev)
	// "hi" base64 is "aGk=".
	var probe struct {
		Chunk string `json:"chunk"`
	}
	if err := json.Unmarshal(out, &probe); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if probe.Chunk != "aGk=" {
		t.Errorf("chunk = %q, want aGk=", probe.Chunk)
	}
}

func TestPatchApplyEndRoundTrip(t *testing.T) {
	ev := Event{
		ID: "p",
		Msg: EventMsg{
			Type: EventMsgKindPatchApplyEnd,
			PatchApplyEnd: &PatchApplyEndEvent{
				CallID:  "c",
				TurnID:  "t",
				Stdout:  "ok",
				Stderr:  "",
				Success: true,
				Changes: map[string]FileChange{
					"/a": {Kind: FileChangeKindAdd, Content: "z"},
				},
				Status: PatchApplyStatusCompleted,
			},
		},
	}
	roundTripEvent(t, ev)
}

func TestExitedReviewModeRoundTrip(t *testing.T) {
	ev := Event{
		ID: "r",
		Msg: EventMsg{
			Type: EventMsgKindExitedReviewMode,
			ExitedReviewMode: &ExitedReviewModeEvent{
				ReviewOutput: &ReviewOutputEvent{
					Findings:               []ReviewFinding{},
					OverallCorrectness:     "good",
					OverallExplanation:     "lgtm",
					OverallConfidenceScore: 0.9,
				},
			},
		},
	}
	roundTripEvent(t, ev)
}

func TestTokenCountNullsRoundTrip(t *testing.T) {
	ev := Event{
		ID:  "tc",
		Msg: EventMsg{Type: EventMsgKindTokenCount, TokenCount: &TokenCountEvent{}},
	}
	got := roundTripEvent(t, ev)
	var probe struct {
		Msg struct {
			Info       *TokenUsageInfo  `json:"info"`
			RateLimits *json.RawMessage `json:"rate_limits"`
		} `json:"msg"`
	}
	if err := json.Unmarshal(got, &probe); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if probe.Msg.Info != nil {
		t.Errorf("info should be null, got %+v", probe.Msg.Info)
	}
}
