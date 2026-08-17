package rollout

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// SessionSourceKind enumerates the variants of SessionSource.
//
// Mirrors the Rust `codex_protocol::protocol::SessionSource` enum, which is an
// externally tagged enum with `#[serde(rename_all = "lowercase")]`. Unit
// variants serialize as bare strings ("cli", "vscode", "exec", "mcp",
// "unknown"); the data-carrying variants serialize as single-key objects
// ({"custom": ...}, {"internal": ...}, {"subagent": ...}).
type SessionSourceKind string

const (
	// SessionSourceKindCli is the interactive CLI source.
	SessionSourceKindCli SessionSourceKind = "cli"
	// SessionSourceKindVSCode is the VS Code extension source. This is the
	// default variant.
	SessionSourceKindVSCode SessionSourceKind = "vscode"
	// SessionSourceKindExec is the non-interactive exec source.
	SessionSourceKindExec SessionSourceKind = "exec"
	// SessionSourceKindMcp is the MCP / app-server source.
	SessionSourceKindMcp SessionSourceKind = "mcp"
	// SessionSourceKindCustom is a user-defined named source (e.g. "atlas").
	SessionSourceKindCustom SessionSourceKind = "custom"
	// SessionSourceKindInternal is an internally-spawned source.
	SessionSourceKindInternal SessionSourceKind = "internal"
	// SessionSourceKindSubAgent is a sub-agent source.
	SessionSourceKindSubAgent SessionSourceKind = "subagent"
	// SessionSourceKindUnknown is the `#[serde(other)]` fallback.
	SessionSourceKindUnknown SessionSourceKind = "unknown"
)

// SessionSource classifies the origin of a session. Exactly one of the
// data-carrying fields is populated for the corresponding Kind; unit variants
// carry only the Kind.
type SessionSource struct {
	Kind SessionSourceKind

	// Custom holds the named source for SessionSourceKindCustom.
	Custom string
	// Internal holds the internal source for SessionSourceKindInternal.
	Internal *InternalSessionSource
	// SubAgent holds the sub-agent source for SessionSourceKindSubAgent.
	SubAgent *SubAgentSource
}

// DefaultSessionSource returns the default SessionSource (VSCode), mirroring the
// Rust `#[default]` on the `VSCode` variant.
func DefaultSessionSource() SessionSource {
	return SessionSource{Kind: SessionSourceKindVSCode}
}

// NewCliSource returns the CLI session source.
func NewCliSource() SessionSource { return SessionSource{Kind: SessionSourceKindCli} }

// NewVSCodeSource returns the VS Code session source.
func NewVSCodeSource() SessionSource { return SessionSource{Kind: SessionSourceKindVSCode} }

// NewExecSource returns the exec session source.
func NewExecSource() SessionSource { return SessionSource{Kind: SessionSourceKindExec} }

// NewMcpSource returns the MCP session source.
func NewMcpSource() SessionSource { return SessionSource{Kind: SessionSourceKindMcp} }

// NewCustomSource returns a custom named session source.
func NewCustomSource(name string) SessionSource {
	return SessionSource{Kind: SessionSourceKindCustom, Custom: name}
}

// String renders the human-readable session source, mirroring the Rust
// `Display` implementation.
func (s SessionSource) String() string {
	switch s.Kind {
	case SessionSourceKindCli:
		return "cli"
	case SessionSourceKindVSCode:
		return "vscode"
	case SessionSourceKindExec:
		return "exec"
	case SessionSourceKindMcp:
		return "mcp"
	case SessionSourceKindCustom:
		return s.Custom
	case SessionSourceKindInternal:
		if s.Internal != nil {
			return "internal_" + s.Internal.String()
		}
		return "internal_"
	case SessionSourceKindSubAgent:
		if s.SubAgent != nil {
			return "subagent_" + s.SubAgent.String()
		}
		return "subagent_"
	default:
		return "unknown"
	}
}

// IsInternal reports whether the source is an internal source.
func (s SessionSource) IsInternal() bool { return s.Kind == SessionSourceKindInternal }

// IsNonRootAgent reports whether the source is internal or a sub-agent.
func (s SessionSource) IsNonRootAgent() bool {
	return s.Kind == SessionSourceKindInternal || s.Kind == SessionSourceKindSubAgent
}

// Nickname returns the nickname assigned to a ThreadSpawn sub-agent, if any.
func (s SessionSource) Nickname() *string {
	if s.Kind == SessionSourceKindSubAgent && s.SubAgent != nil &&
		s.SubAgent.Kind == SubAgentSourceKindThreadSpawn {
		return s.SubAgent.ThreadSpawn.AgentNickname
	}
	return nil
}

// AgentRole returns the agent role assigned to a ThreadSpawn sub-agent, if any.
func (s SessionSource) AgentRole() *string {
	if s.Kind == SessionSourceKindSubAgent && s.SubAgent != nil &&
		s.SubAgent.Kind == SubAgentSourceKindThreadSpawn {
		return s.SubAgent.ThreadSpawn.AgentRole
	}
	return nil
}

// AgentPath returns the canonical agent path assigned to a ThreadSpawn
// sub-agent, if any.
func (s SessionSource) AgentPath() *protocol.AgentPath {
	if s.Kind == SessionSourceKindSubAgent && s.SubAgent != nil &&
		s.SubAgent.Kind == SubAgentSourceKindThreadSpawn {
		return s.SubAgent.ThreadSpawn.AgentPath
	}
	return nil
}

