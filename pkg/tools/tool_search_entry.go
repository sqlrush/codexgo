package tools

// tool_search entry/info types, porting the Rust core `tool_search_entry.rs`.
// A ToolSearchInfo carries the BM25 search text plus the LoadableToolSpec that a
// matching search returns, together with the optional source descriptor that
// feeds the tool_search tool's advertised source list.

// ToolSearchEntry is one searchable deferred-tool entry: its BM25 search text and
// the LoadableToolSpec emitted when it matches. Mirrors Rust `ToolSearchEntry`.
type ToolSearchEntry struct {
	// SearchText is the BM25 document indexed for this entry.
	SearchText string
	// Output is the loadable spec returned to the model when the entry matches.
	Output LoadableToolSpec
}

// ToolSearchInfo bundles a ToolSearchEntry with the optional search-source
// descriptor advertised by the tool_search tool. Mirrors Rust `ToolSearchInfo`.
type ToolSearchInfo struct {
	// Entry is the searchable entry.
	Entry ToolSearchEntry
	// SourceInfo, when non-nil, contributes a row to the tool_search advertised
	// source list.
	SourceInfo *ToolSearchSourceInfo
}

// ToolSearchInfoFromSpec converts a ToolSpec into a ToolSearchInfo, returning
// false for spec kinds that are not searchable. Mirrors Rust
// `ToolSearchInfo::from_spec`: function/namespace specs become deferred loadable
// specs (defer_loading=true, output_schema stripped, default namespace
// description filled in when blank); tool_search/image_generation/web_search/
// custom specs are not searchable.
func ToolSearchInfoFromSpec(searchText string, spec ToolSpec, sourceInfo *ToolSearchSourceInfo) (ToolSearchInfo, bool) {
	var output LoadableToolSpec
	switch spec.Kind {
	case ToolSpecKindFunction:
		tool := *spec.Function
		tool.DeferLoading = boolPtr(true)
		tool.OutputSchema = nil
		output = FunctionLoadableToolSpec(tool)
	case ToolSpecKindNamespace:
		namespace := cloneNamespaceForSearch(*spec.Namespace)
		output = NamespaceLoadableToolSpec(namespace)
	default:
		return ToolSearchInfo{}, false
	}
	return ToolSearchInfo{
		Entry:      ToolSearchEntry{SearchText: searchText, Output: output},
		SourceInfo: sourceInfo,
	}, true
}

// cloneNamespaceForSearch returns a deferred copy of a namespace: a blank
// description is replaced with the default, and every function tool gets
// defer_loading=true with output_schema stripped. The input is not mutated
// (immutability). Mirrors the namespace arm of Rust `ToolSearchInfo::from_spec`.
func cloneNamespaceForSearch(namespace ResponsesApiNamespace) ResponsesApiNamespace {
	description := namespace.Description
	if isBlank(description) {
		description = DefaultNamespaceDescription(namespace.Name)
	}
	tools := make([]ResponsesApiNamespaceTool, len(namespace.Tools))
	for i, child := range namespace.Tools {
		tool := child.Function
		tool.DeferLoading = boolPtr(true)
		tool.OutputSchema = nil
		tools[i] = FunctionNamespaceTool(tool)
	}
	return ResponsesApiNamespace{
		Name:        namespace.Name,
		Description: description,
		Tools:       tools,
	}
}

// isBlank reports whether s is empty or all whitespace, mirroring Rust's
// `str::trim().is_empty()`.
func isBlank(s string) bool {
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r', '\v', '\f':
			continue
		default:
			return false
		}
	}
	return true
}

func boolPtr(b bool) *bool { return &b }
