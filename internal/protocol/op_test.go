package protocol

import (
	"encoding/json"
	"reflect"
	"testing"
)

// roundTrip marshals v, unmarshals into a fresh value of the same type, and
// re-marshals it; it returns the canonical JSON of the original and of the
// round-tripped value for comparison.
func roundTripJSON[T any](t *testing.T, v T) (string, string) {
	t.Helper()
	first, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded T
	if err := json.Unmarshal(first, &decoded); err != nil {
		t.Fatalf("unmarshal %s: %v", first, err)
	}
	second, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	return string(first), string(second)
}

func assertJSONEqual(t *testing.T, got, want string) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal([]byte(got), &g); err != nil {
		t.Fatalf("unmarshal got %q: %v", got, err)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("unmarshal want %q: %v", want, err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Fatalf("JSON mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestOpUnitVariants(t *testing.T) {
	cases := []struct {
		op   Op
		want string
	}{
		{Op{Type: OpInterrupt}, `{"type":"interrupt"}`},
		{Op{Type: OpCleanBackgroundTerminals}, `{"type":"clean_background_terminals"}`},
		{Op{Type: OpRealtimeConversationClose}, `{"type":"realtime_conversation_close"}`},
		{Op{Type: OpRealtimeConversationListVoices}, `{"type":"realtime_conversation_list_voices"}`},
		{Op{Type: OpReloadUserConfig}, `{"type":"reload_user_config"}`},
		{Op{Type: OpCompact}, `{"type":"compact"}`},
		{Op{Type: OpShutdown}, `{"type":"shutdown"}`},
	}
	for _, c := range cases {
		got, err := json.Marshal(c.op)
		if err != nil {
			t.Fatalf("marshal %s: %v", c.op.Type, err)
		}
		assertJSONEqual(t, string(got), c.want)

		var back Op
		if err := json.Unmarshal([]byte(c.want), &back); err != nil {
			t.Fatalf("unmarshal %s: %v", c.want, err)
		}
		if back.Type != c.op.Type {
			t.Fatalf("type mismatch: got %q want %q", back.Type, c.op.Type)
		}
	}
}

func TestOpUserInputMinimal(t *testing.T) {
	op := Op{
		Type:  OpUserInput,
		Items: []UserInput{{Type: UserInputKindText, Text: "hello"}},
	}
	got, err := json.Marshal(op)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// items always emitted; text_elements always emitted by UserInput marshaler.
	want := `{"type":"user_input","items":[{"type":"text","text":"hello","text_elements":[]}]}`
	assertJSONEqual(t, string(got), want)

	first, second := roundTripJSON(t, op)
	assertJSONEqual(t, second, first)
}

func TestOpUserInputWithThreadSettingsFlatten(t *testing.T) {
	model := "gpt-5"
	op := Op{
		Type:  OpUserInput,
		Items: []UserInput{{Type: UserInputKindText, Text: "hi"}},
		ThreadSettings: ThreadSettingsOverrides{
			Model:  &model,
			Effort: SetReasoningEffort(ReasoningEffortHigh),
		},
	}
	got, err := json.Marshal(op)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// model and effort should be flattened into the op object alongside type/items.
	want := `{"type":"user_input","items":[{"type":"text","text":"hi","text_elements":[]}],"model":"gpt-5","effort":"high"}`
	assertJSONEqual(t, string(got), want)

	first, second := roundTripJSON(t, op)
	assertJSONEqual(t, second, first)
}

func TestThreadSettingsDoubleOptionClear(t *testing.T) {
	// Some(None) => explicit null for both double-option fields.
	o := ThreadSettingsOverrides{
		Effort:      ClearReasoningEffort(),
		ServiceTier: ClearString(),
	}
	got, err := json.Marshal(o)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"effort":null,"service_tier":null}`
	assertJSONEqual(t, string(got), want)

	var back ThreadSettingsOverrides
	if err := json.Unmarshal(got, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !back.Effort.Set || back.Effort.Value != nil {
		t.Fatalf("effort double-option not Some(None): %+v", back.Effort)
	}
	if !back.ServiceTier.Set || back.ServiceTier.Value != nil {
		t.Fatalf("service_tier double-option not Some(None): %+v", back.ServiceTier)
	}
}

func TestThreadSettingsDoubleOptionAbsent(t *testing.T) {
	// Absent outer Option => keys omitted entirely.
	o := ThreadSettingsOverrides{}
	got, err := json.Marshal(o)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	assertJSONEqual(t, string(got), `{}`)
	if !o.isEmpty() {
		t.Fatalf("expected empty overrides to report empty")
	}
}

func TestOpThreadSettingsOnly(t *testing.T) {
	tier := "flex"
	op := Op{
		Type: OpThreadSettings,
		ThreadSettings: ThreadSettingsOverrides{
			ServiceTier: SetString(tier),
		},
	}
	got, err := json.Marshal(op)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"type":"thread_settings","service_tier":"flex"}`
	assertJSONEqual(t, string(got), want)

	first, second := roundTripJSON(t, op)
	assertJSONEqual(t, second, first)
}

func TestOpExecApproval(t *testing.T) {
	turn := "turn-1"
	op := Op{
		Type:     OpExecApproval,
		ID:       "sub-1",
		TurnID:   &turn,
		Decision: &ReviewDecision{Kind: ReviewDecisionApproved},
	}
	got, err := json.Marshal(op)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"type":"exec_approval","id":"sub-1","turn_id":"turn-1","decision":"approved"}`
	assertJSONEqual(t, string(got), want)

	first, second := roundTripJSON(t, op)
	assertJSONEqual(t, second, first)
}

func TestOpPatchApprovalDenied(t *testing.T) {
	op := Op{
		Type:     OpPatchApproval,
		ID:       "sub-2",
		Decision: &ReviewDecision{Kind: ReviewDecisionDenied},
	}
	got, err := json.Marshal(op)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"type":"patch_approval","id":"sub-2","decision":"denied"}`
	assertJSONEqual(t, string(got), want)
}

