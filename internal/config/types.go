package config

import (
	"github.com/sqlrush/codexgo/internal/protocol"
)

// Default constants ported from the Rust config crate.
const (
	DefaultProjectDocMaxBytes = 32 * 1024

	DefaultOtelEnvironment = "dev"

	DefaultMemoriesMaxRolloutsPerStartup              = 2
	DefaultMemoriesMaxRolloutAgeDays                  = 10
	DefaultMemoriesMinRolloutIdleHours                = 6
	DefaultMemoriesMinRateLimitRemainingPercent       = 25
	DefaultMemoriesMaxRawMemoriesForConsolidation     = 256
	DefaultMemoriesMaxUnusedDays                      = 30
	minMemoriesMaxRawMemoriesForConsolidation         = 1
	maxMemoriesMaxRawMemoriesForConsolidation         = 4096
	minMemoriesMaxRolloutsPerStartup                  = 1
	maxMemoriesMaxRolloutsPerStartup                  = 128
	DefaultTerminalResizeReflowFallbackMaxRows    int = 1000

	DefaultMcpServerEnvironmentID = "local"
)

// HistoryPersistence controls whether history entries are written to disk.
// Rust serde rename_all = "kebab-case"; default SaveAll.
type HistoryPersistence string

const (
	HistoryPersistenceSaveAll HistoryPersistence = "save-all"
	HistoryPersistenceNone    HistoryPersistence = "none"
)

// History governs what is written to ~/.codex/history.jsonl. Rust serde
// default applies to all fields; deny_unknown_fields.
type History struct {
	Persistence HistoryPersistence `json:"persistence" toml:"persistence"`
	MaxBytes    *uint64            `json:"max_bytes" toml:"max_bytes"`
}

// DefaultHistory returns the History with serde defaults applied.
func DefaultHistory() History {
	return History{Persistence: HistoryPersistenceSaveAll}
}

// AnalyticsConfigToml configures analytics. deny_unknown_fields; enabled is
// optional so defaults can be applied later.
type AnalyticsConfigToml struct {
	Enabled *bool `json:"enabled" toml:"enabled"`
}

// FeedbackConfigToml configures the feedback flow. deny_unknown_fields.
type FeedbackConfigToml struct {
	Enabled *bool `json:"enabled" toml:"enabled"`
}

// AuthCredentialsStoreMode selects where CLI auth credentials are stored. Rust
// serde rename_all = "lowercase"; default File.
type AuthCredentialsStoreMode string

const (
	AuthCredentialsStoreFile      AuthCredentialsStoreMode = "file"
	AuthCredentialsStoreKeyring   AuthCredentialsStoreMode = "keyring"
	AuthCredentialsStoreAuto      AuthCredentialsStoreMode = "auto"
	AuthCredentialsStoreEphemeral AuthCredentialsStoreMode = "ephemeral"
)

// OAuthCredentialsStoreMode selects where MCP OAuth credentials are stored. Rust
// serde rename_all = "lowercase"; default Auto.
type OAuthCredentialsStoreMode string

const (
	OAuthCredentialsStoreAuto    OAuthCredentialsStoreMode = "auto"
	OAuthCredentialsStoreFile    OAuthCredentialsStoreMode = "file"
	OAuthCredentialsStoreKeyring OAuthCredentialsStoreMode = "keyring"
)

// UriBasedFileOpener selects the editor URI scheme for file citations. Rust
// serde uses explicit renames per variant.
type UriBasedFileOpener string

const (
	UriBasedFileOpenerVsCode         UriBasedFileOpener = "vscode"
	UriBasedFileOpenerVsCodeInsiders UriBasedFileOpener = "vscode-insiders"
	UriBasedFileOpenerWindsurf       UriBasedFileOpener = "windsurf"
	UriBasedFileOpenerCursor         UriBasedFileOpener = "cursor"
	UriBasedFileOpenerNone           UriBasedFileOpener = "none"
)

// Scheme returns the URI scheme for the opener, or nil for None.
func (u UriBasedFileOpener) Scheme() *string {
	switch u {
	case UriBasedFileOpenerVsCode:
		s := "vscode"
		return &s
	case UriBasedFileOpenerVsCodeInsiders:
		s := "vscode-insiders"
		return &s
	case UriBasedFileOpenerWindsurf:
		s := "windsurf"
		return &s
	case UriBasedFileOpenerCursor:
		s := "cursor"
		return &s
	default:
		return nil
	}
}

