package exec

import (
	"encoding/json"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// ev wraps an EventMsg in the engine Event envelope used by the processor.
func ev(msg protocol.EventMsg) protocol.Event { return protocol.Event{ID: "sub-1", Msg: msg} }

// TestProcessorCollect exercises the EventMsg -> ThreadEvent transformation for
// the events that drive the JSONL stream, asserting the emitted kinds and status.
func TestProcessorCollect(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(p *JSONLProcessor)
		event      protocol.Event
		wantKinds  []ThreadEventKind
		wantStatus CodexStatus
	}{
		{
			name:       "turn_started",
			event:      ev(protocol.EventMsg{Type: protocol.EventMsgKindTurnStarted, TurnStarted: &protocol.TurnStartedEvent{TurnID: "t"}}),
			wantKinds:  []ThreadEventKind{ThreadEventKindTurnStarted},
			wantStatus: StatusRunning,
		},
		{
			name: "agent_message_records_final_no_event",
			event: ev(protocol.EventMsg{
				Type:         protocol.EventMsgKindAgentMessage,
				AgentMessage: &protocol.AgentMessageEvent{Message: "answer"},
			}),
			wantKinds:  nil,
			wantStatus: StatusRunning,
		},
		{
			name: "item_completed_agent_message",
			event: ev(protocol.EventMsg{
				Type: protocol.EventMsgKindItemCompleted,
				ItemCompleted: &protocol.ItemCompletedEvent{Item: protocol.TurnItem{
					Type: protocol.TurnItemKindAgentMessage,
					AgentMessage: &protocol.AgentMessageItem{
						ID:      "m1",
						Content: []protocol.AgentMessageContent{protocol.NewAgentMessageText("hello")},
					},
				}},
			}),
			wantKinds:  []ThreadEventKind{ThreadEventKindItemCompleted},
			wantStatus: StatusRunning,
		},
		{
			name: "item_started_agent_message_suppressed",
			event: ev(protocol.EventMsg{
				Type: protocol.EventMsgKindItemStarted,
				ItemStarted: &protocol.ItemStartedEvent{Item: protocol.TurnItem{
					Type:         protocol.TurnItemKindAgentMessage,
					AgentMessage: &protocol.AgentMessageItem{ID: "m1"},
				}},
			}),
			wantKinds:  nil,
			wantStatus: StatusRunning,
		},
		{
			name: "item_completed_empty_reasoning_dropped",
			event: ev(protocol.EventMsg{
				Type: protocol.EventMsgKindItemCompleted,
				ItemCompleted: &protocol.ItemCompletedEvent{Item: protocol.TurnItem{
					Type:      protocol.TurnItemKindReasoning,
					Reasoning: &protocol.ReasoningTurnItem{ID: "r1", SummaryText: []string{"  "}},
				}},
			}),
			wantKinds:  nil,
			wantStatus: StatusRunning,
		},
		{
			name: "item_completed_reasoning",
			event: ev(protocol.EventMsg{
				Type: protocol.EventMsgKindItemCompleted,
				ItemCompleted: &protocol.ItemCompletedEvent{Item: protocol.TurnItem{
					Type:      protocol.TurnItemKindReasoning,
					Reasoning: &protocol.ReasoningTurnItem{ID: "r1", SummaryText: []string{"because", "reasons"}},
				}},
			}),
			wantKinds:  []ThreadEventKind{ThreadEventKindItemCompleted},
			wantStatus: StatusRunning,
		},
		{
			name: "exec_command_begin",
			event: ev(protocol.EventMsg{
				Type:             protocol.EventMsgKindExecCommandBegin,
				ExecCommandBegin: &protocol.ExecCommandBeginEvent{CallID: "c1", Command: []string{"ls", "-la"}},
			}),
			wantKinds:  []ThreadEventKind{ThreadEventKindItemStarted},
			wantStatus: StatusRunning,
		},
		{
			name: "error_event",
			event: ev(protocol.EventMsg{
				Type:  protocol.EventMsgKindError,
				Error: &protocol.ErrorEvent{Message: "boom"},
			}),
			wantKinds:  []ThreadEventKind{ThreadEventKindError},
			wantStatus: StatusRunning,
		},
		{
			name: "deprecation_notice_to_error_item",
			event: ev(protocol.EventMsg{
				Type:              protocol.EventMsgKindDeprecationNotice,
				DeprecationNotice: &protocol.DeprecationNoticeEvent{Summary: "old"},
			}),
			wantKinds:  []ThreadEventKind{ThreadEventKindItemCompleted},
			wantStatus: StatusRunning,
		},
		{
			name: "turn_complete_shutdown",
			event: ev(protocol.EventMsg{
				Type:         protocol.EventMsgKindTurnComplete,
				TurnComplete: &protocol.TurnCompleteEvent{TurnID: "t"},
			}),
			wantKinds:  []ThreadEventKind{ThreadEventKindTurnCompleted},
			wantStatus: StatusInitiateShutdown,
		},
		{
			name: "turn_aborted_interrupt_no_event",
			event: ev(protocol.EventMsg{
				Type:        protocol.EventMsgKindTurnAborted,
				TurnAborted: &protocol.TurnAbortedEvent{Reason: protocol.TurnAbortReasonInterrupted},
			}),
			wantKinds:  nil,
			wantStatus: StatusInitiateShutdown,
		},
		{
			name: "turn_aborted_other_fails",
			event: ev(protocol.EventMsg{
				Type:        protocol.EventMsgKindTurnAborted,
				TurnAborted: &protocol.TurnAbortedEvent{Reason: protocol.TurnAbortReasonReplaced},
			}),
			wantKinds:  []ThreadEventKind{ThreadEventKindTurnFailed},
			wantStatus: StatusInitiateShutdown,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := NewJSONLProcessor()
			if tc.setup != nil {
				tc.setup(p)
			}
			got := p.Collect(tc.event)
			gotKinds := kindsOf(got.Events)
			if !equalKinds(gotKinds, tc.wantKinds) {
				t.Fatalf("kinds: got %v want %v", gotKinds, tc.wantKinds)
			}
			if got.Status != tc.wantStatus {
				t.Fatalf("status: got %v want %v", got.Status, tc.wantStatus)
			}
		})
	}
}