func TestReviewDecisionUnitVariants(t *testing.T) {
	cases := []struct {
		d    ReviewDecision
		want string
	}{
		{ReviewDecision{Kind: ReviewDecisionApproved}, `"approved"`},
		{ReviewDecision{Kind: ReviewDecisionApprovedForSession}, `"approved_for_session"`},
		{ReviewDecision{Kind: ReviewDecisionDenied}, `"denied"`},
		{ReviewDecision{Kind: ReviewDecisionTimedOut}, `"timed_out"`},
		{ReviewDecision{Kind: ReviewDecisionAbort}, `"abort"`},
	}
	for _, c := range cases {
		got, err := json.Marshal(c.d)
		if err != nil {
			t.Fatalf("marshal %s: %v", c.d.Kind, err)
		}
		if string(got) != c.want {
			t.Fatalf("got %s want %s", got, c.want)
		}
		var back ReviewDecision
		if err := json.Unmarshal([]byte(c.want), &back); err != nil {
			t.Fatalf("unmarshal %s: %v", c.want, err)
		}
		if back.Kind != c.d.Kind {
			t.Fatalf("kind mismatch: got %q want %q", back.Kind, c.d.Kind)
		}
	}
}

func TestReviewDecisionDataVariants(t *testing.T) {
	exec := ReviewDecision{
		Kind:                        ReviewDecisionApprovedExecpolicyAmendment,
		ProposedExecpolicyAmendment: &ExecPolicyAmendment{Command: []string{"git", "status"}},
	}
	got, err := json.Marshal(exec)
	if err != nil {
		t.Fatalf("marshal exec: %v", err)
	}
	want := `{"approved_execpolicy_amendment":{"proposed_execpolicy_amendment":["git","status"]}}`
	assertJSONEqual(t, string(got), want)

	var backExec ReviewDecision
	if err := json.Unmarshal([]byte(want), &backExec); err != nil {
		t.Fatalf("unmarshal exec: %v", err)
	}
	if backExec.Kind != ReviewDecisionApprovedExecpolicyAmendment ||
		backExec.ProposedExecpolicyAmendment == nil ||
		!reflect.DeepEqual(backExec.ProposedExecpolicyAmendment.Command, []string{"git", "status"}) {
		t.Fatalf("exec round-trip mismatch: %+v", backExec)
	}

	net := ReviewDecision{
		Kind:                   ReviewDecisionNetworkPolicyAmendment,
		NetworkPolicyAmendment: &NetworkPolicyAmendment{Host: "example.com", Action: NetworkPolicyRuleActionAllow},
	}
	gotNet, err := json.Marshal(net)
	if err != nil {
		t.Fatalf("marshal net: %v", err)
	}
	wantNet := `{"network_policy_amendment":{"network_policy_amendment":{"host":"example.com","action":"allow"}}}`
	assertJSONEqual(t, string(gotNet), wantNet)
}

