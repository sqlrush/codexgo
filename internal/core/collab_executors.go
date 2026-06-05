package core

// Deferred collab (multi-agent) tool runtimes: spawn_agent, send_input,
// resume_agent, wait_agent, close_agent. In a default codex run these tools take
// ToolExposure::Deferred (registered, dispatch-only, advertised solely through
// tool_search) when search + namespace tools are available and Feature::Collab is
// enabled without MultiAgentV2. This file mirrors that exposure: each executor
// reports a hidden spec (Spec returns false), exposes its v1 spec + search text
// for tool_search via [deferredToolSource], and handles invocations through the
// injected [CollabControl] control plane.
//
// The actual multi-agent execution (spawning child threads, routing messages,
// waiting) lives in internal/multiagent and is reached through the [CollabControl]
// seam (see collab_control.go). When no control plane is wired, the runtimes
// still participate in tool_search but reject direct calls with a RespondToModel
// error, matching a default run with collab tools deferred but unavailable.

import (
	"context"
	"sync"

	"github.com/sqlrush/codexgo/internal/features"
	"github.com/sqlrush/codexgo/internal/modelsmanager"
	maspec "github.com/sqlrush/codexgo/internal/multiagent/spec"
	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/tools"
)

// collabHandlerKind discriminates which real handler a [collabExecutor] routes a
// direct invocation to. The spec/search machinery is shared; only the dispatch
// differs per tool.
type collabHandlerKind int

const (
	collabHandlerSpawn collabHandlerKind = iota
	collabHandlerSendInput
	collabHandlerResume
	collabHandlerWait
	collabHandlerClose
)

// deferredToolSource is implemented by deferred runtimes that contribute a
// tool_search entry for the turn. Mirrors the Rust `search_info()` on Deferred
// `CoreToolRuntime`s collected by append_tool_search_executor.
type deferredToolSource interface {
	// searchInfo returns the tool_search entry for this turn, or false when the
	// runtime is not deferred-eligible for the turn.
	searchInfo(tc *TurnContext) (tools.ToolSearchInfo, bool)
}

// collabExecutor is a deferred multi-agent tool runtime. The spec builder is a
// closure so each tool carries its own v1 spec, search text, and name.
type collabExecutor struct {
	name       protocol.ToolName
	searchText string
	// buildSpec builds the v1 ToolSpec for the turn (spawn_agent needs the
	// turn's available models; the others ignore the turn).
	buildSpec func(tc *TurnContext) tools.ToolSpec
	// kind selects the real handler for a direct invocation.
	kind collabHandlerKind
	// control is the injected multi-agent control plane. When nil the runtime
	// rejects direct calls with a RespondToModel error (deferred-but-unavailable).
	control CollabControl
}

func (e collabExecutor) Name() protocol.ToolName { return e.name }

// Spec reports a hidden spec: the collab tools are deferred (never directly
// model-visible) in this run. Mirrors ToolExposure::Deferred.
func (collabExecutor) Spec(*TurnContext) (tools.ToolSpec, bool) {
	return tools.ToolSpec{}, false
}

// MatchesPayload accepts function payloads (the model calls these by name after
// loading them via tool_search).
func (collabExecutor) MatchesPayload(p tools.ToolPayload) bool {
	return p.Kind == tools.ToolPayloadKindFunction
}

// Handle dispatches a direct invocation to the real multi-agent handler through
// the injected [CollabControl]. When no control plane is wired the runtime
// rejects the call with a model-facing error (deferred-but-unavailable), so a
// run without the multi-agent area assembled still answers cleanly.
func (e collabExecutor) Handle(ctx context.Context, h *toolHandlerContext) (tools.ToolOutput, error) {
	if e.control == nil {
		return nil, tools.RespondToModelError("collab manager unavailable")
	}
	switch e.kind {
	case collabHandlerSpawn:
		return handleSpawnAgent(ctx, e.control, h)
	case collabHandlerSendInput:
		return handleSendInput(ctx, e.control, h)
	case collabHandlerResume:
		return handleResumeAgent(ctx, e.control, h)
	case collabHandlerWait:
		return handleWaitAgent(ctx, e.control, h)
	case collabHandlerClose:
		return handleCloseAgent(ctx, e.control, h)
	default:
		return nil, tools.RespondToModelError(e.name.Name + " is not supported")
	}
}

// searchInfo returns the deferred tool_search entry for this turn when collab
// tools are deferred-eligible (the turnDeferredCollabSources gate), else false.
// Mirrors the Rust collab `search_info()` gated by ToolExposure::Deferred.
func (e collabExecutor) searchInfo(tc *TurnContext) (tools.ToolSearchInfo, bool) {
	if !collabDeferredEligible(tc) {
		return tools.ToolSearchInfo{}, false
	}
	source := maspec.MultiAgentToolSearchSource()
	return tools.ToolSearchInfoFromSpec(e.searchText, e.buildSpec(tc), &source)
}

