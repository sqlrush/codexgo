package tools

import (
	"github.com/sqlrush/codexgo/pkg/modelsmanager"
	"github.com/sqlrush/codexgo/pkg/protocol"
)

// This file ports the hosted (provider-executed) tool specs from codex-core's
// tools/hosted_spec.rs. Hosted tools have no local handler: the model provider
// executes them server-side, so only the advertised spec matters.

// webSearchTextAndImageContentTypes mirrors WEB_SEARCH_TEXT_AND_IMAGE_CONTENT_TYPES.
var webSearchTextAndImageContentTypes = []string{"text", "image"}

// WebSearchToolOptions mirrors the Rust `WebSearchToolOptions`. A nil
// WebSearchMode means the provider does not support web search (the tool is
// omitted entirely); Disabled likewise omits it.
//
// The optional web_search_config (filters / user_location / search_context_size)
// is config-owned and not yet carried by codexgo's session configuration, so
// those spec fields stay absent — matching codex's default (no [tools.web_search]
// table) output.
type WebSearchToolOptions struct {
	// WebSearchMode is the effective mode for the turn (nil/disabled → no tool).
	WebSearchMode *protocol.WebSearchMode
	// WebSearchToolType selects the text vs text+image capability (model info).
	WebSearchToolType modelsmanager.WebSearchToolType
}

// CreateWebSearchTool builds the hosted `web_search` ToolSpec, or ok=false when
// web search is unavailable for the turn. Mirrors Rust `create_web_search_tool`:
// Cached → external_web_access:false, Live → true, Disabled/None → no tool;
// text_and_image models advertise search_content_types ["text","image"].
func CreateWebSearchTool(opts WebSearchToolOptions) (ToolSpec, bool) {
	if opts.WebSearchMode == nil {
		return ToolSpec{}, false
	}
	var externalWebAccess bool
	switch *opts.WebSearchMode {
	case protocol.WebSearchModeCached:
		externalWebAccess = false
	case protocol.WebSearchModeLive:
		externalWebAccess = true
	default: // Disabled (or unknown)
		return ToolSpec{}, false
	}

	var searchContentTypes []string
	if opts.WebSearchToolType == modelsmanager.WebSearchToolTypeTextAndImage {
		searchContentTypes = append([]string(nil), webSearchTextAndImageContentTypes...)
	}

	return WebSearchToolSpec(WebSearchSpec{
		ExternalWebAccess:  &externalWebAccess,
		SearchContentTypes: searchContentTypes,
	}), true
}
