package cli

import (
	"fmt"
	"os"
	"sort"

	"github.com/sqlrush/codexgo/pkg/config"
	"github.com/sqlrush/codexgo/pkg/features"
	"github.com/sqlrush/codexgo/pkg/modelproviderinfo"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// configLoadCheck loads the merged configuration and reports unknown-field
// warnings, mirroring config.load in doctor.rs. It returns the resolved
// [doctorContext] so downstream checks reuse the merged configuration without
// re-reading config.toml. On failure the returned context has Loaded=false and
// dependent checks degrade gracefully.
func configLoadCheck(root RootOptions) (doctorContext, doctorCheck) {
	b := newCheck("config.load", "config")
	overrides, err := root.Overrides.Parse()
	if err != nil {
		b.fail("could not parse -c overrides").
			detail(fmt.Errorf("override parse error: %w", err).Error()).
			remedy("Fix the reported config error, then rerun codex doctor.")
		return doctorContext{}, b.build()
	}
	result, err := config.Load(config.LoadOptions{
		Profile:      root.Profile,
		CliOverrides: overrides,
		StrictConfig: root.StrictConfig,
	})
	if err != nil {
		b.fail("could not load configuration").
			detail(fmt.Errorf("config load error: %w", err).Error()).
			remedy("Fix the reported config error, then rerun codex doctor.")
		return doctorContext{}, b.build()
	}

	dctx := doctorContext{
		Loaded:         true,
		CodexHome:      result.CodexHome,
		StoreMode:      resolveStoreMode(result.Config.CliAuthCredentialsStore),
		ChatgptBaseURL: result.Config.ChatgptBaseURL,
		Model:          derefString(result.Config.Model),
		ModelProvider:  derefString(result.Config.ModelProvider),
		LogDir:         derefString(result.Config.LogDir),
		SqliteHome:     derefString(result.Config.SqliteHome),
		McpServers:     result.Config.McpServers,
		Cfg:            result.Config,
	}

	// Detail emission order mirrors config_check in doctor.rs: CODEXGO_HOME, cwd,
	// model, model provider, log dir, sqlite home, mcp servers, then the feature
	// flag rows, then the config.toml path/parse rows. JSON keys are sorted by the
	// marshaler, so this order only affects the human renderer.
	b.detail(fmt.Sprintf("CODEXGO_HOME: %s", result.CodexHome))
	b.detail(fmt.Sprintf("cwd: %s", resolveCwd()))
	b.detail(fmt.Sprintf("model: %s", orDefault(dctx.Model)))
	b.detail(fmt.Sprintf("model provider: %s", resolveModelProviderID(dctx.ModelProvider)))
	b.detail(fmt.Sprintf("log dir: %s", resolveLogDir(dctx)))
	b.detail(fmt.Sprintf("sqlite home: %s", resolveSqliteHome(dctx)))
	b.detail(fmt.Sprintf("mcp servers: %d", len(dctx.McpServers)))
	featureFlagDetails(b, result.Config)
	configTomlDetails(b, result.CodexHome)
	for _, w := range result.Warnings {
		b.detail(w)
	}

	if len(result.Warnings) > 0 {
		b.detail(fmt.Sprintf("startup warnings: %d", len(result.Warnings)))
		b.warn("config loaded with warnings").
			remedy("Review the reported warnings, then rerun codex doctor.")
	} else {
		b.ok("config loaded")
	}
	return dctx, b.build()
}

// orDefault returns value, or the literal "<default>" when value is empty,
// mirroring config_check's model rendering (`config.model.unwrap_or("<default>")`).
func orDefault(value string) string {
	if value == "" {
		return "<default>"
	}
	return value
}

// resolveModelProviderID returns the configured provider id, or the built-in
// "openai" default when unset, mirroring Config.model_provider_id resolution.
func resolveModelProviderID(value string) string {
	if value == "" {
		return modelproviderinfo.OpenAIProviderID
	}
	return value
}

// featureFlagDetails emits the feature-flag detail rows for config.load, mirroring
// feature_flag_details in doctor.rs: the enabled count, the comma-joined enabled
// keys ("none" when empty), the overrides (keys whose effective value differs from
// the registry default, "none" when empty), and one legacy-feature row per
// recorded legacy usage. Feature resolution uses the merged config layer (defaults
// + [features] + the legacy unified-exec toggle); CLI -c feature overrides are not
// re-resolved here, matching the doctor's read-only projection.
func featureFlagDetails(b *checkBuilder, cfg config.ConfigToml) {
	source := features.FeatureConfigSource{
		Features:                       cfg.Features,
		ExperimentalUseUnifiedExecTool: cfg.ExperimentalUseUnifiedExecTool,
	}
	resolved := features.FromSources(source, features.FeatureConfigSource{}, features.FeatureOverrides{})

	var enabled []string
	var overrides []string
	for _, spec := range features.FEATURES {
		on := resolved.Enabled(spec.ID)
		if on {
			enabled = append(enabled, spec.Key)
		}
		if on != spec.DefaultEnabled {
			overrides = append(overrides, fmt.Sprintf("%s=%t", spec.Key, on))
		}
	}

	b.detail(fmt.Sprintf("feature flags enabled: %d", len(enabled)))
	b.detail(fmt.Sprintf("enabled feature flags: %s", displayList(enabled)))
	b.detail(fmt.Sprintf("feature flag overrides: %s", displayList(overrides)))
	for _, usage := range resolved.LegacyFeatureUsages() {
		b.detail(fmt.Sprintf("legacy feature flag: %s -> %s", usage.Alias, usage.Feature.Key()))
	}
}

// configTomlDetails emits the config.toml path detail and a follow-up status row,
// mirroring config_toml_details in doctor.rs. A present file emits a second
// "config.toml parse: ..." row; a missing file emits a second "config.toml:
// missing" row, which collapses into the [path, "missing"] array codex emits.
func configTomlDetails(b *checkBuilder, codexHome string) {
	configPath := config.ConfigTomlPath(codexHome)
	b.detail(fmt.Sprintf("config.toml: %s", configPath))
	contents, err := os.ReadFile(configPath)
	switch {
	case err == nil:
		if _, parseErr := config.ParseTomlValue(contents); parseErr != nil {
			b.detail(fmt.Sprintf("config.toml parse: %v", parseErr))
		} else {
			b.detail("config.toml parse: ok")
		}
	case os.IsNotExist(err):
		b.detail("config.toml: missing")
	default:
		b.detail(fmt.Sprintf("config.toml read: %v", err))
	}
}

// displayList joins items with ", ", or returns "none" when empty, mirroring the
// display_list helper in doctor.rs.
func displayList(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	return joinComma(items)
}

// mcpConfigCheck reports whether the configured MCP servers are locally
// consistent (resolvable transports, present env vars), mirroring mcp.config in
// doctor.rs. It performs no network probes.
func mcpConfigCheck(dctx doctorContext) doctorCheck {
	b := newCheck("mcp.config", "mcp")
	if !dctx.Loaded {
		b.warn("skipped: configuration did not load")
		return b.build()
	}
	servers := dctx.McpServers
	if len(servers) == 0 {
		b.ok("no MCP servers configured")
		return b.build()
	}

	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)

	transportCounts := map[string]int{}
	disabled := 0
	var issues []string
	requiredIssue := false

	for _, name := range names {
		server := servers[name]
		serverDisabled := !server.Enabled
		if serverDisabled {
			disabled++
		}
		switch server.Transport.Kind {
		case config.McpTransportStdio:
			transportCounts["stdio"]++
			if serverDisabled {
				continue
			}
			if isBlank(server.Transport.Command) {
				issues = append(issues, fmt.Sprintf("%s: stdio command is empty", name))
				if server.Required {
					requiredIssue = true
				}
			}
		case config.McpTransportStreamableHTTP:
			transportCounts["streamable_http"]++
			if serverDisabled {
				continue
			}
			if server.Transport.BearerTokenEnvVar != nil && !envVarPresent(*server.Transport.BearerTokenEnvVar) {
				issues = append(issues, fmt.Sprintf("%s: bearer token env var %s is not set", name, *server.Transport.BearerTokenEnvVar))
				if server.Required {
					requiredIssue = true
				}
			}
		}
	}

	b.detail(fmt.Sprintf("configured servers: %d", len(servers)))
	b.detail(fmt.Sprintf("disabled servers: %d", disabled))
	for _, transport := range []string{"stdio", "streamable_http"} {
		if count, ok := transportCounts[transport]; ok {
			b.detail(fmt.Sprintf("%s servers: %d", transport, count))
		}
	}
	for _, issue := range issues {
		b.detail(issue)
	}

	switch {
	case requiredIssue:
		b.fail("MCP configuration has failing required inputs").
			remedy("Set the missing MCP env vars or disable the affected server.")
	case len(issues) > 0:
		b.warn("MCP configuration has optional issues").
			remedy("Set the missing MCP env vars or disable the affected server.")
	default:
		b.ok("MCP configuration is locally consistent")
	}
	return b.build()
}

