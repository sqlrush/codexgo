package core

import (
	"testing"

	"github.com/sqlrush/codexgo/internal/modelsmanager"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// TestBuildReasoningMapsUltraToMax mirrors the 0.147 client test for
// reasoning_effort_for_request: `ultra` is sent as `max`; other tiers pass
// through, both for the explicit effort and the model default.
func TestBuildReasoningMapsUltraToMax(t *testing.T) {
	info := modelsmanager.ModelInfo{SupportsReasoningSummary: true}
	ultra := protocol.ReasoningEffortUltra
	high := protocol.ReasoningEffortHigh

	if r := buildReasoning(info, &ultra, protocol.ReasoningSummaryNone); r == nil || r.Effort == nil || *r.Effort != protocol.ReasoningEffortMax {
		t.Fatalf("ultra effort should be sent as max, got %+v", r)
	}
	if r := buildReasoning(info, &high, protocol.ReasoningSummaryNone); r == nil || r.Effort == nil || *r.Effort != protocol.ReasoningEffortHigh {
		t.Fatalf("high effort should pass through, got %+v", r)
	}
	info.DefaultReasoningLevel = &ultra
	if r := buildReasoning(info, nil, protocol.ReasoningSummaryNone); r == nil || r.Effort == nil || *r.Effort != protocol.ReasoningEffortMax {
		t.Fatalf("ultra model default should be sent as max, got %+v", r)
	}
	if protocol.ReasoningEffortMax.ForRequest() != protocol.ReasoningEffortMax {
		t.Fatalf("max must not be remapped")
	}
}
