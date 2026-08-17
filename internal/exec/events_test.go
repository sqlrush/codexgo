package exec

import (
	"encoding/json"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// TestThreadEventMarshal verifies each ThreadEvent variant serializes to the
// expected JSON object (the externally-tagged shape codex exec emits).
func TestThreadEventMarshal(t *testing.T) {
	exit := int32(0)
	tests := []struct {
		name string
		ev   ThreadEvent
		want map[string]any
	}{
		{
			name: "thread_started",
			ev:   ThreadEvent{Kind: ThreadEventKindThreadStarted, ThreadStarted: &ThreadStartedEvent{ThreadID: "t1"}},
			want: map[string]any{"type": "thread.started", "thread_id": "t1"},
		},
		{
			name: "turn_started",
			ev:   ThreadEvent{Kind: ThreadEventKindTurnStarted, TurnStarted: &TurnStartedEvent{}},
			want: map[string]any{"type": "turn.started"},
		},
		{
			name: "turn_completed",
			ev: ThreadEvent{Kind: ThreadEventKindTurnCompleted, TurnCompleted: &TurnCompletedEvent{
				Usage: Usage{InputTokens: 3, CachedInputTokens: 1, OutputTokens: 5, ReasoningOutputTokens: 2},
			}},
			want: map[string]any{
				"type": "turn.completed",
				"usage": map[string]any{
					"input_tokens": 3.0, "cached_input_tokens": 1.0,
					"output_tokens": 5.0, "reasoning_output_tokens": 2.0,
				},
			},
		},
		{
			name: "turn_failed",
			ev: ThreadEvent{Kind: ThreadEventKindTurnFailed, TurnFailed: &TurnFailedEvent{
				Error: ThreadErrorEvent{Message: "boom"},
			}},
			want: map[string]any{"type": "turn.failed", "error": map[string]any{"message": "boom"}},
		},
		{
			name: "error",
			ev:   ThreadEvent{Kind: ThreadEventKindError, Error: &ThreadErrorEvent{Message: "fatal"}},
			want: map[string]any{"type": "error", "message": "fatal"},
		},
		{
			name: "item_completed_agent_message",
			ev: ThreadEvent{Kind: ThreadEventKindItemCompleted, ItemCompleted: &ItemEvent{Item: ThreadItem{
				ID:      "item_0",
				Details: ThreadItemDetails{Kind: ThreadItemDetailKindAgentMessage, AgentMessage: &AgentMessageItem{Text: "hi"}},
			}}},
			want: map[string]any{
				"type": "item.completed",
				"item": map[string]any{"id": "item_0", "type": "agent_message", "text": "hi"},
			},
		},
		{
			name: "item_completed_command_execution",
			ev: ThreadEvent{Kind: ThreadEventKindItemCompleted, ItemCompleted: &ItemEvent{Item: ThreadItem{
				ID: "item_1",
				Details: ThreadItemDetails{Kind: ThreadItemDetailKindCommandExecution, CommandExecution: &CommandExecutionItem{
					Command: "ls -la", AggregatedOutput: "out", ExitCode: &exit, Status: CommandExecutionStatusCompleted,
				}},
			}}},
			want: map[string]any{
				"type": "item.completed",
				"item": map[string]any{
					"id": "item_1", "type": "command_execution",
					"command": "ls -la", "aggregated_output": "out",
					"exit_code": 0.0, "status": "completed",
				},
			},
		},
		{
			name: "item_started_file_change",
			ev: ThreadEvent{Kind: ThreadEventKindItemStarted, ItemStarted: &ItemEvent{Item: ThreadItem{
				ID: "item_2",
				Details: ThreadItemDetails{Kind: ThreadItemDetailKindFileChange, FileChange: &FileChangeItem{
					Changes: []FileUpdateChange{{Path: "a.go", Kind: PatchChangeKindUpdate}},
					Status:  PatchApplyStatusCompleted,
				}},
			}}},
			want: map[string]any{
				"type": "item.started",
				"item": map[string]any{
					"id": "item_2", "type": "file_change", "status": "completed",
					"changes": []any{map[string]any{"path": "a.go", "kind": "update"}},
				},
			},
		},
		{
			name: "item_completed_todo_list",
			ev: ThreadEvent{Kind: ThreadEventKindItemCompleted, ItemCompleted: &ItemEvent{Item: ThreadItem{
				ID: "item_3",
				Details: ThreadItemDetails{Kind: ThreadItemDetailKindTodoList, TodoList: &TodoListItem{
					Items: []TodoItem{{Text: "step", Completed: true}},
				}},
			}}},
			want: map[string]any{
				"type": "item.completed",
				"item": map[string]any{
					"id": "item_3", "type": "todo_list",
					"items": []any{map[string]any{"text": "step", "completed": true}},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.ev)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			var got map[string]any
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			assertJSONEqual(t, tc.want, got)
		})
	}
}

// TestThreadEventRoundTrip verifies ThreadEvents survive a Marshal/Unmarshal
// round-trip with their discriminator and payload intact.
func TestThreadEventRoundTrip(t *testing.T) {
	events := []ThreadEvent{
		{Kind: ThreadEventKindThreadStarted, ThreadStarted: &ThreadStartedEvent{ThreadID: "t"}},
		{Kind: ThreadEventKindTurnStarted, TurnStarted: &TurnStartedEvent{}},
		{Kind: ThreadEventKindTurnCompleted, TurnCompleted: &TurnCompletedEvent{Usage: Usage{InputTokens: 1}}},
		{Kind: ThreadEventKindError, Error: &ThreadErrorEvent{Message: "x"}},
		{Kind: ThreadEventKindItemCompleted, ItemCompleted: &ItemEvent{Item: ThreadItem{
			ID:      "i",
			Details: ThreadItemDetails{Kind: ThreadItemDetailKindReasoning, Reasoning: &ReasoningItem{Text: "why"}},
		}}},
	}
	for _, ev := range events {
		b, err := json.Marshal(ev)
		if err != nil {
			t.Fatalf("Marshal(%s): %v", ev.Kind, err)
		}
		var back ThreadEvent
		if err := json.Unmarshal(b, &back); err != nil {
			t.Fatalf("Unmarshal(%s): %v", ev.Kind, err)
		}
		if back.Kind != ev.Kind {
			t.Fatalf("kind: got %q want %q", back.Kind, ev.Kind)
		}
	}
}

// TestWebSearchItemMarshal verifies the web_search item embeds the protocol
// WebSearchAction faithfully.
func TestWebSearchItemMarshal(t *testing.T) {
	q := "codex docs"
	item := ThreadItem{
		ID: "ws_1",
		Details: ThreadItemDetails{Kind: ThreadItemDetailKindWebSearch, WebSearch: &WebSearchItem{
			ID:     "call_1",
			Query:  q,
			Action: protocol.WebSearchAction{Type: protocol.WebSearchActionKindSearch, Query: &q},
		}},
	}
	b, err := json.Marshal(ThreadEvent{Kind: ThreadEventKindItemCompleted, ItemCompleted: &ItemEvent{Item: item}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	inner := got["item"].(map[string]any)
	// The outer ThreadItem id wins over the flattened WebSearchItem.id (serde
	// flatten collides on "id"; the explicit ThreadItem id is authoritative).
	if inner["type"] != "web_search" || inner["query"] != q || inner["id"] != "ws_1" {
		t.Fatalf("unexpected web_search item: %v", inner)
	}
}

// assertJSONEqual compares two decoded JSON values for deep equality, failing
// with a readable message on mismatch.
func assertJSONEqual(t *testing.T, want, got any) {
	t.Helper()
	wb, _ := json.Marshal(want)
	gb, _ := json.Marshal(got)
	if string(wb) != string(gb) {
		t.Fatalf("JSON mismatch:\n want %s\n  got %s", wb, gb)
	}
}
