package core

import (
	"encoding/xml"
	"strings"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// This file holds the small projection helpers used by event_mapping.go that
// mirror functionality the Rust code reaches through sibling crates
// (web_search.rs, codex_protocol::items hook-prompt parsing, and
// UserMessageItem::message). They are kept here to keep event_mapping.go focused
// on the ResponseItem -> TurnItem mapping itself.

// webSearchActionDetail renders a human-readable detail string for a web-search
// action. Faithful port of the Rust `web_search_action_detail`.
func webSearchActionDetail(action protocol.WebSearchAction) string {
	switch action.Type {
	case protocol.WebSearchActionKindSearch:
		return searchActionDetail(action.Query, action.Queries)
	case protocol.WebSearchActionKindOpenPage:
		return derefString(action.URL)
	case protocol.WebSearchActionKindFindInPage:
		pattern := action.Pattern
		url := action.URL
		switch {
		case pattern != nil && url != nil:
			return "'" + *pattern + "' in " + *url
		case pattern != nil:
			return "'" + *pattern + "'"
		case url != nil:
			return *url
		default:
			return ""
		}
	default:
		return ""
	}
}

// searchActionDetail computes the detail for a Search action, preferring the
// single query and falling back to the first of the queries list (suffixing
// "..." when more than one query is present). Mirrors the Rust
// `search_action_detail`.
func searchActionDetail(query *string, queries *[]string) string {
	if query != nil && *query != "" {
		return *query
	}
	first := ""
	count := 0
	if queries != nil {
		count = len(*queries)
		if count > 0 {
			first = (*queries)[0]
		}
	}
	if count > 1 && first != "" {
		return first + " ..."
	}
	return first
}

// derefString returns the pointed-to string, or "" when nil.
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// hookPromptXML is the wire shape of a serialized hook-prompt fragment. Mirrors
// the Rust `HookPromptXml` used by `parse_hook_prompt_fragment`.
type hookPromptXML struct {
	XMLName   xml.Name `xml:"hook_prompt"`
	Text      string   `xml:"text"`
	HookRunID string   `xml:"hook_run_id"`
}

// parseHookPromptFragment attempts to parse text as a single serialized
// hook-prompt fragment, returning (fragment, true) on success. A fragment is
// only valid when it carries a non-empty hook_run_id. Faithful port of the Rust
// `parse_hook_prompt_fragment`.
func parseHookPromptFragment(text string) (protocol.HookPromptFragment, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return protocol.HookPromptFragment{}, false
	}
	var parsed hookPromptXML
	if err := xml.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return protocol.HookPromptFragment{}, false
	}
	if strings.TrimSpace(parsed.HookRunID) == "" {
		return protocol.HookPromptFragment{}, false
	}
	return protocol.HookPromptFragment{
		Text:      parsed.Text,
		HookRunID: parsed.HookRunID,
	}, true
}

// parseVisibleHookPromptMessage attempts to interpret a user-role message as a
// hook-prompt turn item. The message qualifies only when every content item is
// text and at least one item parses as a hook-prompt fragment (other text items
// must be recognized contextual scaffolding). Faithful port of the Rust
// `parse_visible_hook_prompt_message`.
func parseVisibleHookPromptMessage(id *string, content []protocol.ContentItem) (protocol.HookPromptItem, bool) {
	var fragments []protocol.HookPromptFragment
	for _, item := range content {
		if item.Type != protocol.ContentItemKindInputText {
			return protocol.HookPromptItem{}, false
		}
		if frag, ok := parseHookPromptFragment(item.Text); ok {
			fragments = append(fragments, frag)
			continue
		}
		if isStandardContextualUserText(item.Text) {
			continue
		}
		return protocol.HookPromptItem{}, false
	}
	if len(fragments) == 0 {
		return protocol.HookPromptItem{}, false
	}
	hookID := ""
	if id != nil {
		hookID = *id
	}
	if hookID == "" {
		hookID = newItemID()
	}
	return protocol.HookPromptItem{ID: hookID, Fragments: fragments}, true
}

// userMessageText concatenates the text inputs of a [protocol.UserMessageItem],
// mirroring the Rust `UserMessageItem::message`. Non-text inputs contribute the
// empty string (they are joined with no separator).
func userMessageText(item protocol.UserMessageItem) string {
	var b strings.Builder
	for _, in := range item.Content {
		if in.Type == protocol.UserInputKindText {
			b.WriteString(in.Text)
		}
	}
	return b.String()
}
