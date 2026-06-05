package core

import (
	"context"
	"time"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/shellcmd"
)

// buildSessionInitialContext renders the codex initial-context messages seeded
// into a new thread's history at session start: the developer-role
// `<permissions instructions>` (plus the `<skills_instructions>` content part
// when the host renders one) and the user-role `<environment_context>`.
// Mirrors the developer-section assembly of Session::build_initial_context,
// where every developer section becomes one content part of a single
// developer message. It is only invoked when
// SessionConfiguration.IncludeEnvironmentContext is set (the real binary's
// assembly), so unit tests are unaffected.
func buildSessionInitialContext(cfg SessionConfiguration, skillsInstructions *string) []protocol.ResponseItem {
	mode := permissionsSandboxModeFromProtocol(cfg.SandboxMode)
	permissions := RenderPermissionsInstructions(mode, cfg.NetworkAccessEnabled, cfg.ApprovalPolicy.Kind)
	developerSections := []string{permissions}
	if skillsInstructions != nil && *skillsInstructions != "" {
		developerSections = append(developerSections, *skillsInstructions)
	}

	env := RenderEnvironmentContext(EnvironmentContextInput{
		Cwd:         cfg.Cwd,
		Shell:       sessionShellName(cfg.Shell),
		CurrentDate: time.Now().Format("2006-01-02"),
		Timezone:    ianaTimezone(),
		Filesystem:  filesystemContextForMode(cfg.SandboxMode, cfg.Cwd),
	})

	return []protocol.ResponseItem{
		developerMessage(developerSections),
		userTextMessage(env),
	}
}

// developerMessage builds a developer-role message carrying one input-text
// content part per section, mirroring codex's developer-sections bundling.
func developerMessage(sections []string) protocol.ResponseItem {
	content := make([]protocol.ContentItem, 0, len(sections))
	for _, section := range sections {
		content = append(content, protocol.ContentItem{
			Type: protocol.ContentItemKindInputText,
			Text: section,
		})
	}
	return protocol.ResponseItem{
		Type:    protocol.ResponseItemKindMessage,
		Role:    "developer",
		Content: content,
	}
}

// InitialSkillsRenderer is the optional capability a SkillsManager implements
// to render the `<skills_instructions>` developer section for a new thread's
// initial context (the headless analogue of codex's include_skill_instructions
// block in build_initial_context). ok=false means no skills are eligible and
// the section is omitted.
type InitialSkillsRenderer interface {
	// RenderInitialSkillsInstructions scans the skill roots for cwd and renders
	// the tagged skills_instructions text under the model's metadata budget.
	RenderInitialSkillsInstructions(ctx context.Context, cwd string, contextWindow *int64) (string, bool)
}

// permissionsSandboxModeFromProtocol maps a protocol.SandboxMode to the renderer's
// PermissionsSandboxMode, defaulting to read-only for the zero value.
func permissionsSandboxModeFromProtocol(mode protocol.SandboxMode) PermissionsSandboxMode {
	switch mode {
	case protocol.SandboxModeWorkspaceWrite:
		return SandboxModeWorkspaceWrite
	case protocol.SandboxModeDangerFullAccess:
		return SandboxModeDangerFullAccess
	default:
		return SandboxModeReadOnly
	}
}

// filesystemContextForMode returns the <filesystem> description for the sandbox
// mode. All three modes are byte-verified against codex 0.136.0 captures:
//   - read-only / unset: managed/restricted, one read entry for :root
//   - workspace-write: managed/restricted with the default write + read-only
//     carveout entries (no extra writable_roots configured)
//   - danger-full-access: a disabled, unrestricted profile
func filesystemContextForMode(mode protocol.SandboxMode, cwd string) *FilesystemContext {
	switch mode {
	case protocol.SandboxModeWorkspaceWrite:
		return WorkspaceWriteFilesystemContext(cwd)
	case protocol.SandboxModeDangerFullAccess:
		return DangerFullAccessFilesystemContext(cwd)
	default:
		return ReadOnlyFilesystemContext(cwd)
	}
}

// sessionShellName returns the configured shell name, or the host default shell's
// name. Matches codex's Shell::name() values ("zsh"/"bash"/"sh"/"powershell"/"cmd").
func sessionShellName(configured string) string {
	if configured != "" {
		return configured
	}
	switch shellcmd.DefaultUserShell().Type {
	case shellcmd.ShellTypeZsh:
		return "zsh"
	case shellcmd.ShellTypeBash:
		return "bash"
	case shellcmd.ShellTypePowerShell:
		return "powershell"
	case shellcmd.ShellTypeCmd:
		return "cmd"
	default:
		return "sh"
	}
}
