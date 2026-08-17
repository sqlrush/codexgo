package spec

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/pkg/modelsmanager"
	"github.com/sqlrush/codexgo/pkg/tools"
)

// bareSpawnOptions builds the SpawnAgentToolOptions used in a default codex run:
// bundled picker-visible models, the built-in agent_type description, no hiding,
// usage hint enabled with no override text.
func bareSpawnOptions(t *testing.T) SpawnAgentToolOptions {
	t.Helper()
	resp, err := modelsmanager.BundledModelsResponse()
	if err != nil {
		t.Fatalf("bundled models: %v", err)
	}
	return SpawnAgentToolOptions{
		AvailableModels:             modelsmanager.BuildAvailableModelsForCatalog(resp.Models),
		AgentTypeDescription:        AgentTypeDescription(),
		HideAgentTypeModelReasoning: false,
		IncludeUsageHint:            true,
		UsageHintText:               nil,
	}
}

// loadableSearchSpec converts a v1 spec into the deferred LoadableToolSpec the
// tool_search output carries, mirroring ToolSearchInfo::from_spec.
func loadableSearchSpec(t *testing.T, spec tools.ToolSpec) tools.LoadableToolSpec {
	t.Helper()
	info, ok := tools.ToolSearchInfoFromSpec("search text", spec, nil)
	if !ok {
		t.Fatalf("spec not searchable")
	}
	return info.Entry.Output
}

// TestSpawnAgentModelsDescriptionMatchesGroundTruth asserts the rendered model
// overrides block matches the bundled-catalog rendering captured from codex.
func TestSpawnAgentModelsDescriptionMatchesGroundTruth(t *testing.T) {
	opts := bareSpawnOptions(t)
	got := spawnAgentModelsDescription(opts.AvailableModels)
	want := "Available model overrides (optional; inherited parent model is preferred):\n" +
		"- `gpt-5.5`: Frontier model for complex coding, research, and real-world work. Reasoning efforts: low, medium (default), high, xhigh. Service tiers: priority.\n" +
		"- `gpt-5.4`: Strong model for everyday coding. Reasoning efforts: low, medium (default), high, xhigh. Service tiers: priority.\n" +
		"- `gpt-5.4-mini`: Small, fast, and cost-efficient model for simpler coding tasks. Reasoning efforts: low, medium (default), high, xhigh.\n" +
		"- `gpt-5.3-codex`: Coding-optimized model. Reasoning efforts: low, medium (default), high, xhigh.\n" +
		"- `gpt-5.2`: Optimized for professional work and long-running agents. Reasoning efforts: low, medium (default), high, xhigh."
	if got != want {
		t.Errorf("models description mismatch\n got:  %q\n want: %q", got, want)
	}
}

// TestSpawnAgentDescriptionPrefix asserts the literal leading-indentation and
// model-block prefix of the spawn_agent description.
func TestSpawnAgentDescriptionPrefix(t *testing.T) {
	opts := bareSpawnOptions(t)
	desc := spawnAgentToolDescription(
		ptr(spawnAgentModelsDescription(opts.AvailableModels)),
		"Returns the spawned agent id plus the user-facing nickname when available.",
		true, nil,
	)
	wantPrefix := "\n        \n        Available model overrides (optional; inherited parent model is preferred):\n"
	if !strings.HasPrefix(desc, wantPrefix) {
		t.Errorf("description prefix mismatch\n got:  %q", desc[:min(len(desc), 80)])
	}
	if !strings.Contains(desc, "### Parallel delegation patterns") {
		t.Errorf("description missing usage block")
	}
}

// TestAgentTypeDescriptionMatchesGroundTruth asserts the agent_type description
// matches the role rendering captured from codex.
func TestAgentTypeDescriptionMatchesGroundTruth(t *testing.T) {
	got := AgentTypeDescription()
	wantPrefix := "Optional type name for the new agent. If omitted, `default` is used.\nAvailable roles:\ndefault: {\nDefault agent.\n}\nexplorer: {\nUse `explorer` for specific codebase questions."
	if !strings.HasPrefix(got, wantPrefix) {
		t.Errorf("agent_type prefix mismatch\n got: %q", got[:min(len(got), 120)])
	}
	if !strings.Contains(got, "worker: {\nUse for execution and production work.") {
		t.Errorf("agent_type missing worker role")
	}
	if !strings.HasSuffix(got, "and ensure a cohesive final product.\n}") {
		t.Errorf("agent_type suffix mismatch\n got tail: %q", got[max(0, len(got)-60):])
	}
}

// TestSpawnAgentSpecWireBytes asserts the spawn_agent v1 deferred spec serializes
// byte-identically to the spawn_agent block of the codex ground truth.
func TestSpawnAgentSpecWireBytes(t *testing.T) {
	opts := bareSpawnOptions(t)
	loadable := loadableSearchSpec(t, CreateSpawnAgentToolV1(opts))
	// The loadable spec is a namespace with one function tool (spawn_agent).
	if loadable.Kind != tools.LoadableToolSpecKindNamespace {
		t.Fatalf("loadable kind = %v, want namespace", loadable.Kind)
	}
	raw, err := json.Marshal(loadable.Namespace.Tools[0])
	if err != nil {
		t.Fatalf("marshal spawn_agent tool: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["defer_loading"] != true {
		t.Errorf("defer_loading = %v, want true", got["defer_loading"])
	}
	if got["strict"] != false {
		t.Errorf("strict = %v, want false", got["strict"])
	}
	desc, _ := got["description"].(string)
	if !strings.HasPrefix(desc, "\n        \n        Available model overrides") {
		t.Errorf("spawn_agent description prefix mismatch: %q", desc[:min(len(desc), 60)])
	}
}

func ptr(s string) *string { return &s }

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
