package hooks

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

func testThreadID() protocol.ThreadID {
	return protocol.NewThreadID("11111111-1111-1111-1111-111111111111")
}

func testCwd(t *testing.T) abspath.AbsolutePathBuf {
	t.Helper()
	return abspath.ResolvePathAgainstBase(t.TempDir(), t.TempDir())
}

// posixHandler builds a handler whose command runs under the default POSIX
// shell. Tests using it must call unixOnly first.
func posixHandler(t *testing.T, event protocol.HookEventName, matcher *string, command string) ConfiguredHandler {
	t.Helper()
	h := makeHandler(t, event, matcher, command, 0)
	h.TimeoutSec = 30
	return h
}

func TestEngineHandlersCopyIsolation(t *testing.T) {
	handlers := []ConfiguredHandler{
		makeHandler(t, protocol.HookEventNameStop, nil, "echo a", 0),
	}
	engine := NewEngine(handlers, CommandShell{})
	handlers[0].Command = "mutated"
	if got := engine.Handlers(); got[0].Command != "echo a" {
		t.Errorf("engine handler mutated by caller: %q", got[0].Command)
	}
}

func TestEngineRunPreToolUseEndToEnd(t *testing.T) {
	unixOnly(t)
	// Deny via permissionDecision and verify the stdin payload carries the
	// canonical tool name and JSON tool_input.
	cmd := `read -r line; printf '%s' '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"blocked"}}'`
	handler := posixHandler(t, protocol.HookEventNamePreToolUse, sp("^Bash$"), cmd)
	engine := NewEngine([]ConfiguredHandler{handler}, CommandShell{})

	out := engine.RunPreToolUse(context.Background(), PreToolUseRequest{
		SessionID:      testThreadID(),
		TurnID:         "turn-1",
		Cwd:            testCwd(t),
		Model:          "gpt-test",
		PermissionMode: "default",
		ToolName:       "Bash",
		ToolUseID:      "call-1",
		ToolInput:      json.RawMessage(`{"command":"echo hi"}`),
	})
	if !out.ShouldBlock {
		t.Fatalf("expected block, got %+v", out)
	}
	if out.BlockReason == nil || *out.BlockReason != "blocked" {
		t.Errorf("blockReason = %v", out.BlockReason)
	}
	if len(out.HookEvents) != 1 {
		t.Fatalf("hookEvents = %d, want 1", len(out.HookEvents))
	}
	wantID := "pre-tool-use:0:" + testSourcePath(t).String() + ":call-1"
	if out.HookEvents[0].Run.ID != wantID {
		t.Errorf("run id = %q, want %q", out.HookEvents[0].Run.ID, wantID)
	}
}

func TestEngineRunPreToolUseNoMatch(t *testing.T) {
	handler := makeHandler(t, protocol.HookEventNamePreToolUse, sp("^Edit$"), "echo unused", 0)
	engine := NewEngine([]ConfiguredHandler{handler}, CommandShell{})
	out := engine.RunPreToolUse(context.Background(), PreToolUseRequest{
		SessionID: testThreadID(),
		TurnID:    "turn-1",
		Cwd:       testCwd(t),
		ToolName:  "Bash",
		ToolUseID: "call-1",
		ToolInput: json.RawMessage(`{}`),
	})
	if out.ShouldBlock || len(out.HookEvents) != 0 {
		t.Errorf("non-matching handler should produce empty outcome: %+v", out)
	}
}

func TestEngineRunPreToolUseUpdatedInput(t *testing.T) {
	unixOnly(t)
	cmd := `cat >/dev/null; printf '%s' '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow","updatedInput":{"command":"echo rewritten"}}}'`
	handler := posixHandler(t, protocol.HookEventNamePreToolUse, nil, cmd)
	engine := NewEngine([]ConfiguredHandler{handler}, CommandShell{})
	out := engine.RunPreToolUse(context.Background(), PreToolUseRequest{
		SessionID: testThreadID(),
		TurnID:    "turn-1",
		Cwd:       testCwd(t),
		ToolName:  "Bash",
		ToolUseID: "call-1",
		ToolInput: json.RawMessage(`{"command":"echo hi"}`),
	})
	if out.ShouldBlock {
		t.Fatalf("should not block on allow+updatedInput")
	}
	if !jsonEq(string(out.UpdatedInput), `{"command":"echo rewritten"}`) {
		t.Errorf("updatedInput = %s", out.UpdatedInput)
	}
}

