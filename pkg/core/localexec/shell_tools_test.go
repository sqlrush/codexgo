package localexec

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/ptycap"
	"github.com/sqlrush/codexgo/internal/unifiedexec"
	"github.com/sqlrush/codexgo/pkg/core"
	"github.com/sqlrush/codexgo/pkg/features"
	"github.com/sqlrush/codexgo/pkg/protocol"
	"github.com/sqlrush/codexgo/pkg/tools"
)

// drainEventKinds collects the kinds of every event already emitted. Events
// reach the fixture channel through a forwarding goroutine, so it waits a short
// idle window between events instead of returning on the first empty read.
func drainEventKinds(events <-chan protocol.Event) []protocol.EventMsgKind {
	var kinds []protocol.EventMsgKind
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return kinds
			}
			kinds = append(kinds, ev.Msg.Type)
		case <-time.After(100 * time.Millisecond):
			return kinds
		}
	}
}

func hasEventKind(kinds []protocol.EventMsgKind, want protocol.EventMsgKind) bool {
	for _, k := range kinds {
		if k == want {
			return true
		}
	}
	return false
}

// newShellRouter builds the production router shape for shell hosts: the
// shell family from ShellExecutors plus the standalone apply_patch executor.
func newShellRouter(t *testing.T, deps Deps) *core.DefaultToolRouter {
	t.Helper()
	r, err := core.BuiltinToolRouter(core.BuiltinToolDeps{
		ShellTools: ShellExecutors(deps),
		ApplyPatch: NewApplyPatchExecutor(deps.PatchFS),
	})
	if err != nil {
		t.Fatalf("BuiltinToolRouter: %v", err)
	}
	return r
}

// TestSpecsForTurn asserts the per-turn shell-type selection: UnifiedExec mode
// advertises the exec_command/write_stdin pair (shell_command stays registered
// dispatch-only), shell mode advertises shell_command. The advertised ORDER
// must match codex's spec_plan order.
func TestSpecsForTurn(t *testing.T) {
	t.Parallel()
	r := newShellRouter(t, Deps{
		Exec:        &mockExecService{},
		UnifiedExec: unifiedexec.NewExecutor(nil),
	})

	tests := []struct {
		name        string
		unifiedExec bool
		wantOrder   []string
	}{
		{
			name:        "unified-exec mode advertises the PTY pair in spec_plan order",
			unifiedExec: true,
			wantOrder:   []string{"exec_command", "write_stdin", "update_plan", "apply_patch", "view_image", "web_search"},
		},
		{
			name:        "shell mode advertises shell_command in spec_plan order",
			unifiedExec: false,
			wantOrder:   []string{"shell_command", "update_plan", "apply_patch", "view_image", "web_search"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tc := newTestTurn("/tmp")
			f := features.NewFeaturesWithDefaults()
			if tt.unifiedExec {
				if !ptycap.ConPTYSupported() {
					t.Skip("no PTY support on this platform")
				}
				f.Enable(features.FeatureUnifiedExec)
			} else {
				f.Disable(features.FeatureUnifiedExec)
			}
			tc.Features = &f

			specs, err := r.SpecsForTurn(context.Background(), tc)
			if err != nil {
				t.Fatalf("SpecsForTurn: %v", err)
			}
			got := make([]string, 0, len(specs))
			for _, s := range specs {
				got = append(got, s.Name())
			}
			if strings.Join(got, ",") != strings.Join(tt.wantOrder, ",") {
				t.Errorf("advertised = %v, want %v", got, tt.wantOrder)
			}
		})
	}
}