func TestOpSetThreadMemoryMode(t *testing.T) {
	op := Op{Type: OpSetThreadMemoryMode, MemoryMode: ThreadMemoryModeDisabled}
	got, err := json.Marshal(op)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"type":"set_thread_memory_mode","mode":"disabled"}`
	assertJSONEqual(t, string(got), want)

	first, second := roundTripJSON(t, op)
	assertJSONEqual(t, second, first)
}

func TestOpThreadRollback(t *testing.T) {
	op := Op{Type: OpThreadRollback, NumTurns: 3}
	got, err := json.Marshal(op)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"type":"thread_rollback","num_turns":3}`
	assertJSONEqual(t, string(got), want)
}

func TestOpRunUserShellCommand(t *testing.T) {
	op := Op{Type: OpRunUserShellCommand, Command: "ls -la"}
	got, err := json.Marshal(op)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"type":"run_user_shell_command","command":"ls -la"}`
	assertJSONEqual(t, string(got), want)

	first, second := roundTripJSON(t, op)
	assertJSONEqual(t, second, first)
}

func TestOpReview(t *testing.T) {
	hint := "look here"
	op := Op{
		Type: OpReview,
		ReviewRequest: &ReviewRequest{
			Target:         ReviewTarget{Type: ReviewTargetBaseBranch, Branch: "main"},
			UserFacingHint: &hint,
		},
	}
	got, err := json.Marshal(op)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"type":"review","review_request":{"target":{"type":"baseBranch","branch":"main"},"user_facing_hint":"look here"}}`
	assertJSONEqual(t, string(got), want)

	first, second := roundTripJSON(t, op)
	assertJSONEqual(t, second, first)
}

func TestReviewTargetCommitAlwaysEmitsTitle(t *testing.T) {
	// Commit.title has no skip_serializing_if; serde emits null when absent.
	target := ReviewTarget{Type: ReviewTargetCommit, SHA: "abc123"}
	got, err := json.Marshal(target)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"type":"commit","sha":"abc123","title":null}`
	assertJSONEqual(t, string(got), want)
}

func TestConversationStartParamsPromptDoubleOption(t *testing.T) {
	// Some(None) => prompt: null.
	p := ConversationStartParams{
		OutputModality: RealtimeOutputModalityText,
		Prompt:         ClearString(),
	}
	got, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"output_modality":"text","prompt":null}`
	assertJSONEqual(t, string(got), want)

	// Absent => prompt omitted.
	p2 := ConversationStartParams{OutputModality: RealtimeOutputModalityAudio}
	got2, err := json.Marshal(p2)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	assertJSONEqual(t, string(got2), `{"output_modality":"audio"}`)

	// Some(Some) => prompt value present, round-trips.
	p3 := ConversationStartParams{
		OutputModality: RealtimeOutputModalityText,
		Prompt:         SetString("be brief"),
	}
	first, second := roundTripJSON(t, p3)
	assertJSONEqual(t, second, first)

	var back ConversationStartParams
	if err := json.Unmarshal([]byte(`{"output_modality":"text","prompt":"be brief"}`), &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !back.Prompt.Set || back.Prompt.Value == nil || *back.Prompt.Value != "be brief" {
		t.Fatalf("prompt double-option mismatch: %+v", back.Prompt)
	}
}