func TestEngineRunPermissionRequestEndToEnd(t *testing.T) {
	unixOnly(t)
	cmd := `cat >/dev/null; printf '%s' '{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"deny","message":"nope"}}}'`
	handler := posixHandler(t, protocol.HookEventNamePermissionRequest, nil, cmd)
	engine := NewEngine([]ConfiguredHandler{handler}, CommandShell{})
	out := engine.RunPermissionRequest(context.Background(), PermissionRequestRequest{
		SessionID:   testThreadID(),
		TurnID:      "turn-1",
		Cwd:         t.TempDir(),
		ToolName:    "Bash",
		RunIDSuffix: "call-1",
		ToolInput:   json.RawMessage(`{}`),
	})
	if out.Decision == nil || out.Decision.Kind != PermissionRequestDeny || out.Decision.Message != "nope" {
		t.Fatalf("decision = %+v", out.Decision)
	}
}

func TestEngineRunPostToolUseEndToEnd(t *testing.T) {
	unixOnly(t)
	cmd := `cat >/dev/null; printf '%s' '{"decision":"block","reason":"output looked sketchy"}'`
	handler := posixHandler(t, protocol.HookEventNamePostToolUse, nil, cmd)
	engine := NewEngine([]ConfiguredHandler{handler}, CommandShell{})
	out := engine.RunPostToolUse(context.Background(), PostToolUseRequest{
		SessionID:    testThreadID(),
		TurnID:       "turn-1",
		Cwd:          testCwd(t),
		ToolName:     "Bash",
		ToolUseID:    "call-1",
		ToolInput:    json.RawMessage(`{}`),
		ToolResponse: json.RawMessage(`{}`),
	})
	if len(out.HookEvents) != 1 || out.HookEvents[0].Run.Status != protocol.HookRunStatusBlocked {
		t.Fatalf("expected blocked event, got %+v", out.HookEvents)
	}
}

func TestEngineRunSessionStartEndToEnd(t *testing.T) {
	unixOnly(t)
	handler := posixHandler(t, protocol.HookEventNameSessionStart, nil, "printf 'injected context'")
	engine := NewEngine([]ConfiguredHandler{handler}, CommandShell{})
	out := engine.RunSessionStart(context.Background(), SessionStartRequest{
		SessionID: testThreadID(),
		Cwd:       testCwd(t),
		Model:     "gpt-test",
		Target:    StartHookTarget{Kind: StartHookTargetSessionStart, Source: SessionStartStartup},
	}, nil)
	if !slicesEq(out.AdditionalContexts, []string{"injected context"}) {
		t.Errorf("contexts = %v", out.AdditionalContexts)
	}
}

func TestEngineRunUserPromptSubmitEndToEnd(t *testing.T) {
	unixOnly(t)
	cmd := `cat >/dev/null; printf '%s' '{"decision":"block","reason":"slow down"}'`
	handler := posixHandler(t, protocol.HookEventNameUserPromptSubmit, nil, cmd)
	engine := NewEngine([]ConfiguredHandler{handler}, CommandShell{})
	out := engine.RunUserPromptSubmit(context.Background(), UserPromptSubmitRequest{
		SessionID: testThreadID(),
		TurnID:    "turn-1",
		Cwd:       testCwd(t),
		Prompt:    "do the thing",
	})
	if !out.ShouldStop || out.StopReason == nil || *out.StopReason != "slow down" {
		t.Errorf("stop = %v %v", out.ShouldStop, out.StopReason)
	}
}

func TestEngineRunStopEndToEnd(t *testing.T) {
	unixOnly(t)
	cmd := `cat >/dev/null; printf '%s' '{"decision":"block","reason":"keep going"}'`
	handler := posixHandler(t, protocol.HookEventNameStop, nil, cmd)
	engine := NewEngine([]ConfiguredHandler{handler}, CommandShell{})
	out := engine.RunStop(context.Background(), StopRequest{
		SessionID: testThreadID(),
		TurnID:    "turn-1",
		Cwd:       testCwd(t),
		Target:    StopHookTarget{Kind: StopHookTargetStop},
	})
	if !out.ShouldBlock || out.BlockReason == nil || *out.BlockReason != "keep going" {
		t.Fatalf("block = %v %v", out.ShouldBlock, out.BlockReason)
	}
	if len(out.ContinuationFragments) != 1 || out.ContinuationFragments[0].Text != "keep going" {
		t.Errorf("fragments = %+v", out.ContinuationFragments)
	}
}

