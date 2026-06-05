package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sqlrush/codexgo/internal/applypatch"
	"github.com/sqlrush/codexgo/internal/features"
	"github.com/sqlrush/codexgo/internal/modelsmanager"
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/shellcmd"
	"github.com/sqlrush/codexgo/internal/tools"
	"github.com/sqlrush/codexgo/internal/unifiedexec"
	"github.com/sqlrush/codexgo/internal/utils/truncation"
)

// This file ports the UnifiedExec tool handlers from codex-core's
// tools/handlers/unified_exec/{exec_command,write_stdin}.rs, bridging the
// model-visible exec_command/write_stdin pair onto the PTY session manager in
// internal/unifiedexec. The selection between this pair and the classic
// shell_command tool follows codex's shell_type_for_model_and_features (ported
// in internal/tools/tool_config.go) per turn.
//
// STUB: the approval/sandbox orchestration (ToolOrchestrator), deferred network
// approvals, background exec-end watcher, and hook payload rewriting are owned
// by other area agents; this bridge spawns directly through the unified-exec
// sandbox spawner with the none backend, matching the local exec service.

// ----------------------------------------------------------------------------
// Per-turn shell tool selection
// ----------------------------------------------------------------------------

// turnFeatures resolves the effective feature set for a turn, defaulting when
// the turn carries none. Mirrors the Rust `turn_context.features.get()`.
func turnFeatures(tc *TurnContext) *features.Features {
	if tc != nil && tc.Features != nil {
		return tc.Features
	}
	defaults := features.NewFeaturesWithDefaults()
	return &defaults
}

// turnModelInfo resolves the typed model metadata for a turn, falling back to
// slug-derived metadata when the opaque ModelInfo payload is absent or not a
// catalog entry.
func turnModelInfo(tc *TurnContext) modelsmanager.ModelInfo {
	if tc != nil {
		switch v := tc.ModelInfo.(type) {
		case modelsmanager.ModelInfo:
			return v
		case *modelsmanager.ModelInfo:
			if v != nil {
				return *v
			}
		}
	}
	slug := ""
	if tc != nil {
		slug = tc.ModelSlug
	}
	return modelsmanager.ModelInfoFromSlug(slug)
}

// turnShellToolType resolves which shell tool family the model sees this turn,
// mirroring spec_plan's add_shell_tools selection via
// shell_type_for_model_and_features.
func turnShellToolType(tc *TurnContext) modelsmanager.ConfigShellToolType {
	mi := turnModelInfo(tc)
	return tools.ShellTypeForModelAndFeatures(&mi, turnFeatures(tc))
}

// turnWebSearchMode resolves the effective web-search mode for a turn,
// mirroring `config.web_search_mode` with codex's default (Cached when
// unconfigured — resolve_web_search_mode falls back to WebSearchMode::Cached).
func turnWebSearchMode(tc *TurnContext) protocol.WebSearchMode {
	if tc != nil && tc.WebSearchMode != "" {
		return tc.WebSearchMode
	}
	return protocol.WebSearchModeCached
}

// turnRequestUserInputEnabled mirrors codex's
// config.experimental_request_user_input_enabled, which defaults to TRUE when
// the [tools.experimental_request_user_input] table is absent
// (resolve_experimental_request_user_input_enabled).
func turnRequestUserInputEnabled(tc *TurnContext) bool {
	if tc == nil || tc.ExperimentalRequestUserInput == nil {
		return true
	}
	return *tc.ExperimentalRequestUserInput
}

// turnTruncationPolicy resolves the model-output truncation policy for a turn
// from the model's catalog metadata (the Rust `turn.truncation_policy`).
func turnTruncationPolicy(tc *TurnContext) truncation.TruncationPolicy {
	mi := turnModelInfo(tc)
	switch mi.TruncationPolicy.Mode {
	case modelsmanager.TruncationModeBytes:
		return truncation.BytesPolicy(int(mi.TruncationPolicy.Limit))
	case modelsmanager.TruncationModeTokens:
		return truncation.TokensPolicy(int(mi.TruncationPolicy.Limit))
	default:
		// Unknown/zero policy: fall back to the unified-exec output cap so the
		// response stays bounded.
		return truncation.TokensPolicy(unifiedexec.UnifiedExecOutputMaxTokens)
	}
}

