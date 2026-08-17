// Package spec builds the multi-agent (collab) tool specifications: the
// spawn_agent / send_input / resume_agent / wait_agent / close_agent v1 specs
// and the agent-role description rendered into spawn_agent's agent_type
// parameter. It is a faithful Go port of the Rust codex-core
// `tools/handlers/multi_agents_spec.rs` spec builders and the
// `agent/role.rs::spawn_tool_spec` description renderer.
//
// This lives in its own leaf package (not internal/multiagent) because
// internal/multiagent imports internal/core, while these spec builders must be
// importable by internal/core (for the deferred collab tool runtimes). The leaf
// depends only on internal/tools and internal/modelsmanager, avoiding an import
// cycle. See DEVIATIONS.
package spec

import (
	"fmt"
	"sort"
	"strings"
)

// defaultRoleName is the role used when a caller omits agent_type. Mirrors Rust
// `agent::role::DEFAULT_ROLE_NAME`.
const defaultRoleName = "default"

// builtInAgentRole is a built-in agent role declaration. Mirrors the fields of
// Rust `AgentRoleConfig` that affect the rendered description.
type builtInAgentRole struct {
	name        string
	description string
}

// builtInAgentRoles returns the built-in role declarations in BTreeMap (sorted
// by name) order. Mirrors Rust `agent::role::built_in::configs()`. The explorer
// role's config_file (explorer.toml) is empty in the bundle, so it contributes
// no locked-settings note; the awaiter role is commented out upstream.
func builtInAgentRoles() []builtInAgentRole {
	roles := []builtInAgentRole{
		{name: defaultRoleName, description: "Default agent."},
		{name: "explorer", description: explorerRoleDescription},
		{name: "worker", description: workerRoleDescription},
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i].name < roles[j].name })
	return roles
}

// explorerRoleDescription mirrors the Rust built_in explorer role description.
const explorerRoleDescription = `Use ` + "`explorer`" + ` for specific codebase questions.
Explorers are fast and authoritative.
They must be used to ask specific, well-scoped questions on the codebase.
Rules:
- In order to avoid redundant work, you should avoid exploring the same problem that explorers have already covered. Typically, you should trust the explorer results without additional verification. You are still allowed to inspect the code yourself to gain the needed context!
- You are encouraged to spawn up multiple explorers in parallel when you have multiple distinct questions to ask about the codebase that can be answered independently. This allows you to get more information faster without waiting for one question to finish before asking the next. While waiting for the explorer results, you can continue working on other local tasks that do not depend on those results. This parallelism is a key advantage of delegation, so use it whenever you have multiple questions to ask.
- Reuse existing explorers for related questions.`

// workerRoleDescription mirrors the Rust built_in worker role description.
const workerRoleDescription = `Use for execution and production work.
Typical tasks:
- Implement part of a feature
- Fix tests or bugs
- Split large refactors into independent chunks
Rules:
- Explicitly assign **ownership** of the task (files / responsibility). When the subtask involves code changes, you should clearly specify which files or modules the worker is responsible for. This helps avoid merge conflicts and ensures accountability. For example, you can say "Worker 1 is responsible for updating the authentication module, while Worker 2 will handle the database layer." By defining clear ownership, you can delegate more effectively and reduce coordination overhead.
- Always tell workers they are **not alone in the codebase**, and they should not revert the edits made by others, and they should adjust their implementation to accommodate the changes made by others. This is important because there may be multiple workers making changes in parallel, and they need to be aware of each other's work to avoid conflicts and ensure a cohesive final product.`

// AgentTypeDescription renders the spawn_agent `agent_type` parameter
// description from the built-in roles with no user-defined roles. Mirrors Rust
// `agent::role::spawn_tool_spec::build(&BTreeMap::new())` (built_in roles only,
// formatted via format_role with no locked-settings note).
func AgentTypeDescription() string {
	roles := builtInAgentRoles()
	formatted := make([]string, 0, len(roles))
	for _, role := range roles {
		formatted = append(formatted, formatRole(role))
	}
	return fmt.Sprintf(
		"Optional type name for the new agent. If omitted, `%s` is used.\nAvailable roles:\n%s",
		defaultRoleName,
		strings.Join(formatted, "\n"),
	)
}

// formatRole renders a single role block. Mirrors Rust
// `spawn_tool_spec::format_role` for a role with a description and no
// locked-settings note (the built-in roles' config files carry no
// model/reasoning/service_tier overrides in the bundle).
func formatRole(role builtInAgentRole) string {
	return fmt.Sprintf("%s: {\n%s\n}", role.name, role.description)
}