// TestShellCommandExecutor exercises the shell_command tool: it wraps the
// `command` STRING in the user's shell, runs it through the ExecService, and
// emits the exec_command_begin/end events. (The exec_command tool is the
// UnifiedExec PTY executor, covered in unified_exec_executor_test.go.)
func TestShellCommandExecutor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		toolName    string
		args        string
		exec        *mockExecService
		wantErr     bool
		wantCommand string // the command STRING that must be the last argv token
		wantCwd     string
	}{
		{
			name:        "shell_command runs command with explicit workdir",
			toolName:    "shell_command",
			args:        `{"command":"echo hi","workdir":"/work"}`,
			exec:        &mockExecService{res: ExecResult{ExitCode: 0, Stdout: "hi\n"}},
			wantCommand: "echo hi",
			wantCwd:     "/work",
		},
		{
			name:        "shell_command defaults workdir to turn cwd",
			toolName:    "shell_command",
			args:        `{"command":"pwd"}`,
			exec:        &mockExecService{res: ExecResult{ExitCode: 0, Stdout: "/tmp\n"}},
			wantCommand: "pwd",
			wantCwd:     "/tmp",
		},
		{
			name:     "empty command errors",
			toolName: "shell_command",
			args:     `{"command":""}`,
			exec:     &mockExecService{},
			wantErr:  true,
		},
		{
			name:     "missing command key errors",
			toolName: "shell_command",
			args:     `{}`,
			exec:     &mockExecService{},
			wantErr:  true,
		},
		{
			name:     "exec error surfaces to model",
			toolName: "shell_command",
			args:     `{"command":"x"}`,
			exec:     &mockExecService{err: errors.New("boom")},
			wantErr:  true,
		},
		{
			name:     "malformed json errors",
			toolName: "shell_command",
			args:     `{`,
			exec:     &mockExecService{},
			wantErr:  true,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sess, events := newTestSession(t)
			ex := newShellCommandExecutor(tt.exec, nil)
			out, err := ex.Handle(context.Background(), &core.ToolHandlerContext{
				Session: sess, Turn: newTestTurn("/tmp"), CallID: "c1",
				ToolName: ex.Name(), Payload: tools.FunctionPayload(tt.args),
			})
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if out == nil {
				t.Fatal("want output, got nil")
			}
			gotArgv := tt.exec.gotReq.Command
			if len(gotArgv) == 0 || gotArgv[len(gotArgv)-1] != tt.wantCommand {
				t.Errorf("exec argv = %v, want last token %q", gotArgv, tt.wantCommand)
			}
			if tt.exec.gotReq.Cwd != tt.wantCwd {
				t.Errorf("exec cwd = %q, want %q", tt.exec.gotReq.Cwd, tt.wantCwd)
			}
			kinds := drainEventKinds(events)
			if !hasEventKind(kinds, protocol.EventMsgKindExecCommandBegin) {
				t.Errorf("missing exec_command_begin, got %v", kinds)
			}
			if !hasEventKind(kinds, protocol.EventMsgKindExecCommandEnd) {
				t.Errorf("missing exec_command_end, got %v", kinds)
			}
		})
	}
}

func TestApplyPatchExecutorAddFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ex := applyPatchExecutor{} // nil FS -> real OS filesystem
	patch := "*** Begin Patch\n*** Add File: new.txt\n+hello\n+world\n*** End Patch"

	tests := []struct {
		name    string
		payload tools.ToolPayload
	}{
		{name: "custom payload (raw patch)", payload: tools.CustomPayload(patch)},
		{name: "function payload (json input)", payload: tools.FunctionPayload(mustMarshal(t, map[string]string{"input": patch}))},
	}
	for i, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			subdir := filepath.Join(dir, fmt.Sprintf("case-%d", i))
			if err := os.MkdirAll(subdir, 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			sess, _ := newTestSession(t)
			out, err := ex.Handle(context.Background(), &core.ToolHandlerContext{
				Session: sess, Turn: newTestTurn(subdir), CallID: "c1",
				ToolName: ex.Name(), Payload: tt.payload,
			})
			if err != nil {
				t.Fatalf("Handle: %v", err)
			}
			if out == nil {
				t.Fatal("want output, got nil")
			}
			got, rerr := os.ReadFile(filepath.Join(subdir, "new.txt"))
			if rerr != nil {
				t.Fatalf("read applied file: %v", rerr)
			}
			if string(got) != "hello\nworld\n" {
				t.Errorf("applied content = %q, want %q", string(got), "hello\nworld\n")
			}
		})
	}
}

func TestApplyPatchExecutorErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ex := applyPatchExecutor{}
	tests := []struct {
		name    string
		payload tools.ToolPayload
	}{
		{name: "empty patch", payload: tools.CustomPayload("   ")},
		{name: "garbage patch", payload: tools.CustomPayload("not a patch")},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sess, _ := newTestSession(t)
			_, err := ex.Handle(context.Background(), &core.ToolHandlerContext{
				Session: sess, Turn: newTestTurn(dir), CallID: "c1",
				ToolName: ex.Name(), Payload: tt.payload,
			})
			if err == nil {
				t.Fatal("want error, got nil")
			}
			var fce *tools.FunctionCallError
			if !errors.As(err, &fce) {
				t.Fatalf("want FunctionCallError, got %v", err)
			}
		})
	}
}

// TestShellCommandApplyPatchHeredoc exercises the apply_patch heredoc
// interception: a shell_command whose `command` is `apply_patch <<'EOF' ... EOF`
// is routed to the patch applier (writing the file) and emits the file_change
// item lifecycle (item_started + item_completed) rather than running the shell.
func TestShellCommandApplyPatchHeredoc(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	patch := "*** Begin Patch\n*** Add File: created.txt\n+hello heredoc\n*** End Patch"
	heredoc := "apply_patch <<'EOF'\n" + patch + "\nEOF\n"
	args := mustMarshal(t, map[string]string{"command": heredoc})

	// A non-nil exec service that would fail the test if the heredoc were run as a
	// shell command instead of intercepted.
	exec := &mockExecService{err: errors.New("apply_patch must be intercepted, not executed")}
	ex := newShellCommandExecutor(exec, nil)

	sess, events := newTestSession(t)
	out, err := ex.Handle(context.Background(), &core.ToolHandlerContext{
		Session: sess, Turn: newTestTurn(dir), CallID: "c1",
		ToolName: ex.Name(), Payload: tools.FunctionPayload(args),
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if out == nil {
		t.Fatal("want output, got nil")
	}

	got, rerr := os.ReadFile(filepath.Join(dir, "created.txt"))
	if rerr != nil {
		t.Fatalf("read patched file: %v", rerr)
	}
	if string(got) != "hello heredoc\n" {
		t.Errorf("applied content = %q, want %q", string(got), "hello heredoc\n")
	}

	kinds := drainEventKinds(events)
	if !hasEventKind(kinds, protocol.EventMsgKindItemStarted) {
		t.Errorf("missing item_started (file_change begin), got %v", kinds)
	}
	if !hasEventKind(kinds, protocol.EventMsgKindItemCompleted) {
		t.Errorf("missing item_completed (file_change end), got %v", kinds)
	}
	// The heredoc must NOT reach the exec service.
	if exec.gotReq.Command != nil {
		t.Errorf("exec service was invoked with %v; the heredoc should be intercepted", exec.gotReq.Command)
	}
}

// TestDispatchEndToEnd routes a parsed function call through the router and folds
// the result back into a response item, exercising the full turn-running path.
func TestDispatchEndToEnd(t *testing.T) {
	t.Parallel()
	sess, _ := newTestSession(t)
	exec := &mockExecService{res: ExecResult{ExitCode: 0, Stdout: "out\n"}}
	r := newShellRouter(t, Deps{Exec: exec})
	call := core.ParsedToolCall{
		ToolName: protocol.PlainToolName("shell_command"),
		CallID:   "c1",
		Payload:  tools.FunctionPayload(`{"command":"echo hi"}`),
	}
	res, err := r.DispatchParsed(context.Background(), sess, newTestTurn("/tmp"), call)
	if err != nil {
		t.Fatalf("DispatchParsed: %v", err)
	}
	item := res.IntoResponse()
	if item.Kind != tools.ResponseInputItemKindFunctionCallOutput {
		t.Fatalf("kind = %v, want FunctionCallOutput", item.Kind)
	}
	if item.CallID != "c1" {
		t.Errorf("call id = %q, want c1", item.CallID)
	}
	tr := res.ToToolResult()
	if !tr.Success {
		t.Errorf("Success = false, want true")
	}
	if !strings.Contains(tr.Output, "Exit code: 0") {
		t.Errorf("output = %q, want exit-code header", tr.Output)
	}
}

// ----------------------------------------------------------------------------
// helpers
// ----------------------------------------------------------------------------
