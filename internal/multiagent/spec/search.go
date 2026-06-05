package spec

import "github.com/sqlrush/codexgo/internal/tools"

// Search-text constants for the five collab tools, mirroring the literals in the
// Rust multi_agents handlers' `search_info` methods.
const (
	// SpawnAgentSearchText mirrors multi_agents/spawn.rs search_info.
	SpawnAgentSearchText = "spawn_agent spawn agent subagent sub-agent delegate delegation parallel work worker explorer no-apps fork model reasoning"
	// SendInputSearchText mirrors multi_agents/send_input.rs search_info.
	SendInputSearchText = "send_input send message existing agent subagent follow up interrupt redirect queue target"
	// ResumeAgentSearchText mirrors multi_agents/resume_agent.rs search_info.
	ResumeAgentSearchText = "resume_agent resume reopen closed agent subagent thread id target"
	// WaitAgentSearchText mirrors multi_agents/wait.rs search_info.
	WaitAgentSearchText = "wait_agent wait agent subagent status final result complete timeout targets"
	// CloseAgentSearchText mirrors multi_agents/close_agent.rs search_info.
	CloseAgentSearchText = "close_agent close shutdown stop agent subagent thread status target"
)

// Multi-agent tool_search source descriptor, mirroring the Rust
// MULTI_AGENT_TOOL_SEARCH_SOURCE_NAME / _DESCRIPTION constants in
// handlers/multi_agents.rs.
const (
	multiAgentToolSearchSourceName        = "Multi-agent tools"
	multiAgentToolSearchSourceDescription = "Spawn and manage sub-agents."
)

// MultiAgentToolSearchSource returns the search-source descriptor the collab
// agent tools contribute. Mirrors `multi_agent_tool_search_info`'s source_info.
func MultiAgentToolSearchSource() tools.ToolSearchSourceInfo {
	description := multiAgentToolSearchSourceDescription
	return tools.ToolSearchSourceInfo{Name: multiAgentToolSearchSourceName, Description: &description}
}
