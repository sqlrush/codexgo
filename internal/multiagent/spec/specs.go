package spec

import (
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo/internal/modelsmanager"
	"github.com/sqlrush/codexgo/internal/tools"
)

// MultiAgentV1Namespace is the namespace name shared by all v1 collab tools.
// Mirrors Rust `MULTI_AGENT_V1_NAMESPACE`.
const MultiAgentV1Namespace = "multi_agent_v1"

// multiAgentV1NamespaceDescription mirrors Rust
// `MULTI_AGENT_V1_NAMESPACE_DESCRIPTION`.
const multiAgentV1NamespaceDescription = "Tools for spawning and managing sub-agents."

// spawnAgentInheritedModelGuidance mirrors Rust
// `SPAWN_AGENT_INHERITED_MODEL_GUIDANCE`.
const spawnAgentInheritedModelGuidance = "Spawned agents inherit your current model by default. If provided, `model` specifies the model to use for the spawned agent."

// spawnAgentModelOverrideDescription mirrors Rust
// `SPAWN_AGENT_MODEL_OVERRIDE_DESCRIPTION`.
const spawnAgentModelOverrideDescription = "Model override for the new agent. Omit to inherit the parent model."

// spawnAgentServiceTierOverrideDescription mirrors Rust
// `SPAWN_AGENT_SERVICE_TIER_OVERRIDE_DESCRIPTION`.
const spawnAgentServiceTierOverrideDescription = "Service tier override for the new agent. Omit unless explicitly requested."

// maxModelOverridesInSpawnAgentDescription mirrors Rust
// `MAX_MODEL_OVERRIDES_IN_SPAWN_AGENT_DESCRIPTION`.
const maxModelOverridesInSpawnAgentDescription = 5

// Wait-timeout bounds, mirroring multi_agents_common.rs which derives them from
// config: DEFAULT = 30_000, MIN = DEFAULT_MULTI_AGENT_V2_MIN_WAIT_TIMEOUT_MS
// (10_000), MAX = HARD_MAX_MULTI_AGENT_V2_TIMEOUT_MS (3600*1000).
const (
	defaultWaitTimeoutMS int64 = 30_000
	minWaitTimeoutMS     int64 = 10_000
	maxWaitTimeoutMS     int64 = 3_600_000
)

// SpawnAgentToolOptions configures the spawn_agent v1 spec. Mirrors Rust
// `SpawnAgentToolOptions`.
type SpawnAgentToolOptions struct {
	// AvailableModels are the picker-ready model presets used to render the model
	// override guidance. Mirrors `available_models`.
	AvailableModels []modelsmanager.ModelPreset
	// AgentTypeDescription is the rendered agent_type parameter description.
	AgentTypeDescription string
	// HideAgentTypeModelReasoning drops the agent_type/model/reasoning/service
	// properties and the model guidance. Mirrors `hide_agent_type_model_reasoning`.
	HideAgentTypeModelReasoning bool
	// IncludeUsageHint appends the long usage-guidance block. Mirrors
	// `include_usage_hint`.
	IncludeUsageHint bool
	// UsageHintText, when non-nil, replaces the default usage block. Mirrors
	// `usage_hint_text`.
	UsageHintText *string
	// MaxConcurrentThreadsPerSession is unused by v1 (v2 only); retained for
	// parity. Mirrors `max_concurrent_threads_per_session`.
	MaxConcurrentThreadsPerSession *int
}

// WaitAgentTimeoutOptions configures the wait_agent v1 timeout description.
// Mirrors Rust `WaitAgentTimeoutOptions`.
type WaitAgentTimeoutOptions struct {
	DefaultTimeoutMS int64
	MinTimeoutMS     int64
	MaxTimeoutMS     int64
}

