package analytics

import (
	"encoding/json"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

func TestGuardianReviewEventPayloadFlatten(t *testing.T) {
	t.Parallel()

	tc := NewGuardianReviewTrackContext(
		"thread-1",
		"turn-1",
		"review-1",
		nil,
		GuardianApprovalRequestSourceMainTurn,
		GuardianReviewedAction{Kind: GuardianReviewedActionApplyPatch},
		5000,
	)
	usage := protocol.TokenUsage{InputTokens: 1, CachedInputTokens: 2, OutputTokens: 3, ReasoningOutputTokens: 4, TotalTokens: 10}
	result := GuardianReviewAnalyticsResultWithoutSession()
	result.Decision = GuardianReviewDecisionApproved
	result.TerminalStatus = GuardianReviewTerminalStatusApproved
	result.TokenUsage = &usage

	params := tc.EventParams(result, NowUnixMillis())
	payload := GuardianReviewEventPayload{
		SessionID:       "thread-1",
		AppServerClient: CodexAppServerClientMetadata{RpcTransport: AppServerRpcTransportInProcess},
		Runtime:         CurrentRuntimeMetadata(),
		GuardianReview:  params,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Header fields are present.
	if m["session_id"] != "thread-1" {
		t.Errorf("session_id: got %v", m["session_id"])
	}
	if _, ok := m["app_server_client"]; !ok {
		t.Error("missing app_server_client")
	}
	if _, ok := m["runtime"]; !ok {
		t.Error("missing runtime")
	}
	// Flattened guardian review fields appear at top level (not nested).
	if m["thread_id"] != "thread-1" {
		t.Errorf("flattened thread_id: got %v", m["thread_id"])
	}
	if m["decision"] != "approved" {
		t.Errorf("flattened decision: got %v", m["decision"])
	}
	if m["input_tokens"] != float64(1) {
		t.Errorf("flattened input_tokens: got %v", m["input_tokens"])
	}
	if _, nested := m["guardian_review"]; nested {
		t.Error("guardian_review should be flattened, not nested")
	}
}

func TestGuardianResultWithoutSessionDefaults(t *testing.T) {
	t.Parallel()
	r := GuardianReviewAnalyticsResultWithoutSession()
	if r.Decision != GuardianReviewDecisionDenied {
		t.Errorf("default decision: got %q", r.Decision)
	}
	if r.TerminalStatus != GuardianReviewTerminalStatusFailedClosed {
		t.Errorf("default terminal status: got %q", r.TerminalStatus)
	}
}
