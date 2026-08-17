package localexec

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo/internal/applypatch"
	"github.com/sqlrush/codexgo/internal/core"
	"github.com/sqlrush/codexgo/internal/modelsmanager"
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/sandbox"
	"github.com/sqlrush/codexgo/internal/shellcmd"
	"github.com/sqlrush/codexgo/internal/tools"
	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// This file ports codex-core's shell tool handler (tools/handlers/shell.rs +
// shell/shell_command.rs). gpt-5.5 calls the `shell_command` tool with a single
// `command` STRING argument; codex wraps that string in the user's default shell
// (`/bin/zsh -lc <command>`), runs it through the sandbox/exec path, and emits a
// command_execution lifecycle item -- UNLESS the command is an `apply_patch
// <<'EOF' ... EOF` heredoc, in which case it is intercepted and routed to the
// patch applier (emitting a file_change item) instead of literally spawning
// apply_patch (codex's intercept_apply_patch).
//
// The exec_command tool (which takes a `cmd` string and runs in a PTY) is also
// registered for models that use it, sharing the same execution path.

// shellCommandExecutor is the model-visible `shell_command` tool. It runs the
// `command` string through the user's shell via the injected [ExecService],
// intercepting apply_patch heredocs and routing them to internal/applypatch.
type shellCommandExecutor struct {
	exec ExecService
	// fs is the filesystem apply_patch writes to; nil uses the real OS FS.
	fs applypatch.FileSystem
	// toolName is the registry key (shell_command or exec_command). Both share the
	// same execution semantics; only the argument key differs.
	toolName string
	// argKey is the JSON argument carrying the command STRING ("command" for
	// shell_command, "cmd" for exec_command).
	argKey string
}

// NewShellCommandExecutor builds the shell_command executor.
func NewShellCommandExecutor(exec ExecService, fs applypatch.FileSystem) core.ToolExecutor {
	return newShellCommandExecutor(exec, fs)
}

func newShellCommandExecutor(exec ExecService, fs applypatch.FileSystem) shellCommandExecutor {
	return shellCommandExecutor{exec: exec, fs: fs, toolName: "shell_command", argKey: "command"}
}

func (e shellCommandExecutor) Name() protocol.ToolName { return protocol.PlainToolName(e.toolName) }

// Spec advertises shell_command only when the turn's shell type resolves to it.
// In UnifiedExec mode the tool stays registered but hidden — codex keeps the
// legacy shell tool dispatch-only while exec_command/write_stdin are
// model-visible (spec_plan::add_shell_tools, add_dispatch_only). codex derives
// the `login` parameter from config.permissions.allow_login_shell, which
// defaults to true (config/mod.rs).
func (e shellCommandExecutor) Spec(tc *core.TurnContext) (tools.ToolSpec, bool) {
	switch core.TurnShellToolType(tc) {
	case modelsmanager.ConfigShellToolTypeUnifiedExec, modelsmanager.ConfigShellToolTypeDisabled:
		return tools.ToolSpec{}, false
	default:
		return tools.CreateShellCommandTool(tools.CommandToolOptions{AllowLoginShell: true}), true
	}
}

func (shellCommandExecutor) MatchesPayload(p tools.ToolPayload) bool {
	return p.Kind == tools.ToolPayloadKindFunction || p.Kind == tools.ToolPayloadKindCustom
}

// Handle parses the command string, intercepts apply_patch heredocs, and
// otherwise runs the command through the user's shell.
func (e shellCommandExecutor) Handle(ctx context.Context, h *core.ToolHandlerContext) (tools.ToolOutput, error) {
	command, workdirArg, useLoginShell, err := e.parseArgs(h.Payload)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(command) == "" {
		return nil, tools.RespondToModelError(fmt.Sprintf("%s requires a non-empty command", e.toolName))
	}

	cwd := h.Turn.Cwd
	if workdirArg != "" {
		cwd = resolveWorkdir(h.Turn.Cwd, workdirArg)
	}

	shell := shellcmd.DefaultUserShell()
	argv := shell.DeriveExecArgs(command, useLoginShell)

	// Intercept an apply_patch heredoc and route it to the patch applier.
	if hd, ok := shellcmd.ExtractApplyPatchHeredoc(argv); ok {
		out, herr := applyHeredocPatch(h, e.fs, cwd, hd)
		if herr != nil {
			return nil, herr
		}
		return out, nil
	}

	return e.runShellCommand(ctx, h, argv, cwd)
}