// ----------------------------------------------------------------------------
// exec_command (UnifiedExec) executor
// ----------------------------------------------------------------------------

// unifiedExecCommandArgs mirrors the Rust `ExecCommandArgs` (handlers/
// unified_exec.rs): the required `cmd` plus the optional shell/PTY/yield
// parameters. The approval fields are accepted but unused until the approval
// orchestrator area lands (kept so well-formed calls never fail to parse).
type unifiedExecCommandArgs struct {
	Cmd                   string          `json:"cmd"`
	Workdir               *string         `json:"workdir"`
	Shell                 *string         `json:"shell"`
	Login                 *bool           `json:"login"`
	TTY                   bool            `json:"tty"`
	YieldTimeMS           *uint64         `json:"yield_time_ms"`
	MaxOutputTokens       *int            `json:"max_output_tokens"`
	EnvironmentID         *string         `json:"environment_id"`
	SandboxPermissions    json.RawMessage `json:"sandbox_permissions"`
	AdditionalPermissions json.RawMessage `json:"additional_permissions"`
	Justification         *string         `json:"justification"`
	PrefixRule            []string        `json:"prefix_rule"`
}

// defaultExecYieldTimeMS mirrors default_exec_yield_time_ms.
const defaultExecYieldTimeMS uint64 = 10_000

// defaultWriteStdinYieldTimeMS mirrors default_write_stdin_yield_time_ms.
const defaultWriteStdinYieldTimeMS uint64 = 250

// unifiedExecCommandExecutor is the model-visible `exec_command` tool backed by
// the unified-exec PTY session manager. It is the Go analogue of the Rust
// `ExecCommandHandler`.
type unifiedExecCommandExecutor struct {
	exec *unifiedexec.Executor
	// fs is the filesystem apply_patch heredocs write to; nil uses the OS FS.
	fs applypatch.FileSystem
}

// newUnifiedExecCommandExecutor builds the exec_command (UnifiedExec) executor.
func newUnifiedExecCommandExecutor(exec *unifiedexec.Executor, fs applypatch.FileSystem) unifiedExecCommandExecutor {
	return unifiedExecCommandExecutor{exec: exec, fs: fs}
}

func (unifiedExecCommandExecutor) Name() protocol.ToolName {
	return protocol.PlainToolName("exec_command")
}

// Spec advertises exec_command only when the turn's shell type resolves to
// UnifiedExec (spec_plan::add_shell_tools). codex derives `login` from
// config.permissions.allow_login_shell, which defaults to true.
func (unifiedExecCommandExecutor) Spec(tc *TurnContext) (tools.ToolSpec, bool) {
	if turnShellToolType(tc) != modelsmanager.ConfigShellToolTypeUnifiedExec {
		return tools.ToolSpec{}, false
	}
	includeEnvironmentID := tc != nil && len(tc.Environments) > 1
	return tools.CreateExecCommandToolWithEnvironmentID(
		tools.CommandToolOptions{AllowLoginShell: true}, includeEnvironmentID), true
}

// MatchesPayload accepts only function payloads (the Rust matches_kind).
func (unifiedExecCommandExecutor) MatchesPayload(p tools.ToolPayload) bool {
	return p.Kind == tools.ToolPayloadKindFunction
}