// SandboxWorkspaceWrite configures workspace-write sandbox mode. deny_unknown_fields.
// All fields are serde default; writable_roots are AbsolutePathBuf strings.
type SandboxWorkspaceWrite struct {
	WritableRoots       []string `json:"writable_roots" toml:"writable_roots"`
	NetworkAccess       bool     `json:"network_access" toml:"network_access"`
	ExcludeTmpdirEnvVar bool     `json:"exclude_tmpdir_env_var" toml:"exclude_tmpdir_env_var"`
	ExcludeSlashTmp     bool     `json:"exclude_slash_tmp" toml:"exclude_slash_tmp"`
}

// ShellEnvironmentPolicyToml is the env policy as written in config.toml.
// deny_unknown_fields. `set` uses the Rust raw identifier r#set.
type ShellEnvironmentPolicyToml struct {
	Inherit                *protocol.ShellEnvironmentPolicyInherit `json:"inherit" toml:"inherit"`
	IgnoreDefaultExcludes  *bool                                   `json:"ignore_default_excludes" toml:"ignore_default_excludes"`
	Exclude                *[]string                               `json:"exclude" toml:"exclude"`
	Set                    *map[string]string                      `json:"set" toml:"set"`
	IncludeOnly            *[]string                               `json:"include_only" toml:"include_only"`
	ExperimentalUseProfile *bool                                   `json:"experimental_use_profile" toml:"experimental_use_profile"`
}

// ProjectConfig is a per-project trust entry. deny_unknown_fields.
type ProjectConfig struct {
	TrustLevel *protocol.TrustLevel `json:"trust_level" toml:"trust_level"`
}

// IsTrusted reports whether the project is explicitly trusted.
func (p ProjectConfig) IsTrusted() bool {
	return p.TrustLevel != nil && *p.TrustLevel == protocol.TrustLevelTrusted
}

// IsUntrusted reports whether the project is explicitly untrusted.
func (p ProjectConfig) IsUntrusted() bool {
	return p.TrustLevel != nil && *p.TrustLevel == protocol.TrustLevelUntrusted
}

// WindowsSandboxModeToml selects the Windows sandbox elevation. kebab-case.
type WindowsSandboxModeToml string

const (
	WindowsSandboxElevated   WindowsSandboxModeToml = "elevated"
	WindowsSandboxUnelevated WindowsSandboxModeToml = "unelevated"
)

// WindowsToml is Windows-specific config. deny_unknown_fields.
type WindowsToml struct {
	Sandbox               *WindowsSandboxModeToml `json:"sandbox" toml:"sandbox"`
	SandboxPrivateDesktop *bool                   `json:"sandbox_private_desktop" toml:"sandbox_private_desktop"`
}

// DebugConfigLockToml configures the effective-config lockfile. deny_unknown_fields.
type DebugConfigLockToml struct {
	ExportDir                          *string `json:"export_dir" toml:"export_dir"`
	LoadPath                           *string `json:"load_path" toml:"load_path"`
	AllowCodexVersionMismatch          *bool   `json:"allow_codex_version_mismatch" toml:"allow_codex_version_mismatch"`
	SaveFieldsResolvedFromModelCatalog *bool   `json:"save_fields_resolved_from_model_catalog" toml:"save_fields_resolved_from_model_catalog"`
}

// DebugToml holds debugging/reproducibility settings. deny_unknown_fields.
type DebugToml struct {
	ConfigLockfile *DebugConfigLockToml `json:"config_lockfile" toml:"config_lockfile"`
}

// AutoReviewToml carries optional guardian auto-reviewer policy.
type AutoReviewToml struct {
	Policy *string `json:"policy" toml:"policy"`
}

// AgentRoleToml declares a user-defined agent role. deny_unknown_fields.
type AgentRoleToml struct {
	Description        *string   `json:"description" toml:"description"`
	ConfigFile         *string   `json:"config_file" toml:"config_file"`
	NicknameCandidates *[]string `json:"nickname_candidates" toml:"nickname_candidates"`
}

// GhostSnapshotToml retains legacy no-op fields for compatibility. The Rust
// struct accepts serde aliases; we record the canonical keys.
type GhostSnapshotToml struct {
	IgnoreLargeUntrackedFiles *int64 `json:"ignore_large_untracked_files" toml:"ignore_large_untracked_files"`
	IgnoreLargeUntrackedDirs  *int64 `json:"ignore_large_untracked_dirs" toml:"ignore_large_untracked_dirs"`
	DisableWarnings           *bool  `json:"disable_warnings" toml:"disable_warnings"`
}
