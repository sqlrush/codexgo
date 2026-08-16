package mcpserver

import (
	"context"
	"fmt"
	"io"

	"github.com/sqlrush/codexgo/internal/appserver"
	"github.com/sqlrush/codexgo/internal/core"
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/rollout"
)

// RunConfig bundles the inputs needed to run the MCP server over stdio. The
// engine wiring mirrors the app-server assembly; callers may inject a model
// client factory and managers, or accept the defaults for a server that handles
// the discovery surface (initialize/ping/tools/list) without a live model.
type RunConfig struct {
	// Stdin is the JSON-RPC input stream; defaults are the caller's responsibility.
	Stdin io.Reader
	// Stdout is the JSON-RPC output stream.
	Stdout io.Writer

	// ServerVersion is reported in the initialize response.
	ServerVersion string
	// UserAgent is reported as serverInfo.user_agent in the initialize response.
	UserAgent string

	// CodexHome is the resolved Codex configuration directory.
	CodexHome string
	// DefaultModel / DefaultProvider seed spawned threads.
	DefaultModel    string
	DefaultProvider string
	// DefaultCwd is the default working directory for spawned threads.
	DefaultCwd string
	// BaseInstructions is the default base instruction text.
	BaseInstructions string

	// ModelClientFactory builds the per-thread model client. When nil, a factory
	// returning a clear "model client not configured" error is used so the
	// discovery surface still works but live turns fail fast with a useful
	// message rather than a nil dereference.
	ModelClientFactory appserver.ModelClientFactory

	// Optional managers injected into the assembly. When nil, the engine runs
	// without MCP/skills/plugins/hooks/exec/rollout.
	McpManager      core.McpManager
	SkillsManager   core.SkillsManager
	PluginsManager  core.PluginsManager
	HooksEngine     core.HooksEngine
	RolloutRecorder core.RolloutRecorder
}

// Run assembles the Codex engine and serves the MCP protocol over stdio until
// ctx is cancelled or the input stream reaches EOF. It is the Go analogue of the
// Rust run_main, reduced to the turn-running subset and built around the shared
// app-server assembly.
func Run(ctx context.Context, cfg RunConfig) error {
	if cfg.Stdin == nil || cfg.Stdout == nil {
		return fmt.Errorf("mcpserver: RunConfig.Stdin and Stdout are required")
	}

	factory := cfg.ModelClientFactory
	if factory == nil {
		factory = unconfiguredModelClientFactory()
	}

	assembly, err := appserver.Assemble(appserver.AssemblyConfig{
		ModelClientFactory: factory,
		CodexHome:          cfg.CodexHome,
		DefaultModel:       cfg.DefaultModel,
		SessionSource:      rollout.NewMcpSource(),
		McpManager:         cfg.McpManager,
		SkillsManager:      cfg.SkillsManager,
		PluginsManager:     cfg.PluginsManager,
		HooksEngine:        cfg.HooksEngine,
		RolloutRecorder:    cfg.RolloutRecorder,
	})
	if err != nil {
		return fmt.Errorf("mcpserver: assemble engine: %w", err)
	}

	defaults := appserver.Defaults{
		Model:            cfg.DefaultModel,
		ProviderID:       cfg.DefaultProvider,
		Cwd:              cfg.DefaultCwd,
		BaseInstructions: cfg.BaseInstructions,
		UserAgent:        orDefault(cfg.UserAgent, defaultUserAgent(cfg.ServerVersion)),
	}

	return ServeStdio(ctx, assembly, defaults, cfg.Stdin, cfg.Stdout)
}

// unconfiguredModelClientFactory returns a factory that fails with a clear
// message. The discovery surface (initialize/ping/tools/list) does not build a
// model client, so it still works; only spawning a thread (a live turn) fails.
func unconfiguredModelClientFactory() appserver.ModelClientFactory {
	return func(_ context.Context, _ protocol.ThreadID, _ core.SessionConfiguration) (core.ModelClient, error) {
		return nil, fmt.Errorf("mcpserver: model client not configured")
	}
}

// defaultUserAgent builds a user-agent string from the server version.
func defaultUserAgent(version string) string {
	if version == "" {
		version = "0.0.0-dev"
	}
	return "codex-mcp-server/" + version
}
