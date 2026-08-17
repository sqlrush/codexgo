// Package websearch ports the codex ext/web-search crate: the standalone
// web-search tool's request shaping (results are produced server-side). The
// OpenAI search API request/response shapes are reproduced byte-for-byte from
// codex_api so the wire form matches codex exactly.
package websearch

import (
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/pkg/api"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// SearchInputKind discriminates a SearchInput. Rust: SearchInput (untagged).
type SearchInputKind int

// SearchInputKind variants.
const (
	// SearchInputKindText holds a plain text query.
	SearchInputKindText SearchInputKind = iota
	// SearchInputKindItems holds a conversation tail.
	SearchInputKindItems
)

// SearchInput is the conversation context for a search. Rust: SearchInput
// (#[serde(untagged)] over Text(String) and Items(Vec<ResponseItem>)).
type SearchInput struct {
	Kind  SearchInputKind
	Text  string
	Items []protocol.ResponseItem
}

// TextSearchInput constructs a Text SearchInput.
func TextSearchInput(text string) SearchInput {
	return SearchInput{Kind: SearchInputKindText, Text: text}
}

// ItemsSearchInput constructs an Items SearchInput.
func ItemsSearchInput(items []protocol.ResponseItem) SearchInput {
	return SearchInput{Kind: SearchInputKindItems, Items: items}
}

// MarshalJSON emits the untagged form: a bare string or an array of items.
func (s SearchInput) MarshalJSON() ([]byte, error) {
	switch s.Kind {
	case SearchInputKindText:
		return json.Marshal(s.Text)
	case SearchInputKindItems:
		return json.Marshal(s.Items)
	default:
		return nil, fmt.Errorf("websearch: unknown SearchInput kind %d", s.Kind)
	}
}

// SearchRequest is a standalone web search request. Rust: SearchRequest;
// reasoning, input, commands, settings, max_output_tokens use
// skip_serializing_if = Option::is_none.
type SearchRequest struct {
	ID              string
	Model           string
	Reasoning       *api.Reasoning
	Input           *SearchInput
	Commands        *SearchCommands
	Settings        *SearchSettings
	MaxOutputTokens *uint64
}

// MarshalJSON emits the request with the Rust field order and skip rules.
func (r SearchRequest) MarshalJSON() ([]byte, error) {
	m := newOrderedJSON()
	m.set("id", r.ID)
	m.set("model", r.Model)
	if r.Reasoning != nil {
		m.set("reasoning", r.Reasoning)
	}
	if r.Input != nil {
		m.set("input", r.Input)
	}
	if r.Commands != nil {
		m.set("commands", r.Commands)
	}
	if r.Settings != nil {
		m.set("settings", r.Settings)
	}
	if r.MaxOutputTokens != nil {
		m.set("max_output_tokens", *r.MaxOutputTokens)
	}
	return m.marshal()
}

// SearchResponse is the encrypted result envelope. Rust: SearchResponse.
type SearchResponse struct {
	EncryptedOutput string `json:"encrypted_output"`
}

// orderedJSON preserves insertion order for serializing struct-like JSON
// objects, matching serde's field-declaration order.
type orderedJSON struct {
	keys   []string
	values map[string]any
}

func newOrderedJSON() *orderedJSON {
	return &orderedJSON{values: make(map[string]any)}
}

func (o *orderedJSON) set(key string, value any) {
	o.keys = append(o.keys, key)
	o.values[key] = value
}

func (o *orderedJSON) marshal() ([]byte, error) {
	var buf []byte
	buf = append(buf, '{')
	for i, key := range o.keys {
		if i > 0 {
			buf = append(buf, ',')
		}
		keyJSON, err := json.Marshal(key)
		if err != nil {
			return nil, fmt.Errorf("websearch: marshal key %q: %w", key, err)
		}
		buf = append(buf, keyJSON...)
		buf = append(buf, ':')
		valJSON, err := json.Marshal(o.values[key])
		if err != nil {
			return nil, fmt.Errorf("websearch: marshal value for %q: %w", key, err)
		}
		buf = append(buf, valJSON...)
	}
	buf = append(buf, '}')
	return buf, nil
}