// DefaultWaitAgentTimeoutOptions returns the default timeout bounds. Mirrors the
// Rust `impl Default for WaitAgentTimeoutOptions`.
func DefaultWaitAgentTimeoutOptions() WaitAgentTimeoutOptions {
	return WaitAgentTimeoutOptions{
		DefaultTimeoutMS: defaultWaitTimeoutMS,
		MinTimeoutMS:     minWaitTimeoutMS,
		MaxTimeoutMS:     maxWaitTimeoutMS,
	}
}

func strPtr(s string) *string { return &s }

// namespaceSpec wraps a single function tool in the multi_agent_v1 namespace.
// Mirrors the shared `ToolSpec::Namespace(ResponsesApiNamespace { ... })`
// construction in the v1 builders. Output schemas are intentionally omitted: the
// tool_search ToolSearchInfo conversion strips output_schema, so the v1 search
// specs never need it. See DEVIATIONS.
func namespaceSpec(tool tools.ResponsesApiTool) tools.ToolSpec {
	return tools.NamespaceToolSpec(tools.ResponsesApiNamespace{
		Name:        MultiAgentV1Namespace,
		Description: multiAgentV1NamespaceDescription,
		Tools:       []tools.ResponsesApiNamespaceTool{tools.FunctionNamespaceTool(tool)},
	})
}

// CreateSpawnAgentToolV1 builds the spawn_agent v1 ToolSpec. Mirrors Rust
// `create_spawn_agent_tool_v1`.
func CreateSpawnAgentToolV1(options SpawnAgentToolOptions) tools.ToolSpec {
	var availableModelsDescription *string
	if !options.HideAgentTypeModelReasoning {
		availableModelsDescription = strPtr(spawnAgentModelsDescription(options.AvailableModels))
	}
	returnValueDescription := "Returns the spawned agent id plus the user-facing nickname when available."

	properties := spawnAgentCommonPropertiesV1(options.AgentTypeDescription)
	if options.HideAgentTypeModelReasoning {
		hideSpawnAgentMetadataOptions(properties)
	}

	tool := tools.ResponsesApiTool{
		Name: "spawn_agent",
		Description: spawnAgentToolDescription(
			availableModelsDescription,
			returnValueDescription,
			options.IncludeUsageHint,
			options.UsageHintText,
		),
		Strict:     false,
		Parameters: tools.ObjectSchema(properties, nil, tools.BoolAdditionalProperties(false)),
	}
	return namespaceSpec(tool)
}

// CreateSendInputToolV1 builds the send_input v1 ToolSpec. Mirrors Rust
// `create_send_input_tool_v1`.
func CreateSendInputToolV1() tools.ToolSpec {
	properties := map[string]tools.JsonSchema{
		"target":  tools.StringSchema(strPtr("Agent id to message (from spawn_agent).")),
		"message": tools.StringSchema(strPtr("Legacy plain-text message to send to the agent. Use either message or items.")),
		"items":   createCollabInputItemsSchema(),
		"interrupt": tools.BooleanSchema(strPtr(
			"True interrupts the current task and handles this message immediately; false or omitted queues it.")),
	}
	tool := tools.ResponsesApiTool{
		Name:        "send_input",
		Description: "Send a message to an existing agent. Use interrupt=true to redirect work immediately. You should reuse the agent by send_input if you believe your assigned task is highly dependent on the context of a previous task.",
		Strict:      false,
		Parameters:  tools.ObjectSchema(properties, []string{"target"}, tools.BoolAdditionalProperties(false)),
	}
	return namespaceSpec(tool)
}

// CreateResumeAgentTool builds the resume_agent v1 ToolSpec. Mirrors Rust
// `create_resume_agent_tool`.
func CreateResumeAgentTool() tools.ToolSpec {
	properties := map[string]tools.JsonSchema{
		"id": tools.StringSchema(strPtr("Agent id to resume.")),
	}
	tool := tools.ResponsesApiTool{
		Name:        "resume_agent",
		Description: "Resume a previously closed agent by id so it can receive send_input and wait_agent calls.",
		Strict:      false,
		Parameters:  tools.ObjectSchema(properties, []string{"id"}, tools.BoolAdditionalProperties(false)),
	}
	return namespaceSpec(tool)
}