// Handle opens a unified-exec session for the command, intercepting apply_patch
// heredocs, and returns the initial output snapshot (with a live session id when
// the process is still running).
func (e unifiedExecCommandExecutor) Handle(ctx context.Context, h *toolHandlerContext) (tools.ToolOutput, error) {
	args, err := parseUnifiedExecCommandArgs(h.Payload)
	if err != nil {
		return nil, err
	}

	cwd := h.Turn.Cwd
	if args.Workdir != nil && *args.Workdir != "" {
		cwd = resolveWorkdir(h.Turn.Cwd, *args.Workdir)
	}

	// codex reads allow_login_shell from config.permissions (default true);
	// mirror the default used by the advertised spec.
	const allowLoginShell = true
	argv, cmdErr := resolveUnifiedExecCommand(args, allowLoginShell)
	if cmdErr != nil {
		return nil, tools.RespondToModelError(cmdErr.Error())
	}

	processID := e.exec.Manager().AllocateProcessID()

	// Intercept apply_patch heredocs and route them to the patch applier,
	// releasing the reserved session id (intercept_apply_patch).
	if hd, ok := shellcmd.ExtractApplyPatchHeredoc(argv); ok {
		e.exec.Manager().ReleaseProcessID(processID)
		patchOut, perr := applyHeredocPatch(h, e.fs, cwd, hd)
		if perr != nil {
			return nil, perr
		}
		// Mirror the Rust intercept response: an ExecCommandToolOutput with no
		// chunk id, zero wall time, and no exit/session/original-token fields.
		return unifiedExecToolOutput{
			rawOutput:        []byte(patchOut.text),
			maxOutputTokens:  args.MaxOutputTokens,
			truncationPolicy: turnTruncationPolicy(h.Turn),
		}, nil
	}

	yieldTimeMS := defaultExecYieldTimeMS
	if args.YieldTimeMS != nil {
		yieldTimeMS = *args.YieldTimeMS
	}

	emitExecCommandBegin(h, argv, cwd)

	out, execErr := e.exec.ExecCommand(ctx, &unifiedexec.ExecCommandRequest{
		Command:         argv,
		HookCommand:     args.Cmd,
		CallID:          h.CallID,
		ProcessID:       processID,
		YieldTimeMS:     yieldTimeMS,
		MaxOutputTokens: args.MaxOutputTokens,
		Cwd:             cwd,
		SandboxCwd:      h.Turn.Cwd,
		Env:             execEnvironMap(),
		TTY:             args.TTY,
		// SandboxType none: sandbox policy resolution for unified exec is owned
		// by the permissions/sandbox area (STUB), matching the local exec service.
	})
	if execErr != nil {
		// Sandbox denial is terminal but still carries the captured output, so it
		// is rendered as a tool response rather than an error (the Rust
		// UnifiedExecError::SandboxDenied arm).
		var ue *unifiedexec.Error
		if errors.As(execErr, &ue) && ue.Kind == unifiedexec.ErrSandboxDenied && ue.Output != nil {
			deniedText := ue.Output.AggregatedText
			exitCode := ue.Output.ExitCode
			originalTokens := truncation.ApproxTokenCount(deniedText)
			emitExecCommandEnd(h, argv, cwd, deniedText, exitCode)
			return unifiedExecToolOutput{
				chunkID:            unifiedexec.GenerateChunkID(),
				rawOutput:          []byte(deniedText),
				maxOutputTokens:    args.MaxOutputTokens,
				exitCode:           &exitCode,
				originalTokenCount: &originalTokens,
				truncationPolicy:   turnTruncationPolicy(h.Turn),
			}, nil
		}
		return nil, tools.RespondToModelError(fmt.Sprintf(
			"exec_command failed for `%s`: %v", shellcmd.ShlexJoin(argv), execErr))
	}

	// Emit the end event when the process finished within this call. A live
	// session keeps running in the background; its end event is owned by the
	// background watcher (STUB: async_watcher.rs is not ported yet).
	if out.ProcessID == nil && out.ExitCode != nil {
		emitExecCommandEnd(h, argv, cwd, string(out.RawOutput), *out.ExitCode)
	}

	return newUnifiedExecToolOutput(out, turnTruncationPolicy(h.Turn)), nil
}