func TestOpRealtimeConversationStartFlatten(t *testing.T) {
	op := Op{
		Type: OpRealtimeConversationStart,
		RealtimeConversationStartParams: &ConversationStartParams{
			OutputModality: RealtimeOutputModalityText,
		},
	}
	got, err := json.Marshal(op)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// The ConversationStartParams fields are flattened alongside "type".
	want := `{"type":"realtime_conversation_start","output_modality":"text"}`
	assertJSONEqual(t, string(got), want)

	first, second := roundTripJSON(t, op)
	assertJSONEqual(t, second, first)
}

func TestOpRealtimeConversationText(t *testing.T) {
	op := Op{
		Type:                           OpRealtimeConversationText,
		RealtimeConversationTextParams: &ConversationTextParams{Text: "hi"},
	}
	got, err := json.Marshal(op)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"type":"realtime_conversation_text","text":"hi"}`
	assertJSONEqual(t, string(got), want)
}

func TestOpUserInputAnswerAlias(t *testing.T) {
	// The deserialize alias "request_user_input_response" must map to the
	// UserInputAnswer variant, and re-marshal to the canonical "user_input_answer".
	in := `{"type":"request_user_input_response","id":"q1","response":{"answers":{}}}`
	var op Op
	if err := json.Unmarshal([]byte(in), &op); err != nil {
		t.Fatalf("unmarshal alias: %v", err)
	}
	if op.Type != OpUserInputAnswer {
		t.Fatalf("expected user_input_answer, got %q", op.Type)
	}
	got, err := json.Marshal(op)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(got, &m); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if m["type"] != "user_input_answer" {
		t.Fatalf("expected canonical type user_input_answer, got %v", m["type"])
	}
}

func TestOpUnknownVariantPreserved(t *testing.T) {
	// Forward compatibility: an unknown variant decodes into Extra and
	// re-marshals to the same shape.
	in := `{"type":"some_future_op","payload":{"x":1},"flag":true}`
	var op Op
	if err := json.Unmarshal([]byte(in), &op); err != nil {
		t.Fatalf("unmarshal unknown: %v", err)
	}
	if op.Type != OpKind("some_future_op") {
		t.Fatalf("unexpected type %q", op.Type)
	}
	if len(op.Extra) != 2 {
		t.Fatalf("expected 2 extra fields, got %d: %+v", len(op.Extra), op.Extra)
	}
	got, err := json.Marshal(op)
	if err != nil {
		t.Fatalf("marshal unknown: %v", err)
	}
	assertJSONEqual(t, string(got), in)
}

func TestSubmissionRoundTrip(t *testing.T) {
	clientID := "msg-1"
	tp := "00-trace-span-01"
	sub := Submission{
		ID:                  "sub-100",
		Op:                  Op{Type: OpInterrupt},
		ClientUserMessageID: &clientID,
		Trace:               &W3cTraceContext{Traceparent: &tp},
	}
	got, err := json.Marshal(sub)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"id":"sub-100","op":{"type":"interrupt"},"client_user_message_id":"msg-1","trace":{"traceparent":"00-trace-span-01"}}`
	assertJSONEqual(t, string(got), want)

	first, second := roundTripJSON(t, sub)
	assertJSONEqual(t, second, first)
}

func TestSubmissionOmitsOptionalFields(t *testing.T) {
	sub := Submission{ID: "s", Op: Op{Type: OpShutdown}}
	got, err := json.Marshal(sub)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	assertJSONEqual(t, string(got), `{"id":"s","op":{"type":"shutdown"}}`)
}

func TestInterAgentCommunicationAlwaysEmitsOtherRecipients(t *testing.T) {
	c := InterAgentCommunication{
		Author:      AgentPathRootValue(),
		Recipient:   AgentPathMorpheusValue(),
		Content:     "ping",
		TriggerTurn: true,
	}
	got, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// other_recipients is serde(default) with no skip; emitted as [] when empty.
	want := `{"author":"/root","recipient":"/morpheus","other_recipients":[],"content":"ping","trigger_turn":true}`
	assertJSONEqual(t, string(got), want)
}