// CreateWaitAgentToolV1 builds the wait_agent v1 ToolSpec. Mirrors Rust
// `create_wait_agent_tool_v1`.
func CreateWaitAgentToolV1(options WaitAgentTimeoutOptions) tools.ToolSpec {
	tool := tools.ResponsesApiTool{
		Name:        "wait_agent",
		Description: "Wait for agents to reach a final status. Completed statuses may include the agent's final message. Returns empty status when timed out. Once the agent reaches a final status, a notification message will be received containing the same completed status.",
		Strict:      false,
		Parameters:  waitAgentToolParametersV1(options),
	}
	return namespaceSpec(tool)
}

// CreateCloseAgentToolV1 builds the close_agent v1 ToolSpec. Mirrors Rust
// `create_close_agent_tool_v1`.
func CreateCloseAgentToolV1() tools.ToolSpec {
	properties := map[string]tools.JsonSchema{
		"target": tools.StringSchema(strPtr("Agent id to close (from spawn_agent).")),
	}
	tool := tools.ResponsesApiTool{
		Name:        "close_agent",
		Description: "Close an agent and any open descendants when they are no longer needed, and return the target agent's previous status before shutdown was requested. Don't keep agents open for too long if they are not needed anymore.",
		Strict:      false,
		Parameters:  tools.ObjectSchema(properties, []string{"target"}, tools.BoolAdditionalProperties(false)),
	}
	return namespaceSpec(tool)
}

// createCollabInputItemsSchema builds the shared `items` array schema. Mirrors
// Rust `create_collab_input_items_schema`.
func createCollabInputItemsSchema() tools.JsonSchema {
	properties := map[string]tools.JsonSchema{
		"type":      tools.StringSchema(strPtr("Input item type: text, image, local_image, skill, or mention.")),
		"text":      tools.StringSchema(strPtr("Text content when type is text.")),
		"image_url": tools.StringSchema(strPtr("Image URL when type is image.")),
		"path": tools.StringSchema(strPtr(
			"Path when type is local_image/skill, or structured mention target such as app://<connector-id> or plugin://<plugin-name>@<marketplace-name> when type is mention.")),
		"name": tools.StringSchema(strPtr("Display name when type is skill or mention.")),
	}
	itemSchema := tools.ObjectSchema(properties, nil, tools.BoolAdditionalProperties(false))
	return tools.ArraySchema(itemSchema, strPtr(
		"Structured input items. Use this to pass explicit mentions (for example app:// connector paths)."))
}

// spawnAgentCommonPropertiesV1 builds the shared spawn_agent v1 properties.
// Mirrors Rust `spawn_agent_common_properties_v1`.
func spawnAgentCommonPropertiesV1(agentTypeDescription string) map[string]tools.JsonSchema {
	return map[string]tools.JsonSchema{
		"message":    tools.StringSchema(strPtr("Initial plain-text task for the new agent. Use either message or items.")),
		"items":      createCollabInputItemsSchema(),
		"agent_type": tools.StringSchema(strPtr(agentTypeDescription)),
		"fork_context": tools.BooleanSchema(strPtr(
			"True forks the current thread history into the new agent; false or omitted starts with only the initial prompt.")),
		"model":            tools.StringSchema(strPtr(spawnAgentModelOverrideDescription)),
		"reasoning_effort": tools.StringSchema(strPtr("Reasoning effort override for the new agent. Omit to inherit the parent effort.")),
		"service_tier":     tools.StringSchema(strPtr(spawnAgentServiceTierOverrideDescription)),
	}
}