// parseUnifiedExecCommandArgs decodes and validates exec_command arguments.
func parseUnifiedExecCommandArgs(p tools.ToolPayload) (unifiedExecCommandArgs, error) {
	raw, perr := payloadArguments(p)
	if perr != nil {
		return unifiedExecCommandArgs{}, perr
	}
	var args unifiedExecCommandArgs
	if uerr := json.Unmarshal([]byte(raw), &args); uerr != nil {
		return unifiedExecCommandArgs{}, tools.RespondToModelError(
			fmt.Sprintf("failed to parse function arguments: %v", uerr))
	}
	if args.Cmd == "" {
		return unifiedExecCommandArgs{}, tools.RespondToModelError(
			"failed to parse function arguments: missing required `cmd`")
	}
	return args, nil
}

// resolveUnifiedExecCommand renders the argv for an exec_command call,
// mirroring the Rust `get_command` (Direct mode: the zsh-fork session wrapper
// is feature-gated off). A model-provided `shell` overrides the user shell.
func resolveUnifiedExecCommand(args unifiedExecCommandArgs, allowLoginShell bool) ([]string, error) {
	useLoginShell := allowLoginShell
	if args.Login != nil {
		if *args.Login && !allowLoginShell {
			return nil, errors.New("login shell is disabled by config; omit `login` or set it to false.")
		}
		useLoginShell = *args.Login
	}

	shell := shellcmd.DefaultUserShell()
	if args.Shell != nil && *args.Shell != "" {
		shellType, ok := shellcmd.DetectShellType(*args.Shell)
		if !ok {
			shellType = shellcmd.ShellTypeUnknown
		}
		shell = shellcmd.UserShell{Type: shellType, Path: *args.Shell}
	}
	return shell.DeriveExecArgs(args.Cmd, useLoginShell), nil
}

// execEnvironMap snapshots the parent environment for the spawned PTY child.
//
// STUB: codex applies the shell-environment policy (env_clear/include rules,
// owned by the environment area); the reduced bridge inherits the process env
// like the local exec service does.
func execEnvironMap() map[string]string {
	env := os.Environ()
	out := make(map[string]string, len(env))
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i > 0 {
			out[kv[:i]] = kv[i+1:]
		}
	}
	return out
}

