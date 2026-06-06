// Package brand centralizes every constant that defines codexgo's local
// product identity: the environment-variable prefix, the configuration
// directory name, OS keyring service names, plugin manifest locations, and
// the busybox-style self-dispatch aliases.
//
// codexgo started as a byte-for-byte port of the OpenAI Codex CLI, but it is
// a separate product with a fully isolated local namespace: installing or
// running codexgo never reads or writes the native codex installation's
// configuration, credentials, environment variables, or keychain entries.
//
// Only the LOCAL identity lives here. Wire-protocol identity (originator,
// User-Agent, OAuth client ID, API endpoints) intentionally still matches
// upstream codex so that ChatGPT login keeps working; those values stay in
// their owning packages until codexgo switches its default backend.
//
// To rebrand again, change the constants in this file and recompile.
package brand

const (
	// Name is the product and binary name.
	Name = "codexgo"

	// EnvPrefix is the prefix for every environment variable codexgo reads
	// from the user environment or exports to child processes. The upstream
	// codex CLI uses "CODEX_"; codexgo deliberately uses its own prefix so
	// the two products never observe each other's variables.
	EnvPrefix = "CODEXGO_"

	// HomeEnvVar overrides the configuration directory (~/.codexgo).
	HomeEnvVar = EnvPrefix + "HOME"

	// DotDirName is the per-user configuration directory name under $HOME,
	// and also the project-level convention directory inside repositories
	// (e.g. <repo>/.codexgo/skills).
	DotDirName = ".codexgo"

	// KeyringSecretsService is the OS keyring service name for the secrets
	// feature. Upstream codex uses "codex"; using a distinct service keeps
	// keychain entries fully separated.
	KeyringSecretsService = "codexgo"

	// KeyringMCPOAuthService is the OS keyring service name for MCP OAuth
	// credentials. Upstream codex uses "Codex MCP Credentials".
	KeyringMCPOAuthService = "CodexGo MCP Credentials"

	// PluginManifestDir is the primary plugin manifest directory inside a
	// plugin bundle. Discovery falls back to the upstream codex and Claude
	// conventions (see PluginManifestDirCodexCompat) so existing ecosystem
	// plugins remain installable.
	PluginManifestDir = ".codexgo-plugin"

	// PluginManifestDirCodexCompat is the upstream codex plugin manifest
	// directory accepted as a compatibility fallback.
	PluginManifestDirCodexCompat = ".codex-plugin"

	// MarketplaceInstallMetadataFile records marketplace install metadata
	// inside an installed plugin directory.
	MarketplaceInstallMetadataFile = ".codexgo-marketplace-install.json"

	// Arg0LinuxSandbox is the busybox-style argv[0] alias that routes to the
	// Linux sandbox helper.
	Arg0LinuxSandbox = Name + "-linux-sandbox"

	// Arg0ExecveWrapper is the argv[0] alias that routes to the
	// shell-escalation execve wrapper (unix only).
	Arg0ExecveWrapper = Name + "-execve-wrapper"

	// Arg1FsHelper is the argv[1] marker that routes to the exec-server
	// filesystem helper.
	Arg1FsHelper = "--" + Name + "-run-as-fs-helper"

	// Arg1CoreApplyPatch is the argv[1] marker that routes to the in-process
	// apply_patch helper.
	Arg1CoreApplyPatch = "--" + Name + "-run-as-apply-patch"
)
