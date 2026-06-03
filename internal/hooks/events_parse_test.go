package hooks

import (
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

func TestParsePostToolUseCompleted(t *testing.T) {
	handler := makeHandler(t, protocol.HookEventNamePostToolUse, sp("^Bash$"), "echo", 0)

	t.Run("block decision feedback", func(t *testing.T) {
		parsed := parsePostToolUseCompleted(handler, runResult(exitPtr(0), `{"decision":"block","reason":"bash output looked sketchy"}`, ""), sp("turn-1"))
		if parsed.completed.Run.Status != protocol.HookRunStatusBlocked {
			t.Errorf("status = %s", parsed.completed.Run.Status)
		}
		if !slicesEq(parsed.data.feedbackMessagesModel, []string{"bash output looked sketchy"}) {
			t.Errorf("feedback = %v", parsed.data.feedbackMessagesModel)
		}
		if parsed.data.shouldStop {
			t.Errorf("shouldStop should be false")
		}
	})

	t.Run("additional context", func(t *testing.T) {
		parsed := parsePostToolUseCompleted(handler, runResult(exitPtr(0), `{"hookSpecificOutput":{"hookEventName":"PostToolUse","additionalContext":"Remember the bash cleanup note."}}`, ""), sp("turn-1"))
		if !slicesEq(parsed.data.additionalContextsModel, []string{"Remember the bash cleanup note."}) {
			t.Errorf("contexts = %v", parsed.data.additionalContextsModel)
		}
		want := []protocol.HookOutputEntry{{Kind: protocol.HookOutputEntryKindContext, Text: "Remember the bash cleanup note."}}
		if !entriesEqual(parsed.completed.Run.Entries, want) {
			t.Errorf("entries = %v", parsed.completed.Run.Entries)
		}
	})

	t.Run("unsupported updatedMCPToolOutput fails", func(t *testing.T) {
		parsed := parsePostToolUseCompleted(handler, runResult(exitPtr(0), `{"hookSpecificOutput":{"hookEventName":"PostToolUse","updatedMCPToolOutput":{"ok":true}}}`, ""), sp("turn-1"))
		if parsed.completed.Run.Status != protocol.HookRunStatusFailed {
			t.Errorf("status = %s", parsed.completed.Run.Status)
		}
		want := []protocol.HookOutputEntry{errorEntry("PostToolUse hook returned unsupported updatedMCPToolOutput")}
		if !entriesEqual(parsed.completed.Run.Entries, want) {
			t.Errorf("entries = %v", parsed.completed.Run.Entries)
		}
	})

	t.Run("exit 2 surfaces feedback no block", func(t *testing.T) {
		parsed := parsePostToolUseCompleted(handler, runResult(exitPtr(2), "", "post hook says pause"), sp("turn-1"))
		if parsed.completed.Run.Status != protocol.HookRunStatusCompleted {
			t.Errorf("status = %s", parsed.completed.Run.Status)
		}
		if !slicesEq(parsed.data.feedbackMessagesModel, []string{"post hook says pause"}) {
			t.Errorf("feedback = %v", parsed.data.feedbackMessagesModel)
		}
	})

	t.Run("continue false stops with reason", func(t *testing.T) {
		parsed := parsePostToolUseCompleted(handler, runResult(exitPtr(0), `{"continue":false,"stopReason":"halt after bash output","reason":"post-tool hook says stop"}`, ""), sp("turn-1"))
		if parsed.completed.Run.Status != protocol.HookRunStatusStopped {
			t.Errorf("status = %s", parsed.completed.Run.Status)
		}
		if !parsed.data.shouldStop || !ptrEq(parsed.data.stopReason, sp("halt after bash output")) {
			t.Errorf("stop = %v %v", parsed.data.shouldStop, parsed.data.stopReason)
		}
		if !slicesEq(parsed.data.feedbackMessagesModel, []string{"post-tool hook says stop"}) {
			t.Errorf("feedback = %v", parsed.data.feedbackMessagesModel)
		}
		want := []protocol.HookOutputEntry{stopEntry("halt after bash output")}
		if !entriesEqual(parsed.completed.Run.Entries, want) {
			t.Errorf("entries = %v", parsed.completed.Run.Entries)
		}
	})
}

func TestResolvePermissionRequestDecision(t *testing.T) {
	allow := &PermissionRequestDecision{Kind: PermissionRequestAllow}
	deny := &PermissionRequestDecision{Kind: PermissionRequestDeny, Message: "repo deny"}

	t.Run("deny overrides allow", func(t *testing.T) {
		got := resolvePermissionRequestDecision([]*PermissionRequestDecision{allow, deny})
		if got == nil || got.Kind != PermissionRequestDeny || got.Message != "repo deny" {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("allow when none deny", func(t *testing.T) {
		got := resolvePermissionRequestDecision([]*PermissionRequestDecision{allow, allow})
		if got == nil || got.Kind != PermissionRequestAllow {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("none when no decision", func(t *testing.T) {
		if got := resolvePermissionRequestDecision([]*PermissionRequestDecision{nil}); got != nil {
			t.Errorf("got %+v", got)
		}
	})
}

func TestParsePermissionRequestInvalidReasons(t *testing.T) {
	tests := []struct {
		name string
		json string
		want string
	}{
		{"reserved updatedInput", `{"continue":true,"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"allow","updatedInput":{}}}}`, "PermissionRequest hook returned unsupported updatedInput"},
		{"reserved updatedPermissions", `{"continue":true,"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"allow","updatedPermissions":{}}}}`, "PermissionRequest hook returned unsupported updatedPermissions"},
		{"reserved interrupt", `{"continue":true,"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"allow","interrupt":true}}}`, "PermissionRequest hook returned unsupported interrupt:true"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, ok := parsePermissionRequest(tt.json)
			if !ok {
				t.Fatalf("expected parse")
			}
			if parsed.invalidReason == nil || *parsed.invalidReason != tt.want {
				t.Errorf("invalidReason = %v, want %q", parsed.invalidReason, tt.want)
			}
		})
	}
}

func TestParsePermissionRequestCompleted(t *testing.T) {
	handler := makeHandler(t, protocol.HookEventNamePermissionRequest, sp("^Bash$"), "echo", 0)

	t.Run("allow", func(t *testing.T) {
		parsed := parsePermissionRequestCompleted(handler, runResult(exitPtr(0), `{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"allow"}}}`, ""), sp("t"))
		if parsed.data.decision == nil || parsed.data.decision.Kind != PermissionRequestAllow {
			t.Errorf("decision = %+v", parsed.data.decision)
		}
	})
	t.Run("deny via stdout", func(t *testing.T) {
		parsed := parsePermissionRequestCompleted(handler, runResult(exitPtr(0), `{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"deny","message":"nope"}}}`, ""), sp("t"))
		if parsed.completed.Run.Status != protocol.HookRunStatusBlocked {
			t.Errorf("status = %s", parsed.completed.Run.Status)
		}
		if parsed.data.decision == nil || parsed.data.decision.Kind != PermissionRequestDeny || parsed.data.decision.Message != "nope" {
			t.Errorf("decision = %+v", parsed.data.decision)
		}
	})
	t.Run("deny via exit 2", func(t *testing.T) {
		parsed := parsePermissionRequestCompleted(handler, runResult(exitPtr(2), "", "blocked"), sp("t"))
		if parsed.data.decision == nil || parsed.data.decision.Kind != PermissionRequestDeny || parsed.data.decision.Message != "blocked" {
			t.Errorf("decision = %+v", parsed.data.decision)
		}
	})
}

func TestParseSessionStartCompleted(t *testing.T) {
	sessionHandler := makeHandler(t, protocol.HookEventNameSessionStart, nil, "echo", 0)
	subagentHandler := makeHandler(t, protocol.HookEventNameSubagentStart, nil, "echo", 0)

	t.Run("plain stdout becomes context", func(t *testing.T) {
		parsed := parseSessionStartCompleted(sessionHandler, runResult(exitPtr(0), "hello from hook\n", ""), nil)
		if !slicesEq(parsed.data.additionalContextsModel, []string{"hello from hook"}) {
			t.Errorf("contexts = %v", parsed.data.additionalContextsModel)
		}
		want := []protocol.HookOutputEntry{{Kind: protocol.HookOutputEntryKindContext, Text: "hello from hook"}}
		if !entriesEqual(parsed.completed.Run.Entries, want) {
			t.Errorf("entries = %v", parsed.completed.Run.Entries)
		}
	})

	t.Run("continue false preserves context", func(t *testing.T) {
		parsed := parseSessionStartCompleted(sessionHandler, runResult(exitPtr(0), `{"continue":false,"stopReason":"pause","hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"do not inject"}}`, ""), nil)
		if !parsed.data.shouldStop || !ptrEq(parsed.data.stopReason, sp("pause")) {
			t.Errorf("stop = %v %v", parsed.data.shouldStop, parsed.data.stopReason)
		}
		if !slicesEq(parsed.data.additionalContextsModel, []string{"do not inject"}) {
			t.Errorf("contexts = %v", parsed.data.additionalContextsModel)
		}
		want := []protocol.HookOutputEntry{
			{Kind: protocol.HookOutputEntryKindContext, Text: "do not inject"},
			stopEntry("pause"),
		}
		if !entriesEqual(parsed.completed.Run.Entries, want) {
			t.Errorf("entries = %v", parsed.completed.Run.Entries)
		}
	})

	t.Run("invalid json fails", func(t *testing.T) {
		parsed := parseSessionStartCompleted(sessionHandler, runResult(exitPtr(0), `{"hookSpecificOutput":{"hookEventName":"SessionStart"`, ""), nil)
		if parsed.completed.Run.Status != protocol.HookRunStatusFailed {
			t.Errorf("status = %s", parsed.completed.Run.Status)
		}
	})

	t.Run("subagent continue false ignored", func(t *testing.T) {
		parsed := parseSessionStartCompleted(subagentHandler, runResult(exitPtr(0), `{"continue":false,"stopReason":"skip child","hookSpecificOutput":{"hookEventName":"SubagentStart","additionalContext":"child context"}}`, ""), sp("turn-1"))
		if parsed.data.shouldStop {
			t.Errorf("subagent should not stop")
		}
		if !slicesEq(parsed.data.additionalContextsModel, []string{"child context"}) {
			t.Errorf("contexts = %v", parsed.data.additionalContextsModel)
		}
		if parsed.completed.Run.Status != protocol.HookRunStatusCompleted {
			t.Errorf("status = %s", parsed.completed.Run.Status)
		}
	})
}

func TestParseUserPromptSubmitCompleted(t *testing.T) {
	handler := makeHandler(t, protocol.HookEventNameUserPromptSubmit, nil, "echo", 0)

	t.Run("continue false", func(t *testing.T) {
		parsed := parseUserPromptSubmitCompleted(handler, runResult(exitPtr(0), `{"continue":false,"stopReason":"pause","hookSpecificOutput":{"hookEventName":"UserPromptSubmit","additionalContext":"do not inject"}}`, ""), sp("turn-1"))
		if !parsed.data.shouldStop || !ptrEq(parsed.data.stopReason, sp("pause")) {
			t.Errorf("stop = %v %v", parsed.data.shouldStop, parsed.data.stopReason)
		}
		want := []protocol.HookOutputEntry{
			{Kind: protocol.HookOutputEntryKindContext, Text: "do not inject"},
			stopEntry("pause"),
		}
		if !entriesEqual(parsed.completed.Run.Entries, want) {
			t.Errorf("entries = %v", parsed.completed.Run.Entries)
		}
	})

	t.Run("block decision", func(t *testing.T) {
		parsed := parseUserPromptSubmitCompleted(handler, runResult(exitPtr(0), `{"decision":"block","reason":"slow down","hookSpecificOutput":{"hookEventName":"UserPromptSubmit","additionalContext":"do not inject"}}`, ""), sp("turn-1"))
		if parsed.completed.Run.Status != protocol.HookRunStatusBlocked {
			t.Errorf("status = %s", parsed.completed.Run.Status)
		}
		if !parsed.data.shouldStop || !ptrEq(parsed.data.stopReason, sp("slow down")) {
			t.Errorf("stop = %v %v", parsed.data.shouldStop, parsed.data.stopReason)
		}
	})

	t.Run("block requires reason", func(t *testing.T) {
		parsed := parseUserPromptSubmitCompleted(handler, runResult(exitPtr(0), `{"decision":"block","hookSpecificOutput":{"hookEventName":"UserPromptSubmit","additionalContext":"do not inject"}}`, ""), sp("turn-1"))
		if parsed.completed.Run.Status != protocol.HookRunStatusFailed {
			t.Errorf("status = %s", parsed.completed.Run.Status)
		}
		want := []protocol.HookOutputEntry{errorEntry("UserPromptSubmit hook returned decision:block without a non-empty reason")}
		if !entriesEqual(parsed.completed.Run.Entries, want) {
			t.Errorf("entries = %v", parsed.completed.Run.Entries)
		}
	})

	t.Run("exit 2 blocks", func(t *testing.T) {
		parsed := parseUserPromptSubmitCompleted(handler, runResult(exitPtr(2), "", "blocked by policy\n"), sp("turn-1"))
		if parsed.completed.Run.Status != protocol.HookRunStatusBlocked {
			t.Errorf("status = %s", parsed.completed.Run.Status)
		}
		if !parsed.data.shouldStop || !ptrEq(parsed.data.stopReason, sp("blocked by policy")) {
			t.Errorf("stop = %v %v", parsed.data.shouldStop, parsed.data.stopReason)
		}
	})
}