// emitExecCommandBegin emits the exec_command_begin lifecycle event (shared
// shape with the shell_command path so the exec JSONL mapping stays uniform).
func emitExecCommandBegin(h *toolHandlerContext, argv []string, cwd string) {
	if h.Session == nil {
		return
	}
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

// emitExecCommandEnd emits the exec_command_end lifecycle event for a process
// that finished within the current call. PTY output is a single merged stream,
// so it is reported as stdout/aggregated output.
func emitExecCommandEnd(h *toolHandlerContext, argv []string, cwd, text string, exitCode int) {
	if h.Session == nil {
		return
	}
	status := protocol.ExecCommandStatusCompleted
	if exitCode != 0 {
		status = protocol.ExecCommandStatusFailed
	}
	h.Session.SendEvent(h.Turn.SubID, protocol.EventMsg{
		Type: protocol.EventMsgKindExecCommandEnd,
		ExecCommandEnd: &protocol.ExecCommandEndEvent{
			CallID:           h.CallID,
			TurnID:           h.Turn.SubID,
			Command:          argv,
			Cwd:              protocol.AbsolutePath(cwd),
			Source:           protocol.ExecCommandSourceAgent,
			Stdout:           text,
			AggregatedOutput: text,
			ExitCode:         int32(exitCode),
			FormattedOutput:  text,
			Status:           status,
		},
	})
}

// ----------------------------------------------------------------------------
// write_stdin executor
// ----------------------------------------------------------------------------

// writeStdinArgs mirrors the Rust `WriteStdinArgs`: the required session id
// (the model is trained on `session_id`) plus optional input and yield window.
type writeStdinArgs struct {
	SessionID       *int    `json:"session_id"`
	Chars           string  `json:"chars"`
	YieldTimeMS     *uint64 `json:"yield_time_ms"`
	MaxOutputTokens *int    `json:"max_output_tokens"`
}

// writeStdinExecutor is the model-visible `write_stdin` tool that feeds (or
// polls) a live unified-exec session. It is the Go analogue of the Rust
// `WriteStdinHandler`.
type writeStdinExecutor struct {
	exec *unifiedexec.Executor
}

// newWriteStdinExecutor builds the write_stdin executor.
func newWriteStdinExecutor(exec *unifiedexec.Executor) writeStdinExecutor {
	return writeStdinExecutor{exec: exec}
}

func (writeStdinExecutor) Name() protocol.ToolName {
	return protocol.PlainToolName("write_stdin")
}

// Spec advertises write_stdin alongside exec_command in UnifiedExec mode only.
func (writeStdinExecutor) Spec(tc *TurnContext) (tools.ToolSpec, bool) {
	if turnShellToolType(tc) != modelsmanager.ConfigShellToolTypeUnifiedExec {
		return tools.ToolSpec{}, false
	}
	return tools.CreateWriteStdinTool(), true
}

// MatchesPayload accepts only function payloads.
func (writeStdinExecutor) MatchesPayload(p tools.ToolPayload) bool {
	return p.Kind == tools.ToolPayloadKindFunction
}

// Handle writes the input to the live session (or polls when empty) and returns
// the collected output snapshot.
func (e writeStdinExecutor) Handle(ctx context.Context, h *toolHandlerContext) (tools.ToolOutput, error) {
	raw, perr := payloadArguments(h.Payload)
	if perr != nil {
		return nil, perr
	}
	var args writeStdinArgs
	if uerr := json.Unmarshal([]byte(raw), &args); uerr != nil {
		return nil, tools.RespondToModelError(fmt.Sprintf("failed to parse function arguments: %v", uerr))
	}
	if args.SessionID == nil {
		return nil, tools.RespondToModelError("failed to parse function arguments: missing required `session_id`")
	}

	yieldTimeMS := defaultWriteStdinYieldTimeMS
	if args.YieldTimeMS != nil {
		yieldTimeMS = *args.YieldTimeMS
	}

	out, werr := e.exec.WriteStdin(ctx, &unifiedexec.WriteStdinRequest{
		ProcessID:       *args.SessionID,
		Input:           args.Chars,
		YieldTimeMS:     yieldTimeMS,
		MaxOutputTokens: args.MaxOutputTokens,
	})
	if werr != nil {
		return nil, tools.RespondToModelError(fmt.Sprintf("write_stdin failed: %v", werr))
	}

	// Empty stdin is a background poll, so surface it only while a live process
	// remains; non-empty stdin is a real terminal interaction and stays visible
	// even when it completes the process (write_stdin.rs). The event is tagged
	// with the ORIGINATING exec_command's call id (response.event_call_id),
	// falling back to this call's id when the entry predates call-id tracking.
	if (args.Chars != "" || out.ProcessID != nil) && h.Session != nil {
		pid := *args.SessionID
		if out.ProcessID != nil {
			pid = *out.ProcessID
		}
		eventCallID := out.EventCallID
		if eventCallID == "" {
			eventCallID = h.CallID
		}
		h.Session.SendEvent(h.Turn.SubID, protocol.EventMsg{
			Type: protocol.EventMsgKindTerminalInteraction,
			TerminalInteraction: &protocol.TerminalInteractionEvent{
				CallID:    eventCallID,
				ProcessID: strconv.Itoa(pid),
				Stdin:     args.Chars,
			},
		})
	}

	return newUnifiedExecToolOutput(out, turnTruncationPolicy(h.Turn)), nil
}

// ----------------------------------------------------------------------------
// Unified exec tool output
// ----------------------------------------------------------------------------

// unifiedExecToolOutput renders a unified-exec call result for the model. It is
// the Go analogue of the Rust `ExecCommandToolOutput` (context.rs): an optional
// chunk id, the wall time, exit/session metadata, and the (token-truncated)
// output text.
type unifiedExecToolOutput struct {
	tools.DefaultToolOutput
	chunkID            string
	wallTime           time.Duration
	rawOutput          []byte
	maxOutputTokens    *int
	processID          *int
	exitCode           *int
	originalTokenCount *int
	truncationPolicy   truncation.TruncationPolicy
}

// newUnifiedExecToolOutput wraps a manager Output for model rendering.
func newUnifiedExecToolOutput(out *unifiedexec.Output, policy truncation.TruncationPolicy) unifiedExecToolOutput {
	originalTokens := out.OriginalTokenCount
	return unifiedExecToolOutput{
		chunkID:            out.ChunkID,
		wallTime:           time.Duration(out.WallTime),
		rawOutput:          out.RawOutput,
		maxOutputTokens:    out.MaxOutputTokens,
		processID:          out.ProcessID,
		exitCode:           out.ExitCode,
		originalTokenCount: &originalTokens,
		truncationPolicy:   policy,
	}
}

// modelOutputMaxTokens mirrors ExecCommandToolOutput::model_output_max_tokens:
// the requested cap (default 10000) bounded by the turn truncation budget.
func (o unifiedExecToolOutput) modelOutputMaxTokens() int {
	maxTokens := unifiedexec.DefaultMaxOutputTokens
	if o.maxOutputTokens != nil {
		maxTokens = *o.maxOutputTokens
	}
	if budget := o.truncationPolicy.TokenBudget(); budget < maxTokens {
		maxTokens = budget
	}
	return maxTokens
}

// truncatedOutput mirrors ExecCommandToolOutput::truncated_output.
func (o unifiedExecToolOutput) truncatedOutput() string {
	return truncation.FormattedTruncateText(string(o.rawOutput),
		truncation.TokensPolicy(o.modelOutputMaxTokens()))
}

// responseText mirrors ExecCommandToolOutput::response_text section-for-section.
func (o unifiedExecToolOutput) responseText() string {
	sections := make([]string, 0, 7)
	if o.chunkID != "" {
		sections = append(sections, "Chunk ID: "+o.chunkID)
	}
	sections = append(sections, fmt.Sprintf("Wall time: %.4f seconds", o.wallTime.Seconds()))
	if o.exitCode != nil {
		sections = append(sections, fmt.Sprintf("Process exited with code %d", *o.exitCode))
	}
	if o.processID != nil {
		sections = append(sections, fmt.Sprintf("Process running with session ID %d", *o.processID))
	}
	if o.originalTokenCount != nil {
		sections = append(sections, fmt.Sprintf("Original token count: %d", *o.originalTokenCount))
	}
	sections = append(sections, "Output:", o.truncatedOutput())
	return strings.Join(sections, "\n")
}

func (o unifiedExecToolOutput) LogPreview() string { return telemetryPreview(o.responseText()) }

// SuccessForLogging mirrors the Rust impl: unified exec responses always log
// as success (failure is carried in the rendered exit code).
func (o unifiedExecToolOutput) SuccessForLogging() bool { return true }

func (o unifiedExecToolOutput) ToResponseItem(callID string, payload tools.ToolPayload) tools.ResponseInputItem {
	text := o.responseText()
	success := true
	out := protocol.FunctionCallOutputPayload{Text: &text, Success: &success}
	if payload.Kind == tools.ToolPayloadKindCustom {
		return tools.CustomToolCallOutputInput(callID, nil, out)
	}
	return tools.FunctionCallOutputInput(callID, out)
}

// CodeModeResult mirrors the Rust UnifiedExecCodeModeResult JSON shape.
func (o unifiedExecToolOutput) CodeModeResult(tools.ToolPayload) json.RawMessage {
	result := struct {
		ChunkID            *string `json:"chunk_id,omitempty"`
		WallTimeSeconds    float64 `json:"wall_time_seconds"`
		ExitCode           *int    `json:"exit_code,omitempty"`
		SessionID          *int    `json:"session_id,omitempty"`
		OriginalTokenCount *int    `json:"original_token_count,omitempty"`
		Output             string  `json:"output"`
	}{
		WallTimeSeconds:    o.wallTime.Seconds(),
		ExitCode:           o.exitCode,
		SessionID:          o.processID,
		OriginalTokenCount: o.originalTokenCount,
		Output:             o.truncatedOutput(),
	}
	if o.chunkID != "" {
		chunk := o.chunkID
		result.ChunkID = &chunk
	}
	b, err := json.Marshal(result)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return b
}
