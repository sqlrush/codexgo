package core

import (
	"context"

	"github.com/sqlrush/codexgo/internal/api"
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/rollout"
	"github.com/sqlrush/codexgo/internal/tools"
)

// This file defines the SMALL Go interfaces that core depends on. They are
// declared here (where they are consumed) rather than in the implementing
// packages, following the "accept interfaces, return structs" idiom and to
// avoid import cycles. Concrete implementations are injected through
// CodexSpawnArgs (dependency injection), which keeps core testable with mocks.
//
// Each interface is deliberately narrow: it exposes only the surface the
// turn-running path actually needs. Where the real codex-core managers expose a
// much larger API, the omitted methods are noted as STUB in the doc comment.

// ----------------------------------------------------------------------------
// ModelClient
// ----------------------------------------------------------------------------

// Prompt is the per-request input handed to a [ModelClient]. It is a thin,
// engine-facing view of what the Rust `Prompt` carries: the conversation input
// items, the resolved tool set, and the base/developer instructions. The model
// streaming layer (internal/api) translates this into a provider request.
//
// Prompt is treated as read-only by the client; core constructs a fresh Prompt
// for every model request (immutability).
type Prompt struct {
	// Input is the ordered conversation history plus any turn-local input items
	// to send to the model.
	Input []protocol.ResponseItem
	// Tools is the resolved tool specification for this request.
	Tools []tools.ToolSpec
	// BaseInstructionsOverride, when non-nil, replaces the model's default base
	// instructions for this request.
	BaseInstructionsOverride *string
	// OutputSchema, when non-nil, constrains the model's final output to a JSON
	// schema (the turn's final_output_json_schema).
	OutputSchema []byte
}

// ModelClient streams a single model turn. It abstracts internal/api's
// streaming client so core can run against a mock in tests.
//
// Stream issues one request and returns a channel of [api.ResponseEvent]
// values (plus terminal errors) that the turn runner consumes in order. The
// returned channel must be closed when the stream terminates. Cancellation is
// driven through ctx.
//
// STUB: the real ModelClient also exposes window-generation tracking, prompt
// cache-key overrides, reasoning configuration, and provider rerouting; those
// are deferred.
type ModelClient interface {
	// Stream starts a model request and yields its response events.
	Stream(ctx context.Context, prompt Prompt) (<-chan api.ResponseEvent, error)
	// ContextWindow reports the model's effective context window in tokens, if
	// known, used for token-usage accounting.
	ContextWindow() *int64
	// ModelSlug returns the canonical slug of the model this client targets.
	ModelSlug() string
}

// ----------------------------------------------------------------------------
// ToolRouter
// ----------------------------------------------------------------------------

// ToolInvocation describes a single tool call requested by the model.
type ToolInvocation struct {
	// CallID correlates the call with its output item.
	CallID string
	// Name is the tool name (possibly namespaced).
	Name protocol.ToolName
	// Arguments is the raw JSON argument payload from the model.
	Arguments []byte
}

// ToolResult is the outcome of a tool invocation, ready to be folded back into
// conversation history as a function-call output item.
type ToolResult struct {
	// Output is the textual result returned to the model.
	Output string
	// Success reports whether the tool call succeeded.
	Success bool
}

// ToolRouter resolves the available tool set for a turn and dispatches tool
// invocations. It abstracts internal/tools so core does not depend on the full
// tool framework.
//
// STUB: parallel tool execution, MCP tool fan-out, dynamic-tool registration,
// and approval orchestration are deferred to area agents that own those flows.
type ToolRouter interface {
	// SpecsForTurn returns the resolved tool specifications visible to the
	// model for the given turn context.
	SpecsForTurn(ctx context.Context, tc *TurnContext) ([]tools.ToolSpec, error)
	// Dispatch executes a single tool invocation and returns its result.
	Dispatch(ctx context.Context, tc *TurnContext, inv ToolInvocation) (ToolResult, error)
}

// ----------------------------------------------------------------------------
// McpManager
// ----------------------------------------------------------------------------

// McpManager owns MCP server connections for a session. Core only needs to
// know how to bring servers up at session start and tear them down at shutdown;
// the tool surface is reached through [ToolRouter].
//
// STUB: required-server gating, OAuth status computation, elicitation routing,
// and runtime refresh are deferred.
type McpManager interface {
	// EnsureServers initializes/refreshes the configured MCP servers. It is
	// invoked during session startup.
	EnsureServers(ctx context.Context) error
	// Shutdown closes all MCP connections.
	Shutdown(ctx context.Context) error
}

