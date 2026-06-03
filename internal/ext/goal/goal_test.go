package goal

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/internal/ext/extensionapi"
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/tools"
)

const testThreadUUID = "11111111-1111-1111-1111-111111111111"

func i64(v int64) *int64 { return &v }

// Compile-time assertions that GoalExtension satisfies every contributor
// interface it is installed as.
var (
	_ extensionapi.ThreadLifecycleContributor[struct{}] = (*GoalExtension[struct{}])(nil)
	_ extensionapi.ConfigContributor[struct{}]          = (*GoalExtension[struct{}])(nil)
	_ extensionapi.TurnLifecycleContributor             = (*GoalExtension[struct{}])(nil)
	_ extensionapi.TokenUsageContributor                = (*GoalExtension[struct{}])(nil)
	_ extensionapi.ToolLifecycleContributor             = (*GoalExtension[struct{}])(nil)
	_ extensionapi.ToolContributor                      = (*GoalExtension[struct{}])(nil)
	_ tools.ToolExecutor[tools.ToolCall]                = (*goalToolExecutor)(nil)
)

func TestToolSpecsMatchNames(t *testing.T) {
	tests := []struct {
		name string
		spec tools.ToolSpec
		want string
		req  []string
	}{
		{"get", createGetGoalTool(), GetGoalToolName, []string{}},
		{"create", createCreateGoalTool(), CreateGoalToolName, []string{"objective"}},
		{"update", createUpdateGoalTool(), UpdateGoalToolName, []string{"status"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.spec.Kind != tools.ToolSpecKindFunction || tc.spec.Function == nil {
				t.Fatalf("spec kind = %v", tc.spec.Kind)
			}
			if tc.spec.Function.Name != tc.want {
				t.Errorf("name = %q, want %q", tc.spec.Function.Name, tc.want)
			}
			if tc.spec.Function.Strict {
				t.Error("strict should be false")
			}
			// Required list round-trips through JSON to confirm wire shape.
			b, err := json.Marshal(tc.spec.Function.Parameters)
			if err != nil {
				t.Fatalf("marshal parameters: %v", err)
			}
			if !strings.Contains(string(b), `"additionalProperties":false`) {
				t.Errorf("parameters missing additionalProperties:false: %s", b)
			}
		})
	}
}

func TestUpdateGoalSpecDescriptionReferencesUpdateName(t *testing.T) {
	spec := createCreateGoalTool()
	if !strings.Contains(spec.Function.Description, UpdateGoalToolName) {
		t.Errorf("create description should reference %q", UpdateGoalToolName)
	}
}

