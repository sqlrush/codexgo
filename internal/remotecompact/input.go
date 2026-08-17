package remotecompact

import (
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/pkg/api"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// CompactionInput is the canonical request payload for the compaction endpoint.
// It mirrors the Rust `codex_api::CompactionInput` struct field-for-field,
// including the serde skip_serializing_if rules and the declaration order used
// for serialization.
//
// Reasoning and Text reuse the shared internal/api types so the on-wire shape is
// identical to a normal Responses request.
type CompactionInput struct {
	// Model is the target model slug.
	Model string
	// Input is the conversation transcript to compact.
	Input []protocol.ResponseItem
	// Instructions are the base instructions; omitted from the body when empty.
	Instructions string
	// Tools is the resolved tool specification, pre-encoded as JSON values.
	Tools []json.RawMessage
	// ParallelToolCalls reports whether the model may issue parallel tool calls.
	ParallelToolCalls bool
	// Reasoning, when non-nil, configures reasoning effort/summary.
	Reasoning *api.Reasoning
	// ServiceTier, when non-nil, selects a service tier.
	ServiceTier *string
	// PromptCacheKey, when non-nil, scopes the prompt cache.
	PromptCacheKey *string
	// Text, when non-nil, carries verbosity and output-schema controls.
	Text *api.TextControls
}

// MarshalJSON encodes the compaction input with the Rust serde rules that Go
// struct tags cannot express: `instructions` is omitted when empty, the optional
// pointer fields are omitted when nil, and the field order matches the Rust
// struct declaration order (model, input, instructions, tools,
// parallel_tool_calls, reasoning, service_tier, prompt_cache_key, text).
func (c CompactionInput) MarshalJSON() ([]byte, error) {
	m := make(map[string]json.RawMessage)
	order := make([]string, 0, 9)

	put := func(key string, value any) error {
		raw, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("encode compaction input field %q: %w", key, err)
		}
		m[key] = raw
		order = append(order, key)
		return nil
	}

	if err := put("model", c.Model); err != nil {
		return nil, err
	}
	if err := put("input", ensureItems(c.Input)); err != nil {
		return nil, err
	}
	// instructions: skip when empty (str::is_empty).
	if c.Instructions != "" {
		if err := put("instructions", c.Instructions); err != nil {
			return nil, err
		}
	}
	if err := put("tools", ensureRawSlice(c.Tools)); err != nil {
		return nil, err
	}
	if err := put("parallel_tool_calls", c.ParallelToolCalls); err != nil {
		return nil, err
	}
	if c.Reasoning != nil {
		if err := put("reasoning", c.Reasoning); err != nil {
			return nil, err
		}
	}
	if c.ServiceTier != nil {
		if err := put("service_tier", c.ServiceTier); err != nil {
			return nil, err
		}
	}
	if c.PromptCacheKey != nil {
		if err := put("prompt_cache_key", c.PromptCacheKey); err != nil {
			return nil, err
		}
	}
	if c.Text != nil {
		if err := put("text", c.Text); err != nil {
			return nil, err
		}
	}

	return encodeOrdered(m, order)
}

// CompactConversationRequestSettings carries the per-request knobs the caller
// supplies to a v1 compaction request. It mirrors the Rust
// `CompactConversationRequestSettings`.
type CompactConversationRequestSettings struct {
	// Effort, when non-nil, selects a reasoning effort level.
	Effort *protocol.ReasoningEffort
	// Summary selects a reasoning-summary verbosity.
	Summary protocol.ReasoningSummary
	// ServiceTier, when non-nil, selects a service tier (suppressed for API-key
	// auth by the caller, matching the reference).
	ServiceTier *string
}

// ensureItems returns a non-nil slice so an empty input serializes to `[]`
// rather than `null`, matching the Rust Vec serialization.
func ensureItems(items []protocol.ResponseItem) []protocol.ResponseItem {
	if items == nil {
		return []protocol.ResponseItem{}
	}
	return items
}

// ensureRawSlice returns a non-nil slice so an empty tools list serializes to
// `[]` rather than `null`.
func ensureRawSlice(items []json.RawMessage) []json.RawMessage {
	if items == nil {
		return []json.RawMessage{}
	}
	return items
}

// encodeOrdered serializes the keys of m in the given order into a JSON object,
// preserving insertion order so the output is deterministic and matches the Rust
// serde field order.
func encodeOrdered(m map[string]json.RawMessage, order []string) ([]byte, error) {
	var buf []byte
	buf = append(buf, '{')
	for i, key := range order {
		if i > 0 {
			buf = append(buf, ',')
		}
		keyJSON, err := json.Marshal(key)
		if err != nil {
			return nil, fmt.Errorf("encode key %q: %w", key, err)
		}
		buf = append(buf, keyJSON...)
		buf = append(buf, ':')
		buf = append(buf, m[key]...)
	}
	buf = append(buf, '}')
	return buf, nil
}
