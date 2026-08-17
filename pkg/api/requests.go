package api

import (
	"encoding/json"
	"net/http"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// Compression mirrors the Rust `Compression` enum used by the responses client.
type Compression int

const (
	// CompressionNone sends the body uncompressed.
	CompressionNone Compression = iota
	// CompressionZstd compresses the body with zstd.
	CompressionZstd
)

// SubAgentSourceKind discriminates the SubAgentSource variants that drive the
// x-openai-subagent header. The full SessionSource enum is not part of the
// shared protocol package, so the API package models only the subset needed
// here.
type SubAgentSourceKind int

const (
	// SubAgentReview corresponds to the "review" subagent label.
	SubAgentReview SubAgentSourceKind = iota
	// SubAgentCompact corresponds to the "compact" subagent label.
	SubAgentCompact
	// SubAgentMemoryConsolidation corresponds to "memory_consolidation".
	SubAgentMemoryConsolidation
	// SubAgentThreadSpawn corresponds to "collab_spawn".
	SubAgentThreadSpawn
	// SubAgentOther carries an arbitrary label.
	SubAgentOther
)

// SubAgentSource identifies a subagent session source. It mirrors the subset of
// the Rust `SubAgentSource` enum consumed by `subagent_header`.
type SubAgentSource struct {
	Kind  SubAgentSourceKind
	Label string // used when Kind == SubAgentOther
}

// SessionSource identifies the session origin. Only the SubAgent case influences
// request headers, so this models that case plus an "is subagent" flag.
type SessionSource struct {
	// IsSubAgent reports whether this source is a subagent session.
	IsSubAgent bool
	// SubAgent carries the subagent details when IsSubAgent is true.
	SubAgent SubAgentSource
}

// NewSubAgentSession builds a SessionSource for a subagent.
func NewSubAgentSession(sub SubAgentSource) SessionSource {
	return SessionSource{IsSubAgent: true, SubAgent: sub}
}

// BuildSessionHeaders builds the session-id / thread-id headers. It mirrors the
// Rust `build_session_headers`.
func BuildSessionHeaders(sessionID, threadID *string) http.Header {
	headers := http.Header{}
	if sessionID != nil {
		insertHeader(headers, "session-id", *sessionID)
	}
	if threadID != nil {
		insertHeader(headers, "thread-id", *threadID)
	}
	return headers
}

// subagentHeader returns the x-openai-subagent value for a session source, or
// ("", false) when the source is not a subagent. It mirrors the Rust
// `subagent_header`.
func subagentHeader(source *SessionSource) (string, bool) {
	if source == nil || !source.IsSubAgent {
		return "", false
	}
	switch source.SubAgent.Kind {
	case SubAgentReview:
		return "review", true
	case SubAgentCompact:
		return "compact", true
	case SubAgentMemoryConsolidation:
		return "memory_consolidation", true
	case SubAgentThreadSpawn:
		return "collab_spawn", true
	case SubAgentOther:
		return source.SubAgent.Label, true
	default:
		return "", false
	}
}

// insertHeader sets a header if both the name and value are valid. It mirrors the
// Rust `insert_header`.
func insertHeader(headers http.Header, name, value string) {
	if name == "" {
		return
	}
	headers.Set(name, value)
}

// attachItemIDs inserts the deserialize-only `id` for items that carry one into
// the serialized request body's `input` array. It mirrors the Rust
// `attach_item_ids`, used only for Azure store=true requests. The body is decoded
// into a generic map, mutated, and re-encoded; the original items are not
// mutated.
func attachItemIDs(payload []byte, items []protocol.ResponseItem) ([]byte, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(payload, &root); err != nil {
		return nil, err
	}
	inputRaw, ok := root["input"]
	if !ok {
		return payload, nil
	}
	var input []json.RawMessage
	if err := json.Unmarshal(inputRaw, &input); err != nil {
		return payload, nil
	}

	n := len(input)
	if len(items) < n {
		n = len(items)
	}
	for i := 0; i < n; i++ {
		id, ok := itemAttachableID(items[i])
		if !ok || id == "" {
			continue
		}
		var obj map[string]json.RawMessage
		if json.Unmarshal(input[i], &obj) != nil {
			continue
		}
		idBytes, err := json.Marshal(id)
		if err != nil {
			continue
		}
		obj["id"] = idBytes
		encoded, err := json.Marshal(obj)
		if err != nil {
			continue
		}
		input[i] = encoded
	}

	newInput, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	root["input"] = newInput
	return json.Marshal(root)
}

// itemAttachableID returns the id to attach for the item types listed in the Rust
// `attach_item_ids` match, or ("", false) when the item type does not carry an
// attachable id.
func itemAttachableID(item protocol.ResponseItem) (string, bool) {
	switch item.Type {
	case protocol.ResponseItemKindReasoning:
		return item.ReasoningID, true
	case protocol.ResponseItemKindMessage:
		return derefID(item.MessageID)
	case protocol.ResponseItemKindWebSearchCall:
		return derefID(item.WebSearchID)
	case protocol.ResponseItemKindFunctionCall:
		return derefID(item.FunctionCallID)
	case protocol.ResponseItemKindToolSearchCall:
		return derefID(item.ToolSearchCallID)
	case protocol.ResponseItemKindLocalShellCall:
		return derefID(item.LocalShellID)
	case protocol.ResponseItemKindCustomToolCall:
		return derefID(item.CustomToolCallID)
	default:
		return "", false
	}
}

func derefID(id *string) (string, bool) {
	if id == nil {
		return "", false
	}
	return *id, true
}
