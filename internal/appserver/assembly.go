package appserver

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sqlrush/codexgo/internal/core"
	"github.com/sqlrush/codexgo/internal/modelsmanager"
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/rollout"
	"github.com/sqlrush/codexgo/internal/threadstore"
	"github.com/sqlrush/codexgo/internal/tools"
)

// ModelClientFactory builds the per-thread [core.ModelClient] for a spawning
// thread. It is the dependency-injection seam that lets the app-server be
// assembled against either a real [core.ResponsesModelClient] or a
// [core.MockModelClient] in tests. The factory receives the thread id and the
// resolved session configuration so it can scope the client (window id, prompt
// cache key, model slug) to the thread.
type ModelClientFactory func(ctx context.Context, threadID protocol.ThreadID, cfg core.SessionConfiguration) (core.ModelClient, error)

// AssemblyConfig bundles the inputs needed to build the Codex engine. It is the
// reduced Go analogue of the Rust app-server MessageProcessorArgs init surface,
// focused on the turn-running path. The richer managers (MCP, Skills, Plugins,
// Hooks, Exec, Rollout) are injected when available and otherwise default to
// no-op implementations so the engine still runs.
type AssemblyConfig struct {
	// ModelClientFactory builds the per-thread model client; required.
	ModelClientFactory ModelClientFactory

	// Store is the thread persistence boundary. When nil, an in-memory store is
	// used (mirroring thread_store_from_config for debug/test configs).
	Store threadstore.ThreadStore

	// NewThreadID mints fresh thread ids. When nil, a UUIDv7-style monotonic
	// generator is used.
	NewThreadID core.ThreadIDFactory

	// SessionSource classifies spawned threads. When zero, the rollout default
	// source is used.
	SessionSource rollout.SessionSource

	// InstallationID is the stable installation identifier.
	InstallationID string
	// Originator is recorded in session metadata (e.g. "codex_cli_rs").
	Originator string
	// CliVersion is recorded in session metadata.
	CliVersion string

	// CodexHome is the resolved Codex configuration directory, surfaced in the
	// initialize handshake response.
	CodexHome string

	// DefaultModel is the configured default model slug, returned by the models
	// manager when no per-turn override is supplied.
	DefaultModel string

	// ModelCatalog, when non-empty, backs the models manager so turns resolve
	// full per-model metadata (shell_type, truncation policy, capabilities).
	// When empty, slug-derived fallback metadata is used.
	ModelCatalog []modelsmanager.ModelInfo

	// Now returns the current time; defaults to time.Now when nil.
	Now func() time.Time

	// Optional managers. When nil, no-op implementations are wired so the engine
	// runs without MCP/skills/plugins/hooks/exec/rollout.
	McpManager      core.McpManager
	SkillsManager   core.SkillsManager
	PluginsManager  core.PluginsManager
	HooksEngine     core.HooksEngine
	ExecService     core.ExecService
	RolloutRecorder core.RolloutRecorder

	// McpGateway, when set, exposes the connected MCP tools for the deterministic
	// "slash → tool call" entry (mcp/listTools, mcp/callTool). It is the same
	// process-wide manager that backs the per-thread tool router. Nil disables
	// those methods (they return an empty list / "no MCP" error).
	McpGateway McpToolGateway

	// ToolRouterFactory, when set, builds the per-thread tool router; it
	// receives the spawning thread's id so per-thread tools (e.g. the goal
	// trio, which persists goals keyed by thread) can be scoped. When nil,
	// core.NewDefaultToolRouter is used.
	ToolRouterFactory func(threadID protocol.ThreadID) (core.ToolRouter, error)
}

// McpToolGateway is the minimal surface the deterministic slash→tool-call
// methods need: list the connected tools and invoke one by its canonical name.
// It is satisfied by *mcp.Manager. Kept as an interface here so appserver does
// not import internal/mcp.
type McpToolGateway interface {
	ListAllToolInfos() []tools.McpToolInfo
	CallQualifiedTool(ctx context.Context, qualifiedName string, arguments, meta json.RawMessage) (protocol.CallToolResult, error)
}

// Assembly is the constructed engine: the thread manager plus the models
// manager and the handshake metadata. It is held by a [Processor] and drives
// every thread lifecycle and turn.
type Assembly struct {
	// ThreadManager spawns and tracks threads.
	ThreadManager *core.ThreadManager
	// ModelsManager resolves model metadata for turns.
	ModelsManager core.ModelsManager
	// CodexHome is surfaced in the initialize response.
	CodexHome string
	// McpGateway exposes connected MCP tools for mcp/listTools + mcp/callTool;
	// nil when no MCP servers are wired.
	McpGateway McpToolGateway
}

// Assemble builds the Codex engine from cfg, following the codex init order:
// validate inputs, install defaults, build the per-thread services factory,
// then the thread manager. It mirrors the Rust MessageProcessor::new wiring
// reduced to the turn-running subset. Errors are wrapped with %w.
func Assemble(cfg AssemblyConfig) (*Assembly, error) {
	if cfg.ModelClientFactory == nil {
		return nil, fmt.Errorf("appserver: AssemblyConfig.ModelClientFactory is required")
	}

	store := cfg.Store
	if store == nil {
		store = threadstore.NewInMemoryThreadStore()
	}

	newThreadID := cfg.NewThreadID
	if newThreadID == nil {
		newThreadID = defaultThreadIDFactory()
	}

	now := cfg.Now
	if now == nil {
		now = time.Now
	}

	models := newStaticModelsManager(cfg.DefaultModel, cfg.ModelCatalog)

	routerFactory := cfg.ToolRouterFactory
	if routerFactory == nil {
		routerFactory = func(protocol.ThreadID) (core.ToolRouter, error) {
			return core.NewDefaultToolRouter()
		}
	}

	// servicesFactory constructs the per-thread SessionServices, building a fresh
	// model client and tool router for each spawn (dependency injection). This is
	// the Go analogue of ThreadManager's per-spawn services construction.
	servicesFactory := func(ctx context.Context, threadID protocol.ThreadID, sessionCfg core.SessionConfiguration) (core.SessionServices, error) {
		client, err := cfg.ModelClientFactory(ctx, threadID, sessionCfg)
		if err != nil {
			return core.SessionServices{}, fmt.Errorf("appserver: build model client for thread %s: %w", threadID.String(), err)
		}
		router, err := routerFactory(threadID)
		if err != nil {
			return core.SessionServices{}, fmt.Errorf("appserver: build tool router for thread %s: %w", threadID.String(), err)
		}
		return core.SessionServices{
			ModelClient:     client,
			ToolRouter:      router,
			McpManager:      cfg.McpManager,
			SkillsManager:   cfg.SkillsManager,
			PluginsManager:  cfg.PluginsManager,
			HooksEngine:     cfg.HooksEngine,
			ModelsManager:   models,
			ExecService:     cfg.ExecService,
			RolloutRecorder: cfg.RolloutRecorder,
		}, nil
	}

	manager, err := core.NewThreadManager(core.ThreadManagerConfig{
		Store:           store,
		ServicesFactory: servicesFactory,
		NewThreadID:     newThreadID,
		SessionSource:   cfg.SessionSource,
		InstallationID:  cfg.InstallationID,
		Originator:      cfg.Originator,
		CliVersion:      cfg.CliVersion,
		Now:             now,
	})
	if err != nil {
		return nil, fmt.Errorf("appserver: build thread manager: %w", err)
	}

	return &Assembly{
		ThreadManager: manager,
		ModelsManager: models,
		CodexHome:     cfg.CodexHome,
		McpGateway:    cfg.McpGateway,
	}, nil
}
