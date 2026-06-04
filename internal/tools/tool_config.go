package tools

import (
	"github.com/sqlrush/codexgo/internal/features"
	"github.com/sqlrush/codexgo/internal/modelsmanager"
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/pty"
)

// This file ports the shell-tool selection helpers from codex's
// tools/src/tool_config.rs. The selection decides which shell tool family a
// turn advertises to the model: the UnifiedExec PTY pair (exec_command +
// write_stdin), the classic shell_command tool, or nothing.
//
// The zsh-fork session plumbing (UnifiedExecShellMode::ZshFork) is gated behind
// the under-development shell_zsh_fork feature and is not ported; the Direct
// mode is the only session mode codexgo spawns.

// ShellCommandBackendConfig selects the shell_command execution backend.
// Mirrors Rust `ShellCommandBackendConfig`.
type ShellCommandBackendConfig int

const (
	// ShellCommandBackendClassic runs shell_command through the user's shell.
	ShellCommandBackendClassic ShellCommandBackendConfig = iota
	// ShellCommandBackendZshFork routes shell_command through the zsh exec
	// wrapper (requires both shell_tool and shell_zsh_fork features).
	ShellCommandBackendZshFork
)

// ShellCommandBackendForFeatures resolves the shell_command backend from the
// feature set. Mirrors Rust `shell_command_backend_for_features`.
func ShellCommandBackendForFeatures(f *features.Features) ShellCommandBackendConfig {
	if f.Enabled(features.FeatureShellTool) && f.Enabled(features.FeatureShellZshFork) {
		return ShellCommandBackendZshFork
	}
	return ShellCommandBackendClassic
}

// RequestUserInputAvailableModes lists the collaboration modes in which the
// request_user_input tool may be used: the modes that natively allow it (Plan),
// plus Default when the default_mode_request_user_input feature is enabled.
// Mirrors Rust `request_user_input_available_modes`.
func RequestUserInputAvailableModes(f *features.Features) []protocol.ModeKind {
	modes := make([]protocol.ModeKind, 0, len(protocol.TUIVisibleCollaborationModes))
	for _, mode := range protocol.TUIVisibleCollaborationModes {
		if mode.AllowsRequestUserInput() ||
			(f.Enabled(features.FeatureDefaultModeRequestUserInput) && mode == protocol.ModeKindDefault) {
			modes = append(modes, mode)
		}
	}
	return modes
}

// ShellTypeForModelAndFeatures resolves the effective shell tool family for a
// model under the given feature set. Mirrors Rust
// `shell_type_for_model_and_features` exactly:
//
//  1. shell_tool disabled        -> Disabled (no shell tools at all)
//  2. shell_zsh_fork enabled     -> ShellCommand (zsh-fork backend)
//  3. unified_exec enabled       -> UnifiedExec when a PTY is available,
//     otherwise ShellCommand
//  4. otherwise                  -> the model's own shell_type, with
//     UnifiedExec (feature off) and Default/Local mapped to ShellCommand
//
// Note that an enabled unified_exec feature overrides the model's declared
// shell_type: on platforms with PTY support every model gets the UnifiedExec
// pair, which is why real codex advertises exec_command + write_stdin for
// gpt-5.5 (whose catalog shell_type is "shell_command").
func ShellTypeForModelAndFeatures(modelInfo *modelsmanager.ModelInfo, f *features.Features) modelsmanager.ConfigShellToolType {
	unifiedExecEnabled := f.Enabled(features.FeatureUnifiedExec)

	modelShellType := modelInfo.ShellType
	switch modelShellType {
	case modelsmanager.ConfigShellToolTypeUnifiedExec:
		if !unifiedExecEnabled {
			modelShellType = modelsmanager.ConfigShellToolTypeShellCommand
		}
	case modelsmanager.ConfigShellToolTypeDefault, modelsmanager.ConfigShellToolTypeLocal:
		modelShellType = modelsmanager.ConfigShellToolTypeShellCommand
	}

	switch {
	case !f.Enabled(features.FeatureShellTool):
		return modelsmanager.ConfigShellToolTypeDisabled
	case f.Enabled(features.FeatureShellZshFork):
		return modelsmanager.ConfigShellToolTypeShellCommand
	case unifiedExecEnabled:
		if pty.ConPTYSupported() {
			return modelsmanager.ConfigShellToolTypeUnifiedExec
		}
		return modelsmanager.ConfigShellToolTypeShellCommand
	default:
		return modelShellType
	}
}
