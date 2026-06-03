package rollout

import (
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// UnmarshalJSON decodes a SessionMeta, applying the Rust serde defaults and
// aliases:
//   - `source` defaults to VSCode when absent (`#[serde(default)]`).
//   - `agent_role` accepts the legacy `agent_type` alias.
func (m *SessionMeta) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID               protocol.ThreadID           `json:"id"`
		ForkedFromID     *protocol.ThreadID          `json:"forked_from_id"`
		Timestamp        string                      `json:"timestamp"`
		Cwd              string                      `json:"cwd"`
		Originator       string                      `json:"originator"`
		CliVersion       string                      `json:"cli_version"`
		Source           *SessionSource              `json:"source"`
		ThreadSource     *ThreadSource               `json:"thread_source"`
		AgentNickname    *string                     `json:"agent_nickname"`
		AgentRole        *string                     `json:"agent_role"`
		AgentType        *string                     `json:"agent_type"`
		AgentPath        *string                     `json:"agent_path"`
		ModelProvider    *string                     `json:"model_provider"`
		BaseInstructions *BaseInstructions           `json:"base_instructions"`
		DynamicTools     *[]protocol.DynamicToolSpec `json:"dynamic_tools"`
		MemoryMode       *string                     `json:"memory_mode"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("decode session meta fields: %w", err)
	}

	source := DefaultSessionSource()
	if raw.Source != nil {
		source = *raw.Source
	}

	agentRole := raw.AgentRole
	if agentRole == nil {
		agentRole = raw.AgentType
	}

	*m = SessionMeta{
		ID:               raw.ID,
		ForkedFromID:     raw.ForkedFromID,
		Timestamp:        raw.Timestamp,
		Cwd:              raw.Cwd,
		Originator:       raw.Originator,
		CliVersion:       raw.CliVersion,
		Source:           source,
		ThreadSource:     raw.ThreadSource,
		AgentNickname:    raw.AgentNickname,
		AgentRole:        agentRole,
		AgentPath:        raw.AgentPath,
		ModelProvider:    raw.ModelProvider,
		BaseInstructions: raw.BaseInstructions,
		DynamicTools:     raw.DynamicTools,
		MemoryMode:       raw.MemoryMode,
	}
	return nil
}