func TestEscapeXMLText(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"plain", "plain"},
		{"a & b", "a &amp; b"},
		{"<tag>", "&lt;tag&gt;"},
		{"a<b>c&d", "a&lt;b&gt;c&amp;d"},
	}
	for _, tc := range tests {
		if got := escapeXMLText(tc.in); got != tc.want {
			t.Errorf("escapeXMLText(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSteeringItemsAreHiddenUserContext(t *testing.T) {
	goal := protocol.ThreadGoal{
		ThreadID:        protocol.NewThreadID(testThreadUUID),
		Objective:       "ship <feature>",
		Status:          protocol.ThreadGoalStatusBudgetLimited,
		TokenBudget:     i64(100),
		TokensUsed:      120,
		TimeUsedSeconds: 42,
	}
	item := budgetLimitSteeringItem(goal)
	if item.Type != protocol.ResponseItemKindMessage || item.Role != "user" {
		t.Fatalf("item type=%v role=%q", item.Type, item.Role)
	}
	if len(item.Content) != 1 || item.Content[0].Type != protocol.ContentItemKindInputText {
		t.Fatalf("content = %+v", item.Content)
	}
	text := item.Content[0].Text
	if !strings.HasPrefix(text, contextStartMarker) || !strings.HasSuffix(text, contextEndMarker) {
		t.Fatalf("text not marker-wrapped: %q", text)
	}
	if !strings.Contains(text, `source="goal"`) {
		t.Errorf("missing source attribute: %q", text)
	}
	// Untrusted objective must be XML-escaped inside the body.
	if !strings.Contains(text, "ship &lt;feature&gt;") {
		t.Errorf("objective not escaped: %q", text)
	}
	if !strings.Contains(text, "Tokens used: 120") || !strings.Contains(text, "Token budget: 100") {
		t.Errorf("budget body missing usage: %q", text)
	}
}

func TestObjectiveUpdatedPromptRemainingTokens(t *testing.T) {
	withBudget := objectiveUpdatedPrompt(protocol.ThreadGoal{Objective: "x", TokenBudget: i64(100), TokensUsed: 30})
	if !strings.Contains(withBudget, "Tokens remaining: 70") {
		t.Errorf("expected remaining 70: %q", withBudget)
	}
	overspent := objectiveUpdatedPrompt(protocol.ThreadGoal{Objective: "x", TokenBudget: i64(100), TokensUsed: 130})
	if !strings.Contains(overspent, "Tokens remaining: 0") {
		t.Errorf("expected clamped remaining 0: %q", overspent)
	}
	noBudget := objectiveUpdatedPrompt(protocol.ThreadGoal{Objective: "x"})
	if !strings.Contains(noBudget, "Token budget: none") || !strings.Contains(noBudget, "Tokens remaining: unknown") {
		t.Errorf("expected none/unknown: %q", noBudget)
	}
}

func TestValidateThreadGoalObjective(t *testing.T) {
	if msg := validateThreadGoalObjective(""); msg == "" {
		t.Error("empty objective should be rejected")
	}
	if msg := validateThreadGoalObjective("ok"); msg != "" {
		t.Errorf("valid objective rejected: %q", msg)
	}
	long := strings.Repeat("a", maxThreadGoalObjectiveChars+1)
	if msg := validateThreadGoalObjective(long); msg == "" {
		t.Error("oversized objective should be rejected")
	}
	exact := strings.Repeat("a", maxThreadGoalObjectiveChars)
	if msg := validateThreadGoalObjective(exact); msg != "" {
		t.Errorf("exact-length objective rejected: %q", msg)
	}
}

func TestValidateGoalBudget(t *testing.T) {
	if msg := validateGoalBudget(nil); msg != "" {
		t.Errorf("nil budget rejected: %q", msg)
	}
	if msg := validateGoalBudget(i64(10)); msg != "" {
		t.Errorf("positive budget rejected: %q", msg)
	}
	if msg := validateGoalBudget(i64(0)); msg == "" {
		t.Error("zero budget should be rejected")
	}
	if msg := validateGoalBudget(i64(-1)); msg == "" {
		t.Error("negative budget should be rejected")
	}
}

func TestProtocolGoalFromStateStatusMapping(t *testing.T) {
	tests := []struct {
		state StateThreadGoalStatus
		want  protocol.ThreadGoalStatus
	}{
		{StateGoalStatusActive, protocol.ThreadGoalStatusActive},
		{StateGoalStatusPaused, protocol.ThreadGoalStatusPaused},
		{StateGoalStatusBlocked, protocol.ThreadGoalStatusBlocked},
		{StateGoalStatusUsageLimited, protocol.ThreadGoalStatusUsageLimited},
		{StateGoalStatusBudgetLimited, protocol.ThreadGoalStatusBudgetLimited},
		{StateGoalStatusComplete, protocol.ThreadGoalStatusComplete},
	}
	for _, tc := range tests {
		got := protocolStatusFromState(tc.state)
		if got != tc.want {
			t.Errorf("protocolStatusFromState(%v) = %v, want %v", tc.state, got, tc.want)
		}
		// Round-trip.
		if back := stateStatusFromProtocol(tc.want); back != tc.state {
			t.Errorf("round-trip status %v -> %v -> %v", tc.state, tc.want, back)
		}
	}
}

func TestStateStatusAsStr(t *testing.T) {
	tests := []struct {
		s    StateThreadGoalStatus
		want string
	}{
		{StateGoalStatusActive, "active"},
		{StateGoalStatusPaused, "paused"},
		{StateGoalStatusBlocked, "blocked"},
		{StateGoalStatusUsageLimited, "usage_limited"},
		{StateGoalStatusBudgetLimited, "budget_limited"},
		{StateGoalStatusComplete, "complete"},
	}
	for _, tc := range tests {
		if got := tc.s.AsStr(); got != tc.want {
			t.Errorf("AsStr(%v) = %q, want %q", tc.s, got, tc.want)
		}
	}
	if !StateGoalStatusActive.IsActive() || StateGoalStatusPaused.IsActive() {
		t.Error("IsActive mismatch")
	}
	if !StateGoalStatusComplete.IsTerminal() || !StateGoalStatusBudgetLimited.IsTerminal() || StateGoalStatusActive.IsTerminal() {
		t.Error("IsTerminal mismatch")
	}
}

func TestToolAttemptCountsForGoalProgress(t *testing.T) {
	tests := []struct {
		outcome extensionapi.ToolCallOutcome
		want    bool
	}{
		{extensionapi.CompletedToolCallOutcome(true), true},
		{extensionapi.CompletedToolCallOutcome(false), true},
		{extensionapi.FailedToolCallOutcome(true), true},
		{extensionapi.FailedToolCallOutcome(false), false},
		{extensionapi.BlockedToolCallOutcome(), false},
		{extensionapi.AbortedToolCallOutcome(), false},
	}
	for _, tc := range tests {
		if got := toolAttemptCountsForGoalProgress(tc.outcome); got != tc.want {
			t.Errorf("outcome %+v -> %v, want %v", tc.outcome, got, tc.want)
		}
	}
}

func makeToolCall(callID, args string) tools.ToolCall {
	return tools.ToolCall{
		CallID: callID,
		Payload: tools.ToolPayload{
			Kind:      tools.ToolPayloadKindFunction,
			Arguments: args,
		},
	}
}

func decodeGoalResponse(t *testing.T, out tools.ToolOutput) goalToolResponse {
	t.Helper()
	jto, ok := out.(tools.JsonToolOutput)
	if !ok {
		t.Fatalf("output is %T, want JsonToolOutput", out)
	}
	var resp goalToolResponse
	if err := json.Unmarshal(jto.Value(), &resp); err != nil {
		t.Fatalf("decode response: %v (raw=%s)", err, jto.Value())
	}
	return resp
}

func TestCreateGoalToolHappyPath(t *testing.T) {
	store := newFakeStore()
	metrics := &fakeMetrics{}
	sink := &captureSink{}
	acct := newGoalAccountingState()
	threadID := protocol.NewThreadID(testThreadUUID)
	exec := newGoalToolExecutor(goalToolKindCreate, threadID, store, acct, newGoalEventEmitter(sink), NewGoalMetrics(metrics))

	out, err := exec.Handle(context.Background(), makeToolCall("call-1", `{"objective":" build it ","token_budget":500}`))
	if err != nil {
		t.Fatalf("Handle err = %v", err)
	}
	resp := decodeGoalResponse(t, out)
	if resp.Goal == nil || resp.Goal.Objective != "build it" {
		t.Fatalf("goal = %+v (objective should be trimmed)", resp.Goal)
	}
	if resp.Goal.Status != protocol.ThreadGoalStatusActive {
		t.Errorf("status = %v, want active", resp.Goal.Status)
	}
	if resp.RemainingTokens == nil || *resp.RemainingTokens != 500 {
		t.Errorf("remaining = %v, want 500", resp.RemainingTokens)
	}
	if store.preview != "build it" {
		t.Errorf("thread preview = %q, want 'build it'", store.preview)
	}
	if len(metrics.counters) != 1 || metrics.counters[0] != goalCreatedMetric {
		t.Errorf("counters = %v, want [%s]", metrics.counters, goalCreatedMetric)
	}
	events := sink.all()
	if len(events) != 1 || events[0].ID != "call-1" {
		t.Fatalf("events = %+v", events)
	}
}

func TestCreateGoalToolRejectsDuplicate(t *testing.T) {
	store := newFakeStore()
	store.goal = &StateThreadGoal{ThreadID: protocol.NewThreadID(testThreadUUID), GoalID: "existing", Objective: "old", Status: StateGoalStatusActive}
	exec := newGoalToolExecutor(goalToolKindCreate, protocol.NewThreadID(testThreadUUID), store, newGoalAccountingState(), newGoalEventEmitter(&captureSink{}), NewGoalMetrics(nil))

	_, err := exec.Handle(context.Background(), makeToolCall("c", `{"objective":"new"}`))
	var fce *tools.FunctionCallError
	if err == nil {
		t.Fatal("expected an error for duplicate goal")
	}
	if !errorsAs(err, &fce) || fce.Kind != tools.FunctionCallErrorRespondToModel {
		t.Fatalf("err = %v, want RespondToModel", err)
	}
	if !strings.Contains(fce.Message, "already has a goal") {
		t.Errorf("message = %q", fce.Message)
	}
}

func TestCreateGoalToolRejectsEmptyObjective(t *testing.T) {
	store := newFakeStore()
	exec := newGoalToolExecutor(goalToolKindCreate, protocol.NewThreadID(testThreadUUID), store, newGoalAccountingState(), newGoalEventEmitter(&captureSink{}), NewGoalMetrics(nil))
	_, err := exec.Handle(context.Background(), makeToolCall("c", `{"objective":"   "}`))
	if err == nil {
		t.Fatal("expected error for empty objective")
	}
}

func TestUpdateGoalToolComplete(t *testing.T) {
	store := newFakeStore()
	store.goal = &StateThreadGoal{ThreadID: protocol.NewThreadID(testThreadUUID), GoalID: "g", Objective: "x", Status: StateGoalStatusActive, TokenBudget: i64(100), TokensUsed: 40}
	metrics := &fakeMetrics{}
	sink := &captureSink{}
	exec := newGoalToolExecutor(goalToolKindUpdate, protocol.NewThreadID(testThreadUUID), store, newGoalAccountingState(), newGoalEventEmitter(sink), NewGoalMetrics(metrics))

	out, err := exec.Handle(context.Background(), makeToolCall("u", `{"status":"complete"}`))
	if err != nil {
		t.Fatalf("Handle err = %v", err)
	}
	resp := decodeGoalResponse(t, out)
	if resp.Goal == nil || resp.Goal.Status != protocol.ThreadGoalStatusComplete {
		t.Fatalf("goal = %+v", resp.Goal)
	}
	if resp.CompletionBudgetReport == nil {
		t.Error("expected completion budget report for budgeted complete goal")
	}
	if store.goal.Status != StateGoalStatusComplete {
		t.Errorf("stored status = %v", store.goal.Status)
	}
}

func TestUpdateGoalToolRejectsInvalidStatus(t *testing.T) {
	store := newFakeStore()
	store.goal = &StateThreadGoal{GoalID: "g", Status: StateGoalStatusActive}
	exec := newGoalToolExecutor(goalToolKindUpdate, protocol.NewThreadID(testThreadUUID), store, newGoalAccountingState(), newGoalEventEmitter(&captureSink{}), NewGoalMetrics(nil))
	_, err := exec.Handle(context.Background(), makeToolCall("u", `{"status":"paused"}`))
	if err == nil {
		t.Fatal("expected error for non-complete/blocked status")
	}
}

func TestUpdateGoalToolNoGoal(t *testing.T) {
	store := newFakeStore()
	exec := newGoalToolExecutor(goalToolKindUpdate, protocol.NewThreadID(testThreadUUID), store, newGoalAccountingState(), newGoalEventEmitter(&captureSink{}), NewGoalMetrics(nil))
	_, err := exec.Handle(context.Background(), makeToolCall("u", `{"status":"blocked"}`))
	if err == nil {
		t.Fatal("expected error when no goal exists")
	}
}

func TestGetGoalToolReturnsCurrent(t *testing.T) {
	store := newFakeStore()
	store.goal = &StateThreadGoal{ThreadID: protocol.NewThreadID(testThreadUUID), GoalID: "g", Objective: "obj", Status: StateGoalStatusActive, TokenBudget: i64(200), TokensUsed: 50}
	exec := newGoalToolExecutor(goalToolKindGet, protocol.NewThreadID(testThreadUUID), store, newGoalAccountingState(), newGoalEventEmitter(&captureSink{}), NewGoalMetrics(nil))

	out, err := exec.Handle(context.Background(), makeToolCall("g", `{}`))
	if err != nil {
		t.Fatalf("Handle err = %v", err)
	}
	resp := decodeGoalResponse(t, out)
	if resp.Goal == nil || resp.Goal.Objective != "obj" {
		t.Fatalf("goal = %+v", resp.Goal)
	}
	if resp.RemainingTokens == nil || *resp.RemainingTokens != 150 {
		t.Errorf("remaining = %v, want 150", resp.RemainingTokens)
	}
	if resp.CompletionBudgetReport != nil {
		t.Error("get should never include completion budget report")
	}
}

func TestGetGoalToolNoGoal(t *testing.T) {
	store := newFakeStore()
	exec := newGoalToolExecutor(goalToolKindGet, protocol.NewThreadID(testThreadUUID), store, newGoalAccountingState(), newGoalEventEmitter(&captureSink{}), NewGoalMetrics(nil))
	out, err := exec.Handle(context.Background(), makeToolCall("g", `{}`))
	if err != nil {
		t.Fatalf("Handle err = %v", err)
	}
	resp := decodeGoalResponse(t, out)
	if resp.Goal != nil {
		t.Errorf("goal should be nil, got %+v", resp.Goal)
	}
}

// errorsAs is a tiny wrapper to avoid importing errors in the test body twice.
func errorsAs(err error, target **tools.FunctionCallError) bool {
	fce, ok := err.(*tools.FunctionCallError)
	if ok {
		*target = fce
	}
	return ok
}