// parseArgs decodes the command string, workdir, and login flag from the call
// payload, honoring the executor's argument key (command vs cmd). The login flag
// defaults to true (login shell semantics), matching codex's resolve_use_login_shell
// when login shells are allowed.
func (e shellCommandExecutor) parseArgs(p tools.ToolPayload) (command, workdir string, useLoginShell bool, err error) {
	raw, perr := core.PayloadArguments(p)
	if perr != nil {
		return "", "", false, perr
	}
	// Decode into a generic map so the same code serves both the shell_command
	// (command) and exec_command (cmd) argument keys.
	var generic map[string]json.RawMessage
	if uerr := json.Unmarshal([]byte(raw), &generic); uerr != nil {
		return "", "", false, tools.RespondToModelError(fmt.Sprintf("failed to parse function arguments: %v", uerr))
	}

	commandRaw, ok := generic[e.argKey]
	if !ok {
		return "", "", false, tools.RespondToModelError(fmt.Sprintf("%s requires a %q argument", e.toolName, e.argKey))
	}
	if uerr := json.Unmarshal(commandRaw, &command); uerr != nil {
		return "", "", false, tools.RespondToModelError(fmt.Sprintf("%s %q must be a string", e.toolName, e.argKey))
	}

	if wd, present := generic["workdir"]; present {
		if uerr := json.Unmarshal(wd, &workdir); uerr != nil {
			return "", "", false, tools.RespondToModelError(fmt.Sprintf("%s workdir must be a string", e.toolName))
		}
	}

	useLoginShell = true
	if loginRaw, present := generic["login"]; present {
		var login bool
		if uerr := json.Unmarshal(loginRaw, &login); uerr != nil {
			return "", "", false, tools.RespondToModelError(fmt.Sprintf("%s login must be a boolean", e.toolName))
		}
		useLoginShell = login
	}
	return command, workdir, useLoginShell, nil
}

// runShellCommand executes argv (already wrapped in the user's shell) through the
// ExecService, emitting the exec_command_begin / exec_command_end events that the
// exec JSONL processor renders as the command_execution lifecycle item.
//
// It mirrors the shell handler's begin -> ToolOrchestrator::run -> finish
// ordering: the begin event is emitted ONCE, then the sandboxed attempt runs and
// (on a sandbox denial under a permitting policy) escalates to an unsandboxed
// retry, and the end event is emitted ONCE with the FINAL attempt's result.
func (e shellCommandExecutor) runShellCommand(ctx context.Context, h *core.ToolHandlerContext, argv []string, cwd string) (tools.ToolOutput, error) {
	if h.Session != nil {
		h.Session.SendEvent(h.Turn.SubID, protocol.EventMsg{
			Type: protocol.EventMsgKindExecCommandBegin,
			ExecCommandBegin: &protocol.ExecCommandBeginEvent{
				CallID:  h.CallID,
				TurnID:  h.Turn.SubID,
				Command: argv,
				Cwd:     protocol.AbsolutePath(cwd),
				Source:  protocol.ExecCommandSourceAgent,
			},
		})
	}

	res, runErr := e.runShellWithEscalation(ctx, h, argv, cwd)
	if runErr != nil {
		return nil, tools.RespondToModelError(fmt.Sprintf("exec failed: %v", runErr))
	}

	status := protocol.ExecCommandStatusCompleted
	if res.ExitCode != 0 {
		status = protocol.ExecCommandStatusFailed
	}
	aggregated := res.Stdout + res.Stderr
	if h.Session != nil {
		h.Session.SendEvent(h.Turn.SubID, protocol.EventMsg{
			Type: protocol.EventMsgKindExecCommandEnd,
			ExecCommandEnd: &protocol.ExecCommandEndEvent{
				CallID:           h.CallID,
				TurnID:           h.Turn.SubID,
				Command:          argv,
				Cwd:              protocol.AbsolutePath(cwd),
				Source:           protocol.ExecCommandSourceAgent,
				Stdout:           res.Stdout,
				Stderr:           res.Stderr,
				AggregatedOutput: aggregated,
				ExitCode:         res.ExitCode,
				FormattedOutput:  formatShellToolOutput(res),
				Status:           status,
			},
		})
	}

	return core.NewTextToolOutput(formatShellToolOutput(res), boolPtr(res.ExitCode == 0)), nil
}

