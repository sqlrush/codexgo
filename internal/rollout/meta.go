package rollout

import (
	"github.com/sqlrush/codexgo/internal/gitutils"
	"github.com/sqlrush/codexgo/internal/protocol"
)

// BaseInstructions holds the base instructions for a thread. It corresponds to
// the `instructions` field in the Responses API.
//
// Mirrors the Rust `codex_protocol::models::BaseInstructions` struct, which
// serializes a single `text` field.
type BaseInstructions struct {
	Text string `json:"text"`
}

// SessionMeta contains session-level data that does not correspond to a
// specific turn.
//
// Mirrors the Rust `codex_protocol::protocol::SessionMeta`. Field order is
// significant: the JSON key order matches the Rust struct declaration order so
// the on-disk format is byte-for-byte compatible after canonicalization.
//
// Note: `model_provider` and `base_instructions` have no
// `skip_serializing_if`, so they are ALWAYS emitted (as `null` when unset),
// matching the Rust serde output. The remaining optional fields are omitted
// when nil.
type SessionMeta struct {
	ID            protocol.ThreadID  `json:"id"`
	ForkedFromID  *protocol.ThreadID `json:"forked_from_id,omitempty"`
	Timestamp     string             `json:"timestamp"`
	Cwd           string             `json:"cwd"`
	Originator    string             `json:"originator"`
	CliVersion    string             `json:"cli_version"`
	Source        SessionSource      `json:"source"`
	ThreadSource  *ThreadSource      `json:"thread_source,omitempty"`
	AgentNickname *string            `json:"agent_nickname,omitempty"`
	AgentRole     *string            `json:"agent_role,omitempty"`
	AgentPath     *string            `json:"agent_path,omitempty"`
	ModelProvider *string            `json:"model_provider"`
	// BaseInstructions is always emitted (null when unset) to match Rust.
	BaseInstructions *BaseInstructions           `json:"base_instructions"`
	DynamicTools     *[]protocol.DynamicToolSpec `json:"dynamic_tools,omitempty"`
	MemoryMode       *string                     `json:"memory_mode,omitempty"`
}

// SessionMetaLine is the first line of a rollout file: the session metadata
// plus optional git information.
//
// Mirrors the Rust `SessionMetaLine`, which flattens the SessionMeta fields and
// appends an optional `git` object.
type SessionMetaLine struct {
	Meta SessionMeta
	// Git carries optional repository information; omitted when nil.
	Git *gitutils.GitInfo
}
