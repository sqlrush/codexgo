package tools

// ToolSearchEngine ports the Rust core `ToolSearchHandler`'s search machinery:
// it builds a BM25 corpus from a set of ToolSearchInfo entries, runs queries,
// and returns the coalesced LoadableToolSpecs of the top matches.

// ToolSearchEngine indexes deferred-tool entries for BM25 search. Mirrors the
// search state of the Rust `ToolSearchHandler` (its entries + search_engine).
type ToolSearchEngine struct {
	entries []ToolSearchEntry
	engine  *bm25SearchEngine
	// sources are the advertised search-source descriptors collected from the
	// infos (the Rust `search_source_infos`).
	sources []ToolSearchSourceInfo
}

// NewToolSearchEngine builds a search engine over the given infos. Mirrors Rust
// `ToolSearchHandler::new`: entries and source infos are split out, then a BM25
// engine is fit to each entry's search text (document id = entry index).
func NewToolSearchEngine(infos []ToolSearchInfo) *ToolSearchEngine {
	entries := make([]ToolSearchEntry, 0, len(infos))
	sources := make([]ToolSearchSourceInfo, 0, len(infos))
	for _, info := range infos {
		entries = append(entries, info.Entry)
		if info.SourceInfo != nil {
			sources = append(sources, *info.SourceInfo)
		}
	}
	documents := make([]bm25Document, len(entries))
	for i, entry := range entries {
		documents[i] = bm25Document{id: i, contents: entry.SearchText}
	}
	return &ToolSearchEngine{
		entries: entries,
		engine:  newBM25SearchEngine(documents),
		sources: sources,
	}
}

// Sources returns the advertised search-source descriptors. Mirrors the Rust
// handler's `search_source_infos` used by `spec`.
func (e *ToolSearchEngine) Sources() []ToolSearchSourceInfo {
	return e.sources
}

// Empty reports whether the engine has no indexed entries. Mirrors the
// `self.entries.is_empty()` fast path in the Rust handler.
func (e *ToolSearchEngine) Empty() bool {
	return len(e.entries) == 0
}

// Search returns the coalesced LoadableToolSpecs for the top `limit` BM25
// matches of the query. Mirrors Rust `ToolSearchHandler::search` ->
// `search_output_tools`: rank by BM25, map ids back to entries, clone their
// outputs, and coalesce same-name namespaces in hit order.
func (e *ToolSearchEngine) Search(query string, limit int) []LoadableToolSpec {
	results := e.engine.search(query, limit)
	specs := make([]LoadableToolSpec, 0, len(results))
	for _, result := range results {
		if result.id < 0 || result.id >= len(e.entries) {
			continue
		}
		specs = append(specs, cloneLoadableToolSpec(e.entries[result.id].Output))
	}
	return CoalesceLoadableToolSpecs(specs)
}

// cloneLoadableToolSpec returns a deep-ish copy of a loadable spec so the
// coalesce step (which appends into namespace tool slices) never mutates an
// entry's stored output. Mirrors the `entry.output.clone()` in the Rust handler.
func cloneLoadableToolSpec(spec LoadableToolSpec) LoadableToolSpec {
	switch spec.Kind {
	case LoadableToolSpecKindFunction:
		tool := *spec.Function
		return FunctionLoadableToolSpec(tool)
	case LoadableToolSpecKindNamespace:
		ns := *spec.Namespace
		tools := make([]ResponsesApiNamespaceTool, len(ns.Tools))
		copy(tools, ns.Tools)
		ns.Tools = tools
		return NamespaceLoadableToolSpec(ns)
	default:
		return spec
	}
}