// SessionSourceFromStartupArg parses a session source supplied as a startup
// argument, mirroring the Rust `from_startup_arg`.
func SessionSourceFromStartupArg(value string) (SessionSource, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return SessionSource{}, fmt.Errorf("session source must not be empty")
	}
	normalized := strings.ToLower(trimmed)
	switch normalized {
	case "cli":
		return NewCliSource(), nil
	case "vscode":
		return NewVSCodeSource(), nil
	case "exec":
		return NewExecSource(), nil
	case "mcp", "appserver", "app-server", "app_server":
		return NewMcpSource(), nil
	case "unknown":
		return SessionSource{Kind: SessionSourceKindUnknown}, nil
	default:
		return NewCustomSource(normalized), nil
	}
}

// MarshalJSON encodes the SessionSource as either a bare string (unit variants)
// or a single-key object (data variants).
func (s SessionSource) MarshalJSON() ([]byte, error) {
	switch s.Kind {
	case SessionSourceKindCli, SessionSourceKindVSCode, SessionSourceKindExec,
		SessionSourceKindMcp, SessionSourceKindUnknown:
		return json.Marshal(string(s.Kind))
	case SessionSourceKindCustom:
		return json.Marshal(map[string]string{"custom": s.Custom})
	case SessionSourceKindInternal:
		return json.Marshal(map[string]*InternalSessionSource{"internal": s.Internal})
	case SessionSourceKindSubAgent:
		return json.Marshal(map[string]*SubAgentSource{"subagent": s.SubAgent})
	default:
		return json.Marshal(string(SessionSourceKindUnknown))
	}
}

// UnmarshalJSON decodes a SessionSource from a bare string or single-key object.
// Unrecognized strings and objects fall back to the Unknown variant, mirroring
// the Rust `#[serde(other)]`.
func (s *SessionSource) UnmarshalJSON(data []byte) error {
	var asString string
	if err := json.Unmarshal(data, &asString); err == nil {
		switch SessionSourceKind(asString) {
		case SessionSourceKindCli:
			*s = SessionSource{Kind: SessionSourceKindCli}
		case SessionSourceKindVSCode:
			*s = SessionSource{Kind: SessionSourceKindVSCode}
		case SessionSourceKindExec:
			*s = SessionSource{Kind: SessionSourceKindExec}
		case SessionSourceKindMcp:
			*s = SessionSource{Kind: SessionSourceKindMcp}
		default:
			*s = SessionSource{Kind: SessionSourceKindUnknown}
		}
		return nil
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		*s = SessionSource{Kind: SessionSourceKindUnknown}
		return nil
	}
	if raw, ok := obj["custom"]; ok {
		var name string
		if err := json.Unmarshal(raw, &name); err != nil {
			return fmt.Errorf("decode custom session source: %w", err)
		}
		*s = SessionSource{Kind: SessionSourceKindCustom, Custom: name}
		return nil
	}
	if raw, ok := obj["internal"]; ok {
		var inner InternalSessionSource
		if err := json.Unmarshal(raw, &inner); err != nil {
			return fmt.Errorf("decode internal session source: %w", err)
		}
		*s = SessionSource{Kind: SessionSourceKindInternal, Internal: &inner}
		return nil
	}
	if raw, ok := obj["subagent"]; ok {
		var inner SubAgentSource
		if err := json.Unmarshal(raw, &inner); err != nil {
			return fmt.Errorf("decode subagent session source: %w", err)
		}
		*s = SessionSource{Kind: SessionSourceKindSubAgent, SubAgent: &inner}
		return nil
	}
	*s = SessionSource{Kind: SessionSourceKindUnknown}
	return nil
}

// ThreadSource is an optional analytics classification for a thread.
//
// Mirrors the Rust `ThreadSource` enum with `#[serde(rename_all = "snake_case")]`.
type ThreadSource string

const (
	// ThreadSourceUser is a user-initiated thread.
	ThreadSourceUser ThreadSource = "user"
	// ThreadSourceSubagent is a sub-agent thread.
	ThreadSourceSubagent ThreadSource = "subagent"
	// ThreadSourceMemoryConsolidation is a memory-consolidation thread.
	ThreadSourceMemoryConsolidation ThreadSource = "memory_consolidation"
)

// String returns the snake_case wire string for the thread source.
func (t ThreadSource) String() string { return string(t) }

// ParseThreadSource parses a thread source from its wire string.
func ParseThreadSource(value string) (ThreadSource, error) {
	switch value {
	case "user", "subagent", "memory_consolidation":
		return ThreadSource(value), nil
	default:
		return "", fmt.Errorf("unknown thread source: %s", value)
	}
}

// InternalSessionSource enumerates internal session sources.
//
// Mirrors the Rust `InternalSessionSource` enum with snake_case renaming. It is
// an externally tagged enum of unit variants, which serializes as a bare string.
type InternalSessionSource string

const (
	// InternalSessionSourceMemoryConsolidation is the memory-consolidation
	// internal source.
	InternalSessionSourceMemoryConsolidation InternalSessionSource = "memory_consolidation"
)

// String returns the wire string for the internal source.
func (i InternalSessionSource) String() string { return string(i) }