// collabDeferredEligible reports whether the collab tools take the Deferred
// exposure this turn: Feature::Collab on, MultiAgentV2 off, and search +
// namespace tools available. Mirrors the spec_plan exposure conditions.
func collabDeferredEligible(tc *TurnContext) bool {
	feats := turnFeatures(tc)
	if !feats.Enabled(features.FeatureCollab) || feats.Enabled(features.FeatureMultiAgentV2) {
		return false
	}
	return turnSearchToolEnabled(tc) && turnNamespaceToolsEnabled(tc)
}

// collabSpawnAvailableModels returns the picker-ready model presets used to
// render the spawn_agent model overrides. The bare run derives them from the
// bundled catalog with no authenticated backend, matching codex when the models
// endpoint is empty and no ChatGPT auth is present. See DEVIATIONS. The result
// is a pure function of the embedded catalog, so it is computed once.
var collabSpawnAvailableModels = sync.OnceValue(func() []modelsmanager.ModelPreset {
	resp, err := modelsmanager.BundledModelsResponse()
	if err != nil {
		return nil
	}
	return modelsmanager.BuildAvailableModelsForCatalog(resp.Models)
})

// bareSpawnAgentOptions builds the spawn_agent v1 options for a default run:
// bundled picker models, the built-in agent_type description, no hiding, usage
// hint enabled with no override text (MultiAgentV2Config defaults). Mirrors the
// SpawnAgentToolOptions construction in spec_plan for the v1 (deferred) path.
func bareSpawnAgentOptions() maspec.SpawnAgentToolOptions {
	return maspec.SpawnAgentToolOptions{
		AvailableModels:             collabSpawnAvailableModels(),
		AgentTypeDescription:        maspec.AgentTypeDescription(),
		HideAgentTypeModelReasoning: false,
		IncludeUsageHint:            true,
		UsageHintText:               nil,
	}
}

// collabExecutors returns the five deferred collab runtimes with no control
// plane wired (tool_search-only; direct calls report the manager unavailable).
// Prefer [collabExecutorsWithDeps] when a [CollabControl] is available.
func collabExecutors() []collabExecutor {
	return collabExecutorsWithDeps(BuiltinToolDeps{})
}

// collabExecutorsWithDeps returns the five deferred collab runtimes in spec_plan
// registration order (spawn, send_input, resume, wait, close), each wired with
// the injected control plane. The order fixes the BM25 corpus document ids,
// which determines tie-break order in search.
func collabExecutorsWithDeps(deps BuiltinToolDeps) []collabExecutor {
	control := deps.Collab
	return []collabExecutor{
		{
			name:       protocol.NamespacedToolName(maspec.MultiAgentV1Namespace, "spawn_agent"),
			searchText: maspec.SpawnAgentSearchText,
			buildSpec: func(*TurnContext) tools.ToolSpec {
				return maspec.CreateSpawnAgentToolV1(bareSpawnAgentOptions())
			},
			kind:    collabHandlerSpawn,
			control: control,
		},
		{
			name:       protocol.NamespacedToolName(maspec.MultiAgentV1Namespace, "send_input"),
			searchText: maspec.SendInputSearchText,
			buildSpec:  func(*TurnContext) tools.ToolSpec { return maspec.CreateSendInputToolV1() },
			kind:       collabHandlerSendInput,
			control:    control,
		},
		{
			name:       protocol.NamespacedToolName(maspec.MultiAgentV1Namespace, "resume_agent"),
			searchText: maspec.ResumeAgentSearchText,
			buildSpec:  func(*TurnContext) tools.ToolSpec { return maspec.CreateResumeAgentTool() },
			kind:       collabHandlerResume,
			control:    control,
		},
		{
			name:       protocol.NamespacedToolName(maspec.MultiAgentV1Namespace, "wait_agent"),
			searchText: maspec.WaitAgentSearchText,
			buildSpec: func(*TurnContext) tools.ToolSpec {
				return maspec.CreateWaitAgentToolV1(maspec.DefaultWaitAgentTimeoutOptions())
			},
			kind:    collabHandlerWait,
			control: control,
		},
		{
			name:       protocol.NamespacedToolName(maspec.MultiAgentV1Namespace, "close_agent"),
			searchText: maspec.CloseAgentSearchText,
			buildSpec:  func(*TurnContext) tools.ToolSpec { return maspec.CreateCloseAgentToolV1() },
			kind:       collabHandlerClose,
			control:    control,
		},
	}
}
