package cli

import (
	"fmt"
	"sort"

	"github.com/sqlrush/codexgo/internal/config"
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/sandbox"
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

	b.detail(fmt.Sprintf("CODEX_HOME: %s", result.CodexHome))
	b.detail(fmt.Sprintf("cwd: %s", resolveCwd()))
	b.detail(fmt.Sprintf("config.toml: %s", config.ConfigTomlPath(result.CodexHome)))
	b.detail(fmt.Sprintf("model: %s", orNone(dctx.Model)))
	b.detail(fmt.Sprintf("model provider: %s", orNone(dctx.ModelProvider)))
	b.detail(fmt.Sprintf("mcp servers: %d", len(dctx.McpServers)))
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

// sandboxHelpersCheck reports the resolved sandbox configuration (approval
// policy and sandbox mode) and whether a platform sandbox is available, mirroring
// sandbox.helpers in doctor.rs. It inspects configuration only and never spawns
// the sandbox.
func sandboxHelpersCheck(dctx doctorContext) doctorCheck {
	b := newCheck("sandbox.helpers", "sandbox")
	if !dctx.Loaded {
		b.warn("skipped: configuration did not load")
		return b.build()
	}

	approval := string(protocol.AskForApprovalOnRequest)
	if dctx.Cfg.ApprovalPolicy != nil {
		approval = string(dctx.Cfg.ApprovalPolicy.Kind)
	}
	sandboxMode := string(protocol.SandboxModeReadOnly)
	if dctx.Cfg.SandboxMode != nil {
		sandboxMode = string(*dctx.Cfg.SandboxMode)
	}
	_, platformSandbox := sandbox.GetPlatformSandbox(false)
	b.detail(fmt.Sprintf("approval policy: %s", approval))
	b.detail(fmt.Sprintf("sandbox mode: %s", sandboxMode))
	b.detail(fmt.Sprintf("platform sandbox available: %t", platformSandbox))

	b.ok("sandbox configuration is readable")
	return b.build()
}
