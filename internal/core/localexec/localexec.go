// Package localexec holds the built-in tool executors that run commands and
// write files on the local machine: exec_command / write_stdin (the unified
// exec PTY session pair), shell_command, apply_patch, and the sandbox policy /
// escalation logic they share. It is layered on top of core through the
// exported executor contract ([core.ToolExecutor], [core.ToolHandlerContext],
// [core.SessionArmer]) so that core itself stays free of the process-spawning
// stack (internal/unifiedexec, execserver, sandbox, pty, shellcmd parsing).
//
// Hosts that run local commands (the CLI, app-server, MCP server) assemble
// these executors into [core.BuiltinToolDeps] via [ShellExecutors] and
// [NewApplyPatchExecutor]; hosts that must never execute local commands
// (airush's hosted agent runtime, AD-9) simply do not import this package.
//
// This split is a codexgo-only structural deviation from the Rust crate layout
// (codex-core owns tools/handlers/{shell,unified_exec,apply_patch} directly);
// see DEVIATIONS.md (spec 50 D0.9).
package localexec

import (
	"github.com/sqlrush/codexgo/internal/applypatch"
	"github.com/sqlrush/codexgo/internal/core"
	"github.com/sqlrush/codexgo/internal/unifiedexec"
)

// Deps bundles the local-execution dependencies. Nil fields disable the
// corresponding tools: a nil UnifiedExec omits exec_command/write_stdin, a nil
// Exec omits shell_command.
type Deps struct {
	// Exec runs sandboxed shell commands (shell_command / apply_patch fallback).
	Exec ExecService
	// UnifiedExec drives the exec_command/write_stdin PTY session pair. A nil
	// value omits the UnifiedExec tools (the turn then falls back to
	// shell_command regardless of the feature-resolved shell type).
	UnifiedExec *unifiedexec.Executor
	// PatchFS is the filesystem apply_patch writes to; nil uses the real OS FS.
	PatchFS applypatch.FileSystem
}

// ShellExecutors builds the shell-family executors in codex's
// spec_plan::add_shell_tools registration order: exec_command, write_stdin,
// then shell_command. All registered shell executors stay in the router; the
// per-turn shell type decides which advertises a spec (shell_command is
// dispatch-only in UnifiedExec mode). Assign the result to
// [core.BuiltinToolDeps.ShellTools].
func ShellExecutors(deps Deps) []core.ToolExecutor {
	var execs []core.ToolExecutor
	if deps.UnifiedExec != nil {
		execs = append(execs, NewUnifiedExecCommandExecutor(deps.UnifiedExec, deps.PatchFS))
		execs = append(execs, NewWriteStdinExecutor(deps.UnifiedExec))
	}
	if deps.Exec != nil {
		// shell_command takes a `command` STRING, wraps it in the user's shell,
		// and intercepts apply_patch heredocs.
		execs = append(execs, NewShellCommandExecutor(deps.Exec, deps.PatchFS))
	}
	return execs
}

// NewApplyPatchExecutor returns the standalone apply_patch executor writing to
// fs (nil = the real OS filesystem). Assign it to
// [core.BuiltinToolDeps.ApplyPatch].
func NewApplyPatchExecutor(fs applypatch.FileSystem) core.ToolExecutor {
	return applyPatchExecutor{fs: fs}
}
