package guardian

import (
	"github.com/sqlrush/codexgo/internal/ext/extensionapi"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// AssessmentEmitter publishes guardian assessment lifecycle events into the
// approval flow via the host event sink.
//
// The guardian reviews a proposed action (a command, execve, apply_patch,
// network access, MCP tool call, or permission request) and reports its
// progress and verdict as protocol.GuardianAssessmentEvent values. Those events
// feed the host approval flow. The canonical assessment payload types live in
// internal/protocol and are reused verbatim here; this emitter is the thin
// bridge from the guardian to the host event sink, mirroring how the host wires
// guardian output into approvals.
type AssessmentEmitter struct {
	sink extensionapi.ExtensionEventSink
}

// NewAssessmentEmitter builds an emitter over a host event sink. A nil sink is
// replaced with a no-op sink so callers never need a nil check.
func NewAssessmentEmitter(sink extensionapi.ExtensionEventSink) AssessmentEmitter {
	if sink == nil {
		sink = extensionapi.NoopExtensionEventSink{}
	}
	return AssessmentEmitter{sink: sink}
}

// Emit publishes one guardian assessment lifecycle event correlated with the
// supplied submission id. The event id is the correlation id the host uses to
// order and route the event; the assessment carries the reviewed action and the
// current status/verdict.
func (e AssessmentEmitter) Emit(eventID string, assessment protocol.GuardianAssessmentEvent) {
	e.sink.Emit(protocol.Event{
		ID: eventID,
		Msg: protocol.EventMsg{
			Type:               protocol.EventMsgKindGuardianAssessment,
			GuardianAssessment: &assessment,
		},
	})
}