// hideSpawnAgentMetadataOptions removes the metadata properties. Mirrors Rust
// `hide_spawn_agent_metadata_options`.
func hideSpawnAgentMetadataOptions(properties map[string]tools.JsonSchema) {
	delete(properties, "agent_type")
	delete(properties, "model")
	delete(properties, "reasoning_effort")
	delete(properties, "service_tier")
}

// spawnAgentToolDescription renders the long spawn_agent description. Mirrors
// Rust `spawn_agent_tool_description`, including the literal leading newline and
// the "\n        " indentation produced by the raw-string format! templates.
func spawnAgentToolDescription(
	availableModelsDescription *string,
	returnValueDescription string,
	includeUsageHint bool,
	usageHintText *string,
) string {
	agentRoleGuidance := ""
	if availableModelsDescription != nil {
		agentRoleGuidance = *availableModelsDescription
	}

	toolDescription := fmt.Sprintf(
		"\n        %s\n        Spawn a sub-agent for a well-scoped task. %s %s",
		agentRoleGuidance, returnValueDescription, spawnAgentInheritedModelGuidance,
	)

	if !includeUsageHint {
		return toolDescription
	}
	if usageHintText != nil {
		return fmt.Sprintf("\n        %s\n%s", toolDescription, *usageHintText)
	}

	agentRoleUsageHint := ""
	if availableModelsDescription != nil {
		agentRoleUsageHint = "Agent-role guidance below only helps choose which agent to use after spawning is already authorized; it never authorizes spawning by itself."
	}
	return fmt.Sprintf("\n        %s\n%s", toolDescription, spawnAgentUsageBlock(agentRoleUsageHint))
}

// spawnAgentUsageBlock renders the usage-guidance body that follows the tool
// description when include_usage_hint is set and no override text is provided.
// Mirrors the trailing raw string of Rust `spawn_agent_tool_description`.
func spawnAgentUsageBlock(agentRoleUsageHint string) string {
	return fmt.Sprintf(`This spawn_agent tool provides you access to sub-agents that inherit your current model by default. Do not set the `+"`model`"+` field unless the user explicitly asks for a different model or there is a clear task-specific reason. You should follow the rules and guidelines below to use this tool.

Only use `+"`spawn_agent`"+` if and only if the user explicitly asks for sub-agents, delegation, or parallel agent work.
Requests for depth, thoroughness, research, investigation, or detailed codebase analysis do not count as permission to spawn.
%s

### When to delegate vs. do the subtask yourself
- First, quickly analyze the overall user task and form a succinct high-level plan. Identify which tasks are immediate blockers on the critical path, and which tasks are sidecar tasks that are needed but can run in parallel without blocking the next local step. As part of that plan, explicitly decide what immediate task you should do locally right now. Do this planning step before delegating to agents so you do not hand off the immediate blocking task to a submodel and then waste time waiting on it.
- Use a subagent when a subtask is easy enough for it to handle and can run in parallel with your local work. Prefer delegating concrete, bounded sidecar tasks that materially advance the main task without blocking your immediate next local step.
- Do not delegate urgent blocking work when your immediate next step depends on that result. If the very next action is blocked on that task, the main rollout should usually do it locally to keep the critical path moving.
- Keep work local when the subtask is too difficult to delegate well and when it is tightly coupled, urgent, or likely to block your immediate next step.

### Designing delegated subtasks
- Subtasks must be concrete, well-defined, and self-contained.
- Delegated subtasks must materially advance the main task.
- Do not duplicate work between the main rollout and delegated subtasks.
- Avoid issuing multiple delegate calls on the same unresolved thread unless the new delegated task is genuinely different and necessary.
- Narrow the delegated ask to the concrete output you need next.
- For coding tasks, prefer delegating concrete code-change worker subtasks over read-only explorer analysis when the subagent can make a bounded patch in a clear write scope.
- When delegating coding work, instruct the submodel to edit files directly in its forked workspace and list the file paths it changed in the final answer.
- For code-edit subtasks, decompose work so each delegated task has a disjoint write set.

### After you delegate
- Call wait_agent very sparingly. Only call wait_agent when you need the result immediately for the next critical-path step and you are blocked until it returns.
- Do not redo delegated subagent tasks yourself; focus on integrating results or tackling non-overlapping work.
- While the subagent is running in the background, do meaningful non-overlapping work immediately.
- Do not repeatedly wait by reflex.
- When a delegated coding task returns, quickly review the uploaded changes, then integrate or refine them.

### Parallel delegation patterns
- Run multiple independent information-seeking subtasks in parallel when you have distinct questions that can be answered independently.
- Split implementation into disjoint codebase slices and spawn multiple agents for them in parallel when the write scopes do not overlap.
- Delegate verification only when it can run in parallel with ongoing implementation and is likely to catch a concrete risk before final integration.
- The key is to find opportunities to spawn multiple independent subtasks in parallel within the same round, while ensuring each subtask is well-defined, self-contained, and materially advances the main task.`, agentRoleUsageHint)
}