// ----------------------------------------------------------------------------
// SkillsManager
// ----------------------------------------------------------------------------

// SkillsManager loads the skills available for a turn. Core threads the loaded
// outcome into the turn context as opaque metadata.
//
// STUB: implicit-invocation tracking, side-effect rendering, and per-plugin
// skill roots are deferred.
type SkillsManager interface {
	// SkillsForTurn returns an opaque handle describing the skills available for
	// the turn. Core does not interpret the payload.
	SkillsForTurn(ctx context.Context, tc *TurnContext) (any, error)
}

// ----------------------------------------------------------------------------
// PluginsManager
// ----------------------------------------------------------------------------

// PluginsManager resolves plugin-provided configuration (skill roots, hooks,
// tool provenance) for a session.
//
// STUB: cache invalidation and config-contributor notification are deferred.
type PluginsManager interface {
	// PluginSkillRoots returns the effective filesystem roots contributed by
	// enabled plugins.
	PluginSkillRoots(ctx context.Context) ([]string, error)
}

// ----------------------------------------------------------------------------
// HooksEngine
// ----------------------------------------------------------------------------

// HookEvent identifies a lifecycle point at which hooks may fire.
type HookEvent string

const (
	// HookSessionStart fires once when the session is configured.
	HookSessionStart HookEvent = "session_start"
	// HookTurnStart fires at the beginning of each turn.
	HookTurnStart HookEvent = "turn_start"
	// HookTurnEnd fires after each turn completes.
	HookTurnEnd HookEvent = "turn_end"
	// HookSessionEnd fires once during session shutdown.
	HookSessionEnd HookEvent = "session_end"
)

// HooksEngine runs user/plugin-defined hooks at session and turn boundaries.
//
// STUB: hook output injection into history, blocking decisions, and per-hook
// telemetry are deferred.
type HooksEngine interface {
	// Fire runs the hooks registered for the given lifecycle event. The payload
	// is an opaque, event-specific value.
	Fire(ctx context.Context, event HookEvent, payload any) error
}

// ----------------------------------------------------------------------------
// ModelsManager
// ----------------------------------------------------------------------------

// ModelsManager resolves model metadata. Core uses it to look up the model info
// for a turn (context window, reasoning defaults).
//
// STUB: online refresh strategies, model catalog listing, and preset
// enumeration are deferred; ModelInfo is carried as an opaque value so core
// does not import the modelsmanager type directly.
type ModelsManager interface {
	// ModelInfo returns opaque model metadata for the given slug. Core stores it
	// on the turn context without interpreting it.
	ModelInfo(ctx context.Context, slug string) (any, error)
	// DefaultModelSlug returns the configured default model slug.
	DefaultModelSlug() string
}

// ----------------------------------------------------------------------------
// ExecService
// ----------------------------------------------------------------------------

// ExecRequest describes a command to run through the exec service.
type ExecRequest struct {
	// Command is the argv to execute.
	Command []string
	// Cwd is the working directory for the command.
	Cwd string
}

// ExecResult is the outcome of an exec request.
type ExecResult struct {
	// ExitCode is the process exit code.
	ExitCode int32
	// Stdout / Stderr capture the command's output streams.
	Stdout string
	Stderr string
}

// ExecService runs sandboxed commands on behalf of tool calls. It abstracts
// internal/execserver.
//
// STUB: streaming output deltas, unified-exec session reuse, PTY interaction,
// and approval escalation are deferred.
type ExecService interface {
	// Run executes a command and returns its result.
	Run(ctx context.Context, req ExecRequest) (ExecResult, error)
}

// ----------------------------------------------------------------------------
// RolloutRecorder
// ----------------------------------------------------------------------------

// RolloutRecorder persists rollout items (history, events, turn context) so a
// session can be resumed or forked later. It abstracts internal/rollout.
//
// STUB: durability barriers, materialization, state-db handles, and extended
// vs. limited persistence modes are deferred to the recorder implementation.
type RolloutRecorder interface {
	// Record appends rollout items to the session's rollout.
	Record(ctx context.Context, items []rollout.RolloutItem) error
	// Flush forces buffered rollout writes to durable storage.
	Flush(ctx context.Context) error
}