// runShellWithEscalation runs the command under the turn's resolved sandbox
// policy and, on a likely sandbox denial under a permitting approval policy,
// prompts for approval and retries unsandboxed. It is the shell-path port of the
// ToolOrchestrator::run attempt loop (first attempt sandboxed; on SandboxErr +
// policy != never -> ask; on approval -> retry with SandboxType none).
//
// Under danger-full-access the first attempt already runs with SandboxType none,
// so isLikelySandboxDenied is always false and the escalation is skipped, keeping
// the danger-full-access differentials byte-identical. Under approval_policy=never
// the escalation surfaces the denial unchanged (wantsNoSandboxApproval is false),
// also preserving the existing never-policy behavior.
func (e shellCommandExecutor) runShellWithEscalation(ctx context.Context, h *core.ToolHandlerContext, argv []string, cwd string) (ExecResult, error) {
	// Resolve and apply the turn's REAL sandbox policy (mirrors codex's
	// ToolOrchestrator): read-only / workspace-write spawn under the platform
	// sandbox; danger-full-access keeps the none backend.
	pol := resolveTurnSandboxPolicy(h.Turn)
	res, runErr := e.exec.Run(ctx, ExecRequest{
		Command:                 argv,
		Cwd:                     cwd,
		SandboxType:             pol.SandboxType,
		FileSystemSandboxPolicy: pol.FileSystemSandboxPolicy,
		NetworkSandboxPolicy:    pol.NetworkSandboxPolicy,
		SandboxPolicyCwd:        h.Turn.Cwd,
	})
	if runErr != nil {
		return ExecResult{}, runErr
	}

	// Detect a likely sandbox denial on the sandboxed attempt and escalate.
	aggregated := res.Stdout + res.Stderr
	if !isLikelySandboxDenied(pol.SandboxType, int(res.ExitCode), res.Stdout, res.Stderr, aggregated) {
		return res, nil
	}

	decision := resolveSandboxEscalation(ctx, h.Session, sandboxEscalationRequest{
		Turn:     h.Turn,
		CallID:   h.CallID,
		Command:  argv,
		Cwd:      cwd,
		ToolName: h.ToolName,
	})
	if decision != sandboxEscalationRetryUnsandboxed {
		// Surface the denial unchanged (Never/OnRequest, denied reads, or the
		// user declined). The model sees the original sandboxed result.
		return res, nil
	}

	// Escalated retry: re-run without the sandbox (SandboxType none).
	retryRes, retryErr := e.exec.Run(ctx, ExecRequest{
		Command:          argv,
		Cwd:              cwd,
		SandboxType:      sandbox.SandboxTypeNone,
		SandboxPolicyCwd: h.Turn.Cwd,
	})
	if retryErr != nil {
		return ExecResult{}, retryErr
	}
	return retryRes, nil
}

// applyHeredocPatch applies an intercepted apply_patch heredoc through
// internal/applypatch, emitting the file_change item lifecycle (item.started /
// item.completed) so the JSONL stream matches codex's intercept_apply_patch
// path. It is shared by the shell_command and exec_command (UnifiedExec)
// executors, both of which intercept the heredoc form.
func applyHeredocPatch(h *core.ToolHandlerContext, fsys applypatch.FileSystem, cwd string, hd shellcmd.ApplyPatchHeredoc) (applyPatchToolOutput, error) {
	effectiveCwd := cwd
	if hd.Workdir != "" {
		effectiveCwd = resolveWorkdir(cwd, hd.Workdir)
	}

	parsed, perr := applypatch.ParsePatch(hd.Body)
	if perr != nil {
		return applyPatchToolOutput{}, tools.RespondToModelError(fmt.Sprintf("apply_patch failed to parse: %v", perr))
	}

	cwdAbs, cerr := abspath.FromAbsolutePath(effectiveCwd)
	if cerr != nil {
		return applyPatchToolOutput{}, tools.RespondToModelError(fmt.Sprintf("invalid cwd for apply_patch: %v", cerr))
	}
	if fsys == nil {
		fsys = applypatch.OSFileSystem{}
	}

	// Build the file_change item up front so begin/complete reuse the same id and
	// the change set is derived from the parsed hunks (codex emits the begin item
	// before writing). The change set is the same regardless of apply success.
	changes := fileChangesFromHunks(parsed.Hunks, cwdAbs)
	itemID := h.CallID

	// The started (begin) item carries no status: the exec mapping fills an
	// absent status with in_progress, matching codex's v2 conversion
	// (`status.map(...).unwrap_or(InProgress)`). The completed item carries the
	// final status.
	if h.Session != nil {
		core.EmitTurnItemStarted(h.Session, h.Turn, fileChangeTurnItem(itemID, changes, nil))
	}

	var stdout, stderr bytes.Buffer
	_, applyErr := applypatch.ApplyHunks(parsed.Hunks, cwdAbs, &stdout, &stderr, fsys)
	if applyErr != nil {
		if h.Session != nil {
			failed := protocol.PatchApplyStatusFailed
			core.EmitTurnItemCompleted(h.Session, h.Turn, fileChangeTurnItem(itemID, changes, &failed))
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = applyErr.Error()
		}
		return applyPatchToolOutput{}, tools.RespondToModelError(msg)
	}

	if h.Session != nil {
		completed := protocol.PatchApplyStatusCompleted
		core.EmitTurnItemCompleted(h.Session, h.Turn, fileChangeTurnItem(itemID, changes, &completed))
	}

	out := strings.TrimSpace(stdout.String())
	if out == "" {
		out = "Success. Updated the following files:"
	}
	return applyPatchToolOutput{text: out}, nil
}

