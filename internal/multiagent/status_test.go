package multiagent

import (
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

func strptr(s string) *string { return &s }

func TestStatusFromEvent(t *testing.T) {
	tests := []struct {
		name     string
		msg      protocol.EventMsg
		wantOK   bool
		wantKind protocol.AgentStatusKind
		wantMsg  string
	}{
		{
			name:     "turn_started maps to running",
			msg:      protocol.EventMsg{Type: protocol.EventMsgKindTurnStarted},
			wantOK:   true,
			wantKind: protocol.AgentStatusRunning,
		},
		{
			name:     "turn_complete maps to completed with message",
			msg:      protocol.EventMsg{Type: protocol.EventMsgKindTurnComplete, TurnComplete: &protocol.TurnCompleteEvent{LastAgentMessage: strptr("done")}},
			wantOK:   true,
			wantKind: protocol.AgentStatusCompleted,
			wantMsg:  "done",
		},
		{
			name:     "turn_aborted interrupted maps to interrupted",
			msg:      protocol.EventMsg{Type: protocol.EventMsgKindTurnAborted, TurnAborted: &protocol.TurnAbortedEvent{Reason: protocol.TurnAbortReasonInterrupted}},
			wantOK:   true,
			wantKind: protocol.AgentStatusInterrupted,
		},
		{
			name:     "turn_aborted budget_limited maps to interrupted",
			msg:      protocol.EventMsg{Type: protocol.EventMsgKindTurnAborted, TurnAborted: &protocol.TurnAbortedEvent{Reason: protocol.TurnAbortReasonBudgetLimited}},
			wantOK:   true,
			wantKind: protocol.AgentStatusInterrupted,
		},
		{
			name:     "turn_aborted other reason maps to errored",
			msg:      protocol.EventMsg{Type: protocol.EventMsgKindTurnAborted, TurnAborted: &protocol.TurnAbortedEvent{Reason: protocol.TurnAbortReasonReplaced}},
			wantOK:   true,
			wantKind: protocol.AgentStatusErrored,
			wantMsg:  string(protocol.TurnAbortReasonReplaced),
		},
		{
			name:     "error maps to errored",
			msg:      protocol.EventMsg{Type: protocol.EventMsgKindError, Error: &protocol.ErrorEvent{Message: "boom"}},
			wantOK:   true,
			wantKind: protocol.AgentStatusErrored,
			wantMsg:  "boom",
		},
		{
			name:     "shutdown_complete maps to shutdown",
			msg:      protocol.EventMsg{Type: protocol.EventMsgKindShutdownComplete},
			wantOK:   true,
			wantKind: protocol.AgentStatusShutdown,
		},
		{
			name:   "unrelated event does not affect status",
			msg:    protocol.EventMsg{Type: protocol.EventMsgKindSessionConfigured},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := StatusFromEvent(tt.msg)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if got.Kind != tt.wantKind {
				t.Fatalf("kind = %q, want %q", got.Kind, tt.wantKind)
			}
			switch got.Kind {
			case protocol.AgentStatusCompleted:
				if got.CompletedMessage == nil || *got.CompletedMessage != tt.wantMsg {
					t.Fatalf("completed message = %v, want %q", got.CompletedMessage, tt.wantMsg)
				}
			case protocol.AgentStatusErrored:
				if got.ErroredMessage != tt.wantMsg {
					t.Fatalf("errored message = %q, want %q", got.ErroredMessage, tt.wantMsg)
				}
			}
		})
	}
}

func TestIsFinal(t *testing.T) {
	tests := []struct {
		kind protocol.AgentStatusKind
		want bool
	}{
		{protocol.AgentStatusPendingInit, false},
		{protocol.AgentStatusRunning, false},
		{protocol.AgentStatusInterrupted, false},
		{protocol.AgentStatusCompleted, true},
		{protocol.AgentStatusErrored, true},
		{protocol.AgentStatusShutdown, true},
		{protocol.AgentStatusNotFound, true},
	}
	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			if got := IsFinal(protocol.AgentStatus{Kind: tt.kind}); got != tt.want {
				t.Fatalf("IsFinal(%q) = %v, want %v", tt.kind, got, tt.want)
			}
		})
	}
}