func TestEngineRunCompactEndToEnd(t *testing.T) {
	unixOnly(t)
	preHandler := posixHandler(t, protocol.HookEventNamePreCompact, sp("manual"), `cat >/dev/null; printf '%s' '{"continue":false,"stopReason":"halt compaction"}'`)
	preEngine := NewEngine([]ConfiguredHandler{preHandler}, CommandShell{})
	preOut := preEngine.RunPreCompact(context.Background(), PreCompactRequest{
		SessionID: testThreadID(),
		TurnID:    "turn-1",
		Cwd:       testCwd(t),
		Trigger:   "manual",
	})
	if !preOut.ShouldStop || preOut.StopReason == nil || *preOut.StopReason != "halt compaction" {
		t.Errorf("pre compact = %v %v", preOut.ShouldStop, preOut.StopReason)
	}

	postHandler := posixHandler(t, protocol.HookEventNamePostCompact, sp("auto"), `cat >/dev/null; printf '%s' '{}'`)
	postEngine := NewEngine([]ConfiguredHandler{postHandler}, CommandShell{})
	postOut := postEngine.RunPostCompact(context.Background(), PostCompactRequest{
		SessionID: testThreadID(),
		TurnID:    "turn-1",
		Cwd:       testCwd(t),
		Trigger:   "auto",
	})
	if postOut.ShouldStop {
		t.Errorf("post compact unexpectedly stopped: %+v", postOut)
	}
	if len(postOut.HookEvents) != 1 || postOut.HookEvents[0].Run.Status != protocol.HookRunStatusCompleted {
		t.Errorf("post compact events = %+v", postOut.HookEvents)
	}
}

func TestEnginePreviewAllEvents(t *testing.T) {
	handlers := []ConfiguredHandler{
		makeHandler(t, protocol.HookEventNamePreToolUse, sp("^Bash$"), "echo", 0),
		makeHandler(t, protocol.HookEventNamePermissionRequest, sp("^Bash$"), "echo", 1),
		makeHandler(t, protocol.HookEventNamePostToolUse, sp("^Bash$"), "echo", 2),
		makeHandler(t, protocol.HookEventNamePreCompact, sp("manual"), "echo", 3),
		makeHandler(t, protocol.HookEventNamePostCompact, sp("manual"), "echo", 4),
		makeHandler(t, protocol.HookEventNameSessionStart, sp("startup"), "echo", 5),
		makeHandler(t, protocol.HookEventNameUserPromptSubmit, nil, "echo", 6),
		makeHandler(t, protocol.HookEventNameStop, nil, "echo", 7),
	}
	engine := NewEngine(handlers, CommandShell{})
	cwd := testCwd(t)

	if got := engine.PreviewPreToolUse(&PreToolUseRequest{ToolName: "Bash", ToolUseID: "x", Cwd: cwd}); len(got) != 1 {
		t.Errorf("preview pre tool use = %d", len(got))
	}
	if got := engine.PreviewPermissionRequest(&PermissionRequestRequest{ToolName: "Bash", RunIDSuffix: "x"}); len(got) != 1 {
		t.Errorf("preview permission request = %d", len(got))
	}
	if got := engine.PreviewPostToolUse(&PostToolUseRequest{ToolName: "Bash", ToolUseID: "x", Cwd: cwd}); len(got) != 1 {
		t.Errorf("preview post tool use = %d", len(got))
	}
	if got := engine.PreviewPreCompact(&PreCompactRequest{Trigger: "manual", Cwd: cwd}); len(got) != 1 {
		t.Errorf("preview pre compact = %d", len(got))
	}
	if got := engine.PreviewPostCompact(&PostCompactRequest{Trigger: "manual", Cwd: cwd}); len(got) != 1 {
		t.Errorf("preview post compact = %d", len(got))
	}
	if got := engine.PreviewSessionStart(&SessionStartRequest{Cwd: cwd, Target: StartHookTarget{Source: SessionStartStartup}}); len(got) != 1 {
		t.Errorf("preview session start = %d", len(got))
	}
	if got := engine.PreviewUserPromptSubmit(&UserPromptSubmitRequest{Cwd: cwd}); len(got) != 1 {
		t.Errorf("preview user prompt submit = %d", len(got))
	}
	if got := engine.PreviewStop(&StopRequest{Cwd: cwd, Target: StopHookTarget{Kind: StopHookTargetStop}}); len(got) != 1 {
		t.Errorf("preview stop = %d", len(got))
	}
}