// fileChangeTurnItem builds a FileChange turn item with the given id, change set,
// and optional status. A nil status leaves the item's status absent (the started
// item), which the exec mapping renders as in_progress.
func fileChangeTurnItem(id string, changes map[string]protocol.FileChange, status *protocol.PatchApplyStatus) protocol.TurnItem {
	raw := make(map[string]json.RawMessage, len(changes))
	for path, change := range changes {
		b, err := json.Marshal(change)
		if err != nil {
			continue
		}
		raw[path] = b
	}
	item := &protocol.FileChangeItem{ID: id, Changes: raw}
	if status != nil {
		if sb, err := json.Marshal(*status); err == nil {
			item.Status = sb
		}
	}
	return protocol.TurnItem{Type: protocol.TurnItemKindFileChange, FileChange: item}
}

// fileChangesFromHunks derives the path->FileChange map for a parsed patch's
// hunks, resolving each path against the effective cwd. Only the change KIND is
// surfaced to the file_change item, but the content/diff fields are populated so
// the wire shape matches codex's FileChange.
func fileChangesFromHunks(hunks []applypatch.Hunk, cwd abspath.AbsolutePathBuf) map[string]protocol.FileChange {
	changes := make(map[string]protocol.FileChange, len(hunks))
	for _, hunk := range hunks {
		pathAbs := hunk.ResolvePath(cwd).Path()
		switch hunk.Kind {
		case applypatch.HunkAddFile:
			changes[pathAbs] = protocol.FileChange{Kind: protocol.FileChangeKindAdd, Content: hunk.Contents}
		case applypatch.HunkDeleteFile:
			changes[pathAbs] = protocol.FileChange{Kind: protocol.FileChangeKindDelete}
		case applypatch.HunkUpdateFile:
			fc := protocol.FileChange{Kind: protocol.FileChangeKindUpdate}
			if hunk.HasMovePath {
				dest := abspath.ResolvePathAgainstBase(hunk.MovePath, cwd.Path()).Path()
				fc.MovePath = &dest
			}
			changes[pathAbs] = fc
		}
	}
	return changes
}

// resolveWorkdir resolves a (possibly relative) workdir against the turn cwd,
// mirroring codex's TurnContext::resolve_path. An absolute workdir is used as-is.
func resolveWorkdir(cwd, workdir string) string {
	return abspath.ResolvePathAgainstBase(workdir, cwd).Path()
}

// formatShellToolOutput renders an exec result for the model in codex's shell
// tool format ("Exit code: N\nWall time: 0 seconds\nOutput:\n<output>"),
// matching format_exec_output_str for the ShellTool capture policy.
func formatShellToolOutput(res ExecResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Exit code: %d\n", res.ExitCode)
	b.WriteString("Wall time: 0 seconds\n")
	b.WriteString("Output:\n")
	b.WriteString(res.Stdout)
	if res.Stderr != "" {
		b.WriteString(res.Stderr)
	}
	return b.String()
}
