package core

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/shellcmd"
)

// buildSessionInitialContext renders the codex initial-context messages seeded
// into a new thread's history at session start: the developer-role
// `<permissions instructions>` and the user-role `<environment_context>`. Mirrors
// the environment-context subset of Session::build_initial_context. It is only
// invoked when SessionConfiguration.IncludeEnvironmentContext is set (the real
// binary's assembly), so unit tests are unaffected.
func buildSessionInitialContext(cfg SessionConfiguration) []protocol.ResponseItem {
	mode := permissionsSandboxModeFromProtocol(cfg.SandboxMode)
	permissions := RenderPermissionsInstructions(mode, cfg.NetworkAccessEnabled, cfg.ApprovalPolicy.Kind)

	env := RenderEnvironmentContext(EnvironmentContextInput{
		Cwd:         cfg.Cwd,
		Shell:       sessionShellName(cfg.Shell),
		CurrentDate: time.Now().Format("2006-01-02"),
		Timezone:    ianaTimezone(),
		Filesystem:  filesystemContextForMode(cfg.SandboxMode, cfg.Cwd),
	})

	return []protocol.ResponseItem{
		developerTextMessage(permissions),
		userTextMessage(env),
	}
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
// mode. Only read-only is byte-verified against codex today (managed/restricted,
// one read entry for :root); for the other modes the filesystem element is omitted
// (the verified permissions text still conveys the mode) until a capture pins the
// exact XML — tracked in docs/PARITY.md.
func filesystemContextForMode(mode protocol.SandboxMode, cwd string) *FilesystemContext {
	if mode == "" || mode == protocol.SandboxModeReadOnly {
		return ReadOnlyFilesystemContext(cwd)
	}
	return nil
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

// ianaTimezone returns the host IANA timezone name best-effort, mirroring codex's
// iana_time_zone::get_timezone(): the TZ env var, else the /etc/localtime symlink
// target under a zoneinfo root, else "UTC".
func ianaTimezone() string {
	if tz := strings.TrimSpace(os.Getenv("TZ")); tz != "" {
		return tz
	}
	if target, err := os.Readlink("/etc/localtime"); err == nil {
		if idx := strings.LastIndex(target, "zoneinfo/"); idx >= 0 {
			if name := target[idx+len("zoneinfo/"):]; name != "" {
				return filepath.ToSlash(name)
			}
		}
	}
	return "UTC"
}