// TestProcessorCommandLifecycle verifies begin/end share an item id and that the
// completed item carries the exit code, output, and translated status.
func TestProcessorCommandLifecycle(t *testing.T) {
	p := NewJSONLProcessor()
	begin := p.Collect(ev(protocol.EventMsg{
		Type:             protocol.EventMsgKindExecCommandBegin,
		ExecCommandBegin: &protocol.ExecCommandBeginEvent{CallID: "c1", Command: []string{"echo", "hi"}},
	}))
	end := p.Collect(ev(protocol.EventMsg{
		Type: protocol.EventMsgKindExecCommandEnd,
		ExecCommandEnd: &protocol.ExecCommandEndEvent{
			CallID: "c1", Command: []string{"echo", "hi"},
			AggregatedOutput: "hi\n", ExitCode: 0, Status: protocol.ExecCommandStatusCompleted,
		},
	}))

	beginItem := begin.Events[0].ItemStarted.Item
	endItem := end.Events[0].ItemCompleted.Item
	if beginItem.ID != endItem.ID {
		t.Fatalf("begin/end ids differ: %q vs %q", beginItem.ID, endItem.ID)
	}
	ce := endItem.Details.CommandExecution
	if ce.Command != "echo hi" || ce.AggregatedOutput != "hi\n" || ce.Status != CommandExecutionStatusCompleted {
		t.Fatalf("unexpected completed command item: %+v", ce)
	}
	if ce.ExitCode == nil || *ce.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %v", ce.ExitCode)
	}
}

// TestProcessorTodoListLifecycle verifies plan updates start a todo item, update
// it in place (same id), and the open list is flushed as completed on turn end.
func TestProcessorTodoListLifecycle(t *testing.T) {
	p := NewJSONLProcessor()
	first := p.Collect(ev(protocol.EventMsg{
		Type: protocol.EventMsgKindPlanUpdate,
		PlanUpdate: &protocol.UpdatePlanArgs{Plan: []protocol.PlanItemArg{
			{Step: "a", Status: protocol.StepStatusPending},
		}},
	}))
	if first.Events[0].Kind != ThreadEventKindItemStarted {
		t.Fatalf("first plan update should start an item, got %v", first.Events[0].Kind)
	}
	startID := first.Events[0].ItemStarted.Item.ID

	second := p.Collect(ev(protocol.EventMsg{
		Type: protocol.EventMsgKindPlanUpdate,
		PlanUpdate: &protocol.UpdatePlanArgs{Plan: []protocol.PlanItemArg{
			{Step: "a", Status: protocol.StepStatusCompleted},
		}},
	}))
	if second.Events[0].Kind != ThreadEventKindItemUpdated {
		t.Fatalf("second plan update should update the item, got %v", second.Events[0].Kind)
	}
	if second.Events[0].ItemUpdated.Item.ID != startID {
		t.Fatalf("todo item id changed across updates")
	}
	if !second.Events[0].ItemUpdated.Item.Details.TodoList.Items[0].Completed {
		t.Fatalf("expected the step to be marked completed")
	}

	done := p.Collect(ev(protocol.EventMsg{
		Type:         protocol.EventMsgKindTurnComplete,
		TurnComplete: &protocol.TurnCompleteEvent{TurnID: "t"},
	}))
	if got := kindsOf(done.Events); !equalKinds(got, []ThreadEventKind{ThreadEventKindItemCompleted, ThreadEventKindTurnCompleted}) {
		t.Fatalf("turn complete should flush todo then complete; got %v", got)
	}
}