// sandboxHelpersCheck reports the resolved sandbox configuration (approval policy,
// filesystem/network sandbox policy) and the resolved arg0 helper paths, mirroring
// sandbox.helpers in doctor.rs. It inspects configuration only and never spawns
// the sandbox.
//
// codexgo has no Arg0DispatchPaths plumbing yet, so the execve wrapper helper is
// reported as "none"; the codex-linux-sandbox helper is "none" off Linux (matching
// codex on macOS). See DEVIATIONS.md (doctor).
func sandboxHelpersCheck(dctx doctorContext) doctorCheck {
	b := newCheck("sandbox.helpers", "sandbox")
	if !dctx.Loaded {
		b.warn("skipped: configuration did not load")
		return b.build()
	}

	approval := approvalPolicyDebugName(protocol.AskForApprovalOnRequest)
	if dctx.Cfg.ApprovalPolicy != nil {
		approval = approvalPolicyDebugName(dctx.Cfg.ApprovalPolicy.Kind)
	}
	sandboxMode := protocol.SandboxModeReadOnly
	if dctx.Cfg.SandboxMode != nil {
		sandboxMode = *dctx.Cfg.SandboxMode
	}
	filesystem, network := sandboxPoliciesForMode(sandboxMode, dctx.Cfg)

	b.detail(fmt.Sprintf("approval policy: %s", approval))
	b.detail(fmt.Sprintf("filesystem sandbox: %s", filesystem))
	b.detail(fmt.Sprintf("network sandbox: %s", network))
	b.detail(fmt.Sprintf("codex-linux-sandbox helper: %s", codexLinuxSandboxHelperPath()))
	b.detail("execve wrapper helper: none")

	b.ok("sandbox configuration is readable")
	return b.build()
}

