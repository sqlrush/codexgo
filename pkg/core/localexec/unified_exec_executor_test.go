package localexec

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/pty"
	"github.com/sqlrush/codexgo/internal/unifiedexec"
	"github.com/sqlrush/codexgo/internal/utils/truncation"
	"github.com/sqlrush/codexgo/pkg/core"
	"github.com/sqlrush/codexgo/pkg/features"
	"github.com/sqlrush/codexgo/pkg/modelsmanager"
	"github.com/sqlrush/codexgo/pkg/protocol"
	"github.com/sqlrush/codexgo/pkg/tools"
)

// turnWithShellFeatures builds a test turn whose feature set pins the shell
// selection deterministically (platform-independent).
func turnWithShellFeatures(cwd string, unifiedExec bool) *core.TurnContext {
	tc := newTestTurn(cwd)
	f := features.NewFeaturesWithDefaults()
	if unifiedExec {
		f.Enable(features.FeatureUnifiedExec)
	} else {
		f.Disable(features.FeatureUnifiedExec)
	}
	tc.Features = &f
	return tc
}

func requirePTY(t *testing.T) {
	t.Helper()
	if !pty.ConPTYSupported() {
		t.Skip("no PTY support on this platform")
	}
}

// TestUnifiedExecSpecVisibility asserts the exec_command/write_stdin pair is
// advertised only in UnifiedExec mode and shell_command only otherwise,
// mirroring spec_plan::add_shell_tools.
func TestUnifiedExecSpecVisibility(t *testing.T) {
	t.Parallel()
	requirePTY(t)

	execEx := newUnifiedExecCommandExecutor(unifiedexec.NewExecutor(nil), nil)
	stdinEx := newWriteStdinExecutor(unifiedexec.NewExecutor(nil))
	shellEx := newShellCommandExecutor(&mockExecService{}, nil)

	unified := turnWithShellFeatures("/tmp", true)
	if _, ok := execEx.Spec(unified); !ok {
		t.Error("exec_command hidden in unified-exec mode")
	}
	if _, ok := stdinEx.Spec(unified); !ok {
		t.Error("write_stdin hidden in unified-exec mode")
	}
	if _, ok := shellEx.Spec(unified); ok {
		t.Error("shell_command advertised in unified-exec mode (must be dispatch-only)")
	}

	shell := turnWithShellFeatures("/tmp", false)
	if _, ok := execEx.Spec(shell); ok {
		t.Error("exec_command advertised in shell mode")
	}
	if _, ok := stdinEx.Spec(shell); ok {
		t.Error("write_stdin advertised in shell mode")
	}
	if _, ok := shellEx.Spec(shell); !ok {
		t.Error("shell_command hidden in shell mode")
	}

	disabled := turnWithShellFeatures("/tmp", false)
	disabled.Features.Disable(features.FeatureShellTool)
	for name, visible := range map[string]bool{
		"exec_command":  specVisible(execEx, disabled),
		"write_stdin":   specVisible(stdinEx, disabled),
		"shell_command": specVisible(shellEx, disabled),
	} {
		if visible {
			t.Errorf("%s advertised with shell_tool disabled", name)
		}
	}
}

func specVisible(ex core.ToolExecutor, tc *core.TurnContext) bool {
	_, ok := ex.Spec(tc)
	return ok
}

// TestUnifiedExecSpecEnvironmentID asserts the environment_id parameter appears
// only for multi-environment turns.
func TestUnifiedExecSpecEnvironmentID(t *testing.T) {
	t.Parallel()
	requirePTY(t)
	ex := newUnifiedExecCommandExecutor(unifiedexec.NewExecutor(nil), nil)

	tc := turnWithShellFeatures("/tmp", true)
	tc.Environments = []protocol.TurnEnvironmentSelection{{}, {}}
	spec, ok := ex.Spec(tc)
	if !ok {
		t.Fatal("exec_command hidden")
	}
	if _, present := spec.Function.Parameters.Properties["environment_id"]; !present {
		t.Error("environment_id missing for multi-environment turn")
	}

	tc.Environments = nil
	spec, _ = ex.Spec(tc)
	if _, present := spec.Function.Parameters.Properties["environment_id"]; present {
		t.Error("environment_id present for single-environment turn")
	}
}