// TestProcessorUsageFromTokenCount verifies token usage is threaded into the
// turn.completed event.
func TestProcessorUsageFromTokenCount(t *testing.T) {
	p := NewJSONLProcessor()
	p.Collect(ev(protocol.EventMsg{
		Type: protocol.EventMsgKindTokenCount,
		TokenCount: &protocol.TokenCountEvent{Info: &protocol.TokenUsageInfo{
			TotalTokenUsage: protocol.TokenUsage{InputTokens: 10, CachedInputTokens: 2, OutputTokens: 7, ReasoningOutputTokens: 3},
		}},
	}))
	done := p.Collect(ev(protocol.EventMsg{
		Type:         protocol.EventMsgKindTurnComplete,
		TurnComplete: &protocol.TurnCompleteEvent{TurnID: "t"},
	}))
	usage := done.Events[len(done.Events)-1].TurnCompleted.Usage
	want := Usage{InputTokens: 10, CachedInputTokens: 2, OutputTokens: 7, ReasoningOutputTokens: 3}
	if usage != want {
		t.Fatalf("usage: got %+v want %+v", usage, want)
	}
}

// TestProcessorFinalMessageFromTurnComplete verifies the last agent message in
// the turn.complete event is captured and that a failed turn clears it.
func TestProcessorFinalMessageFromTurnComplete(t *testing.T) {
	p := NewJSONLProcessor()
	final := "the answer"
	p.Collect(ev(protocol.EventMsg{
		Type:         protocol.EventMsgKindTurnComplete,
		TurnComplete: &protocol.TurnCompleteEvent{TurnID: "t", LastAgentMessage: &final},
	}))
	if p.FinalMessage() == nil || *p.FinalMessage() != final {
		t.Fatalf("final message not captured: %v", p.FinalMessage())
	}
	if !p.EmitFinalOnExit() {
		t.Fatal("expected emitFinalOnExit after a successful turn")
	}

	p2 := NewJSONLProcessor()
	msg := "partial"
	p2.finalMessage = &msg
	p2.Collect(ev(protocol.EventMsg{
		Type:        protocol.EventMsgKindTurnAborted,
		TurnAborted: &protocol.TurnAbortedEvent{Reason: protocol.TurnAbortReasonReplaced},
	}))
	if p2.FinalMessage() != nil {
		t.Fatalf("failed turn should clear final message, got %v", *p2.FinalMessage())
	}
	if p2.EmitFinalOnExit() {
		t.Fatal("failed turn should not emit final on exit")
	}
}

// TestProcessorMcpResultPreservesMeta mirrors the Rust jsonl test: the MCP
// result _meta survives into the emitted item.
func TestProcessorMcpResultPreservesMeta(t *testing.T) {
	p := NewJSONLProcessor()
	result := json.RawMessage(`{"content":[{"type":"text","text":"r"}],"_meta":{"k":"v"},"structured_content":null}`)
	out := p.Collect(ev(protocol.EventMsg{
		Type: protocol.EventMsgKindItemCompleted,
		ItemCompleted: &protocol.ItemCompletedEvent{Item: protocol.TurnItem{
			Type: protocol.TurnItemKindMcpToolCall,
			McpToolCall: &protocol.McpToolCallItem{
				ID: "mcp1", Server: "s", Tool: "t",
				Status: protocol.McpToolCallStatusCompleted,
				Result: result,
			},
		}},
	}))
	b, err := json.Marshal(out.Events[0])
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	item := decoded["item"].(map[string]any)
	res := item["result"].(map[string]any)
	meta, ok := res["_meta"].(map[string]any)
	if !ok || meta["k"] != "v" {
		t.Fatalf("expected _meta preserved, got %v", res)
	}
	if _, has := res["meta"]; has {
		t.Fatalf("did not expect a bare meta key: %v", res)
	}
}

// kindsOf extracts the event kinds in order.
func kindsOf(events []ThreadEvent) []ThreadEventKind {
	var out []ThreadEventKind
	for _, e := range events {
		out = append(out, e.Kind)
	}
	return out
}

// equalKinds compares two kind slices for equality (nil == empty).
func equalKinds(a, b []ThreadEventKind) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