// approvalPolicyDebugName maps a serde-kebab approval policy to the Rust enum
// Debug name (e.g. "on-request" -> "OnRequest"), matching the `{:?}` rendering
// codex uses for the approval policy detail.
func approvalPolicyDebugName(kind protocol.AskForApprovalKind) string {
	switch kind {
	case protocol.AskForApprovalUnlessTrusted:
		return "UnlessTrusted"
	case protocol.AskForApprovalOnFailure:
		return "OnFailure"
	case protocol.AskForApprovalOnRequest:
		return "OnRequest"
	case protocol.AskForApprovalGranular:
		return "Granular"
	case protocol.AskForApprovalNever:
		return "Never"
	default:
		return string(kind)
	}
}

// sandboxPoliciesForMode derives the filesystem-sandbox kind and network-sandbox
// policy strings from the resolved sandbox mode, mirroring
// Permissions::file_system_sandbox_policy/network_sandbox_policy for the common
// case. read-only and workspace-write keep filesystem access restricted; only
// danger-full-access relaxes to unrestricted. Network stays restricted unless
// danger-full-access or workspace-write opts into network access.
func sandboxPoliciesForMode(mode protocol.SandboxMode, cfg config.ConfigToml) (filesystem, network string) {
	switch mode {
	case protocol.SandboxModeDangerFullAccess:
		return string(protocol.FileSystemSandboxKindUnrestricted), string(protocol.NetworkSandboxPolicyEnabled)
	case protocol.SandboxModeWorkspaceWrite:
		network := protocol.NetworkSandboxPolicyRestricted
		if workspaceWriteAllowsNetwork(cfg) {
			network = protocol.NetworkSandboxPolicyEnabled
		}
		return string(protocol.FileSystemSandboxKindRestricted), string(network)
	default: // read-only
		return string(protocol.FileSystemSandboxKindRestricted), string(protocol.NetworkSandboxPolicyRestricted)
	}
}

// workspaceWriteAllowsNetwork reports whether the workspace-write sandbox config
// opts into network access, mirroring SandboxPolicy::WorkspaceWrite.network_access.
func workspaceWriteAllowsNetwork(cfg config.ConfigToml) bool {
	return cfg.SandboxWorkspaceWrite != nil && cfg.SandboxWorkspaceWrite.NetworkAccess
}

// codexLinuxSandboxHelperPath returns the resolved codex-linux-sandbox helper
// path, or "none" when unavailable. Off Linux the helper is absent (matching
// codex on macOS); on Linux codexgo does not yet bundle the helper, so "none" is
// the closest faithful value.
func codexLinuxSandboxHelperPath() string {
	return "none"
}