// spawnAgentModelsDescription renders the picker-visible model overrides. Mirrors
// Rust `spawn_agent_models_description`.
func spawnAgentModelsDescription(models []modelsmanager.ModelPreset) string {
	visible := make([]modelsmanager.ModelPreset, 0, maxModelOverridesInSpawnAgentDescription)
	for _, model := range models {
		if !model.ShowInPicker {
			continue
		}
		visible = append(visible, model)
		if len(visible) == maxModelOverridesInSpawnAgentDescription {
			break
		}
	}
	if len(visible) == 0 {
		return "No picker-visible model overrides are currently loaded."
	}

	lines := make([]string, 0, len(visible))
	for _, model := range visible {
		efforts := make([]string, 0, len(model.SupportedReasoningEfforts))
		for _, preset := range model.SupportedReasoningEfforts {
			effort := string(preset.Effort)
			if preset.Effort == model.DefaultReasoningEffort {
				effort = fmt.Sprintf("%s (default)", effort)
			}
			efforts = append(efforts, effort)
		}
		reasoningEffortsSuffix := ""
		if len(efforts) > 0 {
			reasoningEffortsSuffix = fmt.Sprintf(" Reasoning efforts: %s.", strings.Join(efforts, ", "))
		}

		tiers := make([]string, 0, len(model.ServiceTiers))
		for _, tier := range model.ServiceTiers {
			tiers = append(tiers, tier.ID)
		}
		serviceTiersSuffix := ""
		if len(tiers) > 0 {
			serviceTiersSuffix = fmt.Sprintf(" Service tiers: %s.", strings.Join(tiers, ", "))
		}

		lines = append(lines, fmt.Sprintf("- `%s`: %s%s%s",
			model.Model, model.Description, reasoningEffortsSuffix, serviceTiersSuffix))
	}
	return fmt.Sprintf(
		"Available model overrides (optional; inherited parent model is preferred):\n%s",
		strings.Join(lines, "\n"),
	)
}

// waitAgentToolParametersV1 builds the wait_agent v1 parameter schema. Mirrors
// Rust `wait_agent_tool_parameters_v1`.
func waitAgentToolParametersV1(options WaitAgentTimeoutOptions) tools.JsonSchema {
	properties := map[string]tools.JsonSchema{
		"targets": tools.ArraySchema(
			tools.StringSchema(nil),
			strPtr("Agent ids to wait on. Pass multiple ids to wait for whichever finishes first."),
		),
		"timeout_ms": tools.NumberSchema(strPtr(fmt.Sprintf(
			"Timeout in milliseconds. Defaults to %d, min %d, max %d. Prefer longer waits (minutes) to avoid busy polling.",
			options.DefaultTimeoutMS, options.MinTimeoutMS, options.MaxTimeoutMS,
		))),
	}
	return tools.ObjectSchema(properties, []string{"targets"}, tools.BoolAdditionalProperties(false))
}