// TestUnifiedExecArgValidation exercises the argument-parsing error paths.
func TestUnifiedExecArgValidation(t *testing.T) {
	t.Parallel()
	ex := newUnifiedExecCommandExecutor(unifiedexec.NewExecutor(nil), nil)
	stdinEx := newWriteStdinExecutor(unifiedexec.NewExecutor(nil))

	tests := []struct {
		name    string
		exec    core.ToolExecutor
		args    string
		wantSub string
	}{
		{"missing cmd", ex, `{}`, "missing required `cmd`"},
		{"malformed json", ex, `{`, "failed to parse function arguments"},
		{"missing session_id", stdinEx, `{"chars":"y\n"}`, "missing required `session_id`"},
		{"write_stdin unknown session", stdinEx, `{"session_id":424242}`, "write_stdin failed"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := tt.exec.Handle(context.Background(), &core.ToolHandlerContext{
				Turn: turnWithShellFeatures("/tmp", true), CallID: "c1",
				ToolName: tt.exec.Name(), Payload: tools.FunctionPayload(tt.args),
			})
			if err == nil {
				t.Fatal("want error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantSub)
			}
		})
	}
}

// TestResolveUnifiedExecCommandLogin ports the login-shell gate from
// get_command: an explicit login:true is rejected when login shells are
// disallowed, and the allowLoginShell default applies when login is omitted.
func TestResolveUnifiedExecCommandLogin(t *testing.T) {
	t.Parallel()
	boolPtrVal := func(b bool) *bool { return &b }

	if _, err := resolveUnifiedExecCommand(unifiedExecCommandArgs{Cmd: "ls", Login: boolPtrVal(true)}, false); err == nil {
		t.Error("want error for login:true with login shells disallowed")
	}

	argv, err := resolveUnifiedExecCommand(unifiedExecCommandArgs{Cmd: "ls"}, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(argv) != 3 || argv[len(argv)-1] != "ls" {
		t.Errorf("argv = %v, want [shell, flag, ls]", argv)
	}
	// allowLoginShell=true with no explicit login uses the login-shell flag.
	if !strings.Contains(argv[1], "l") {
		t.Errorf("argv flag = %q, want a login flag", argv[1])
	}
}

// TestUnifiedExecCommandRoundTrip opens a real PTY session with exec_command,
// then drives it with write_stdin until the process exits, asserting the
// rendered response sections mirror codex's ExecCommandToolOutput layout.
func TestUnifiedExecCommandRoundTrip(t *testing.T) {
	t.Parallel()
	requirePTY(t)

	executor := unifiedexec.NewExecutor(nil)
	t.Cleanup(executor.Shutdown)
	execEx := newUnifiedExecCommandExecutor(executor, nil)
	stdinEx := newWriteStdinExecutor(executor)
	tc := turnWithShellFeatures(t.TempDir(), true)

	// `cat` keeps running on a PTY until stdin delivers EOF, so the session
	// must survive the first call and report a session id.
	out, err := execEx.Handle(context.Background(), &core.ToolHandlerContext{
		Turn: tc, CallID: "c1", ToolName: execEx.Name(),
		Payload: tools.FunctionPayload(`{"cmd":"cat","tty":true,"yield_time_ms":300}`),
	})
	if err != nil {
		t.Fatalf("exec_command: %v", err)
	}
	first, ok := out.(unifiedExecToolOutput)
	if !ok {
		t.Fatalf("output type = %T, want unifiedExecToolOutput", out)
	}
	if first.processID == nil {
		t.Fatalf("no session id in response:\n%s", first.responseText())
	}
	text := first.responseText()
	if !strings.Contains(text, "Wall time: ") || !strings.Contains(text, "Output:") {
		t.Errorf("response missing sections:\n%s", text)
	}
	if !strings.Contains(text, "Process running with session ID ") {
		t.Errorf("response missing session line:\n%s", text)
	}

	// Write through the live session; cat echoes the input back.
	sessionID := *first.processID
	out, err = stdinEx.Handle(context.Background(), &core.ToolHandlerContext{
		Turn: tc, CallID: "c2", ToolName: stdinEx.Name(),
		Payload: tools.FunctionPayload(
			`{"session_id":` + strconv.Itoa(sessionID) + `,"chars":"hello\n","yield_time_ms":400}`),
	})
	if err != nil {
		t.Fatalf("write_stdin: %v", err)
	}
	second := out.(unifiedExecToolOutput)
	if !strings.Contains(string(second.rawOutput), "hello") {
		t.Errorf("write_stdin output = %q, want echoed hello", string(second.rawOutput))
	}

	_ = executor.Kill(sessionID)
}

// TestUnifiedExecShortLivedCommand runs a command that exits within the yield
// window and asserts the exit code section is rendered (no session id).
func TestUnifiedExecShortLivedCommand(t *testing.T) {
	t.Parallel()
	requirePTY(t)

	executor := unifiedexec.NewExecutor(nil)
	t.Cleanup(executor.Shutdown)
	execEx := newUnifiedExecCommandExecutor(executor, nil)

	out, err := execEx.Handle(context.Background(), &core.ToolHandlerContext{
		Turn: turnWithShellFeatures(t.TempDir(), true), CallID: "c1",
		ToolName: execEx.Name(),
		Payload:  tools.FunctionPayload(`{"cmd":"echo unified-bridge","yield_time_ms":3000}`),
	})
	if err != nil {
		t.Fatalf("exec_command: %v", err)
	}
	res := out.(unifiedExecToolOutput)
	text := res.responseText()
	if !strings.Contains(text, "Process exited with code 0") {
		t.Errorf("missing exit section:\n%s", text)
	}
	if strings.Contains(text, "Process running with session ID") {
		t.Errorf("unexpected live session for short-lived command:\n%s", text)
	}
	if !strings.Contains(text, "unified-bridge") {
		t.Errorf("missing command output:\n%s", text)
	}
}

// TestUnifiedExecOutputRendering pins the response_text section layout against
// the Rust ExecCommandToolOutput::response_text.
func TestUnifiedExecOutputRendering(t *testing.T) {
	t.Parallel()
	exit := 0
	pid := 42
	tokens := 3
	o := unifiedExecToolOutput{
		chunkID:            "abc123",
		wallTime:           1500 * time.Millisecond,
		rawOutput:          []byte("hi\n"),
		processID:          &pid,
		exitCode:           &exit,
		originalTokenCount: &tokens,
		truncationPolicy:   truncation.TokensPolicy(10_000),
	}
	want := strings.Join([]string{
		"Chunk ID: abc123",
		"Wall time: 1.5000 seconds",
		"Process exited with code 0",
		"Process running with session ID 42",
		"Original token count: 3",
		"Output:",
		"hi\n",
	}, "\n")
	if got := o.responseText(); got != want {
		t.Errorf("responseText mismatch\n got: %q\nwant: %q", got, want)
	}

	// The heredoc-intercept shape: no chunk/exit/session/token sections.
	intercept := unifiedExecToolOutput{
		rawOutput:        []byte("Success. Updated the following files:"),
		truncationPolicy: truncation.TokensPolicy(10_000),
	}
	wantIntercept := "Wall time: 0.0000 seconds\nOutput:\nSuccess. Updated the following files:"
	if got := intercept.responseText(); got != wantIntercept {
		t.Errorf("intercept responseText mismatch\n got: %q\nwant: %q", got, wantIntercept)
	}
}

// TestTurnShellToolTypeModelInfoFallbacks asserts the helper accepts the typed
// catalog value, a pointer, and the absent case.
func TestTurnShellToolTypeModelInfoFallbacks(t *testing.T) {
	t.Parallel()
	requirePTY(t)

	tc := turnWithShellFeatures("/tmp", true)
	mi := modelsmanager.ModelInfoFromSlug("gpt-5.5")

	tc.ModelInfo = mi
	if got := core.TurnShellToolType(tc); got != modelsmanager.ConfigShellToolTypeUnifiedExec {
		t.Errorf("value ModelInfo: shell type = %q", got)
	}
	tc.ModelInfo = &mi
	if got := core.TurnShellToolType(tc); got != modelsmanager.ConfigShellToolTypeUnifiedExec {
		t.Errorf("pointer ModelInfo: shell type = %q", got)
	}
	tc.ModelInfo = nil
	if got := core.TurnShellToolType(tc); got != modelsmanager.ConfigShellToolTypeUnifiedExec {
		t.Errorf("absent ModelInfo: shell type = %q", got)
	}
}
