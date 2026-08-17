package hooks

import (
	"encoding/json"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

func runResult(exitCode *int, stdout, stderr string) commandRunResult {
	return commandRunResult{
		startedAt:   1,
		completedAt: 2,
		durationMs:  1,
		exitCode:    exitCode,
		stdout:      stdout,
		stderr:      stderr,
	}
}

func exitPtr(c int) *int { return &c }

func entriesEqual(a, b []protocol.HookOutputEntry) bool {
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

func TestParsePreToolUseCompleted(t *testing.T) {
	handler := makeHandler(t, protocol.HookEventNamePreToolUse, sp("^Bash$"), "echo hook", 0)

	tests := []struct {
		name            string
		result          commandRunResult
		wantStatus      protocol.HookRunStatus
		wantShouldBlock bool
		wantBlockReason *string
		wantContexts    []string
		wantUpdated     string // raw json or ""
		wantEntries     []protocol.HookOutputEntry
	}{
		{
			name:            "permissionDecision deny blocks",
			result:          runResult(exitPtr(0), `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"do not run that"}}`, ""),
			wantStatus:      protocol.HookRunStatusBlocked,
			wantShouldBlock: true,
			wantBlockReason: sp("do not run that"),
			wantEntries:     []protocol.HookOutputEntry{feedbackEntry("do not run that")},
		},
		{
			name:        "permissionDecision allow updates input",
			result:      runResult(exitPtr(0), `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow","updatedInput":{"command":"echo rewritten"}}}`, ""),
			wantStatus:  protocol.HookRunStatusCompleted,
			wantUpdated: `{"command":"echo rewritten"}`,
			wantEntries: []protocol.HookOutputEntry{},
		},
		{
			name:        "allow without updatedInput fails open",
			result:      runResult(exitPtr(0), `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow"}}`, ""),
			wantStatus:  protocol.HookRunStatusFailed,
			wantEntries: []protocol.HookOutputEntry{errorEntry("PreToolUse hook returned unsupported permissionDecision:allow")},
		},
		{
			name:            "deprecated block decision",
			result:          runResult(exitPtr(0), `{"decision":"block","reason":"do not run that"}`, ""),
			wantStatus:      protocol.HookRunStatusBlocked,
			wantShouldBlock: true,
			wantBlockReason: sp("do not run that"),
			wantEntries:     []protocol.HookOutputEntry{feedbackEntry("do not run that")},
		},
		{
			name:            "block decision with additional context",
			result:          runResult(exitPtr(0), `{"decision":"block","reason":"do not run that","hookSpecificOutput":{"hookEventName":"PreToolUse","additionalContext":"remember this"}}`, ""),
			wantStatus:      protocol.HookRunStatusBlocked,
			wantShouldBlock: true,
			wantBlockReason: sp("do not run that"),
			wantContexts:    []string{"remember this"},
			wantEntries: []protocol.HookOutputEntry{
				{Kind: protocol.HookOutputEntryKindContext, Text: "remember this"},
				feedbackEntry("do not run that"),
			},
		},
		{
			name:        "unsupported ask fails open",
			result:      runResult(exitPtr(0), `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"ask","permissionDecisionReason":"please confirm"}}`, ""),
			wantStatus:  protocol.HookRunStatusFailed,
			wantEntries: []protocol.HookOutputEntry{errorEntry("PreToolUse hook returned unsupported permissionDecision:ask")},
		},
		{
			name:        "deprecated approve fails open",
			result:      runResult(exitPtr(0), `{"decision":"approve"}`, ""),
			wantStatus:  protocol.HookRunStatusFailed,
			wantEntries: []protocol.HookOutputEntry{errorEntry("PreToolUse hook returned unsupported decision:approve")},
		},
		{
			name:        "plain stdout ignored",
			result:      runResult(exitPtr(0), "hook ran successfully\n", ""),
			wantStatus:  protocol.HookRunStatusCompleted,
			wantEntries: []protocol.HookOutputEntry{},
		},
		{
			name:        "json-like invalid stdout fails",
			result:      runResult(exitPtr(0), "{\"decision\":\n", ""),
			wantStatus:  protocol.HookRunStatusFailed,
			wantEntries: []protocol.HookOutputEntry{errorEntry("hook returned invalid pre-tool-use JSON output")},
		},
		{
			name:            "exit code 2 blocks",
			result:          runResult(exitPtr(2), "", "blocked by policy\n"),
			wantStatus:      protocol.HookRunStatusBlocked,
			wantShouldBlock: true,
			wantBlockReason: sp("blocked by policy"),
			wantEntries:     []protocol.HookOutputEntry{feedbackEntry("blocked by policy")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed := parsePreToolUseCompleted(handler, tt.result, sp("turn-1"))
			if parsed.completed.Run.Status != tt.wantStatus {
				t.Errorf("status = %s, want %s", parsed.completed.Run.Status, tt.wantStatus)
			}
			if parsed.data.shouldBlock != tt.wantShouldBlock {
				t.Errorf("shouldBlock = %v, want %v", parsed.data.shouldBlock, tt.wantShouldBlock)
			}
			if !ptrEq(parsed.data.blockReason, tt.wantBlockReason) {
				t.Errorf("blockReason = %v, want %v", parsed.data.blockReason, tt.wantBlockReason)
			}
			if !slicesEq(parsed.data.additionalContextsModel, tt.wantContexts) {
				t.Errorf("contexts = %v, want %v", parsed.data.additionalContextsModel, tt.wantContexts)
			}
			gotUpdated := ""
			if parsed.data.updatedInput != nil {
				gotUpdated = string(parsed.data.updatedInput)
			}
			if !jsonEq(gotUpdated, tt.wantUpdated) {
				t.Errorf("updatedInput = %q, want %q", gotUpdated, tt.wantUpdated)
			}
			if !entriesEqual(parsed.completed.Run.Entries, tt.wantEntries) {
				t.Errorf("entries = %v, want %v", parsed.completed.Run.Entries, tt.wantEntries)
			}
		})
	}
}

func TestLatestUpdatedInputWins(t *testing.T) {
	handler := makeHandler(t, protocol.HookEventNamePreToolUse, nil, "echo", 0)
	laterConfigured := parsePreToolUseCompleted(handler,
		runResult(exitPtr(0), `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow","updatedInput":{"command":"echo configured later"}}}`, ""),
		sp("turn-1"))
	laterConfigured.completionOrder = 0
	earlierConfigured := parsePreToolUseCompleted(handler,
		runResult(exitPtr(0), `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow","updatedInput":{"command":"echo finished later"}}}`, ""),
		sp("turn-1"))
	earlierConfigured.completionOrder = 1

	got := latestUpdatedInput([]parsedHandler[preToolUseHandlerData]{laterConfigured, earlierConfigured})
	if !jsonEq(string(got), `{"command":"echo finished later"}`) {
		t.Errorf("latestUpdatedInput = %s", got)
	}
}

func ptrEq(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func slicesEq(a, b []string) bool {
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

func jsonEq(a, b string) bool {
	if a == "" || b == "" {
		return a == b
	}
	var av, bv any
	if json.Unmarshal([]byte(a), &av) != nil || json.Unmarshal([]byte(b), &bv) != nil {
		return false
	}
	am, _ := json.Marshal(av)
	bm, _ := json.Marshal(bv)
	return string(am) == string(bm)
}
