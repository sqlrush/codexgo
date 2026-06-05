package tools

// tool_search spec builder, faithfully porting the Rust core
// `handlers/tool_search_spec.rs` create_tool_search_tool: the description
// renders the deduplicated, name-sorted searchable sources, and the parameters
// take a required query plus an optional result limit.

import (
	"fmt"
	"sort"
	"strings"
)

// CreateToolSearchTool builds the tool_search ToolSpec advertising the given
// searchable sources. Mirrors the Rust `create_tool_search_tool`: sources are
// deduplicated by name (first non-nil description wins) and rendered sorted by
// name (the Rust BTreeMap ordering).
func CreateToolSearchTool(searchableSources []ToolSearchSourceInfo, defaultLimit int) ToolSpec {
	queryDesc := "Search query for deferred tools."
	limitDesc := fmt.Sprintf("Maximum number of tools to return. Defaults to %d.", defaultLimit)
	properties := map[string]JsonSchema{
		"query": StringSchema(&queryDesc),
		"limit": NumberSchema(&limitDesc),
	}

	descriptions := make(map[string]*string, len(searchableSources))
	names := make([]string, 0, len(searchableSources))
	for _, source := range searchableSources {
		existing, seen := descriptions[source.Name]
		if !seen {
			names = append(names, source.Name)
			descriptions[source.Name] = source.Description
			continue
		}
		if existing == nil {
			descriptions[source.Name] = source.Description
		}
	}
	sort.Strings(names)

	sourceDescriptions := "None currently enabled."
	if len(names) > 0 {
		lines := make([]string, 0, len(names))
		for _, name := range names {
			if description := descriptions[name]; description != nil {
				lines = append(lines, fmt.Sprintf("- %s: %s", name, *description))
			} else {
				lines = append(lines, fmt.Sprintf("- %s", name))
			}
		}
		sourceDescriptions = strings.Join(lines, "\n")
	}

	description := fmt.Sprintf(
		"# Tool discovery\n\nSearches over deferred tool metadata with BM25 and exposes matching tools for the next model call.\n\nYou have access to tools from the following sources:\n%s\nSome of the tools may not have been provided to you upfront, and you should use this tool (`%s`) to search for the required tools. For MCP tool discovery, always use `%s` instead of `list_mcp_resources` or `list_mcp_resource_templates`.",
		sourceDescriptions, ToolSearchToolName, ToolSearchToolName,
	)

	return ToolSearchToolSpec(ToolSearchSpec{
		Execution:   "client",
		Description: description,
		Parameters: ObjectSchema(
			properties,
			[]string{"query"},
			BoolAdditionalProperties(false),
		),
	})
}
