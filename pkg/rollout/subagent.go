package rollout

import (
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// SubAgentSourceKind enumerates the variants of SubAgentSource.
//
// Mirrors the Rust `SubAgentSource` externally tagged enum with
// `#[serde(rename_all = "snake_case")]`. The unit variants serialize as bare
// strings; the data variants serialize as single-key objects.
type SubAgentSourceKind string

const (
	// SubAgentSourceKindReview is the review sub-agent source.
	SubAgentSourceKindReview SubAgentSourceKind = "review"
	// SubAgentSourceKindCompact is the compact sub-agent source.
	SubAgentSourceKindCompact SubAgentSourceKind = "compact"
	// SubAgentSourceKindThreadSpawn is a thread-spawn sub-agent source.
	SubAgentSourceKindThreadSpawn SubAgentSourceKind = "thread_spawn"
	// SubAgentSourceKindMemoryConsolidation is the memory-consolidation
	// sub-agent source.
	SubAgentSourceKindMemoryConsolidation SubAgentSourceKind = "memory_consolidation"
	// SubAgentSourceKindOther is a named other sub-agent source.
	SubAgentSourceKindOther SubAgentSourceKind = "other"
)

// ThreadSpawnSource carries the fields of the ThreadSpawn sub-agent variant.
type ThreadSpawnSource struct {
	ParentThreadID protocol.ThreadID   `json:"parent_thread_id"`
	Depth          int32               `json:"depth"`
	AgentPath      *protocol.AgentPath `json:"agent_path,omitempty"`
	AgentNickname  *string             `json:"agent_nickname,omitempty"`
	AgentRole      *string             `json:"agent_role,omitempty"`
}

// SubAgentSource classifies a sub-agent session origin. Exactly one of the
// data-carrying fields is populated for the corresponding Kind.
type SubAgentSource struct {
	Kind SubAgentSourceKind

	// ThreadSpawn holds the data for SubAgentSourceKindThreadSpawn.
	ThreadSpawn *ThreadSpawnSource
	// Other holds the named source for SubAgentSourceKindOther.
	Other string
}

// String renders the sub-agent source, mirroring the Rust `Display`.
func (s SubAgentSource) String() string {
	switch s.Kind {
	case SubAgentSourceKindReview:
		return "review"
	case SubAgentSourceKindCompact:
		return "compact"
	case SubAgentSourceKindMemoryConsolidation:
		return "memory_consolidation"
	case SubAgentSourceKindThreadSpawn:
		if s.ThreadSpawn != nil {
			return fmt.Sprintf("thread_spawn_%s_d%d",
				s.ThreadSpawn.ParentThreadID.String(), s.ThreadSpawn.Depth)
		}
		return "thread_spawn"
	case SubAgentSourceKindOther:
		return s.Other
	default:
		return string(s.Kind)
	}
}

// MarshalJSON encodes the SubAgentSource as a bare string (unit variants) or a
// single-key object (data variants).
func (s SubAgentSource) MarshalJSON() ([]byte, error) {
	switch s.Kind {
	case SubAgentSourceKindReview, SubAgentSourceKindCompact,
		SubAgentSourceKindMemoryConsolidation:
		return json.Marshal(string(s.Kind))
	case SubAgentSourceKindThreadSpawn:
		return json.Marshal(map[string]*ThreadSpawnSource{"thread_spawn": s.ThreadSpawn})
	case SubAgentSourceKindOther:
		return json.Marshal(map[string]string{"other": s.Other})
	default:
		return nil, fmt.Errorf("unknown sub-agent source kind: %q", s.Kind)
	}
}

// UnmarshalJSON decodes a SubAgentSource from a bare string or single-key object.
func (s *SubAgentSource) UnmarshalJSON(data []byte) error {
	var asString string
	if err := json.Unmarshal(data, &asString); err == nil {
		switch SubAgentSourceKind(asString) {
		case SubAgentSourceKindReview, SubAgentSourceKindCompact,
			SubAgentSourceKindMemoryConsolidation:
			*s = SubAgentSource{Kind: SubAgentSourceKind(asString)}
			return nil
		default:
			return fmt.Errorf("unknown sub-agent source variant: %q", asString)
		}
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return fmt.Errorf("decode sub-agent source: %w", err)
	}
	if raw, ok := obj["thread_spawn"]; ok {
		var inner ThreadSpawnSource
		if err := json.Unmarshal(raw, &inner); err != nil {
			return fmt.Errorf("decode thread_spawn sub-agent source: %w", err)
		}
		*s = SubAgentSource{Kind: SubAgentSourceKindThreadSpawn, ThreadSpawn: &inner}
		return nil
	}
	if raw, ok := obj["other"]; ok {
		var name string
		if err := json.Unmarshal(raw, &name); err != nil {
			return fmt.Errorf("decode other sub-agent source: %w", err)
		}
		*s = SubAgentSource{Kind: SubAgentSourceKindOther, Other: name}
		return nil
	}
	return fmt.Errorf("unrecognized sub-agent source object")
}
