// Package plugins ports the OpenAI Codex Rust crates
// `codex-rs/plugin` and `codex-rs/core-plugins` to Go as part of a faithful,
// drop-in-compatible reimplementation of Codex 0.136.0.
//
// It provides the foundational plugin subsystem:
//
//   - Stable plugin identifiers of the form <name>@<marketplace> and their
//     validation (see plugin_id.go).
//   - The on-disk plugin manifest schema and loader, supporting the canonical
//     ".codex-plugin/plugin.json" location with a ".claude-plugin/" fallback
//     (see manifest.go).
//   - The plugin cache store that installs/uninstalls/upgrades plugin bundles on
//     disk and resolves the active version (see store.go).
//   - Per-plugin enabled/disabled toggle collection (see toggles.go).
//   - Load-outcome aggregation that computes effective skill roots, MCP servers,
//     apps, hook sources and model-facing capability summaries (see
//     load_outcome.go).
//   - Marketplace manifest parsing, discovery and listing (see marketplace.go,
//     installed_marketplaces.go).
//   - tar.gz plugin bundle packing/unpacking with path-traversal and size
//     guards (see bundle_archive.go).
//
// All public values produced by this package are immutable: every operation
// returns a new value and never mutates its receiver or its arguments. On-disk,
// JSON and manifest formats match Codex byte-for-byte.
package plugins

// Marketplace names that Codex treats specially.
const (
	// OpenAICuratedMarketplaceName is the name of the curated marketplace.
	OpenAICuratedMarketplaceName = "openai-curated"
	// OpenAIBundledMarketplaceName is the name of the bundled marketplace.
	OpenAIBundledMarketplaceName = "openai-bundled"
)

// ToolSuggestDiscoverablePluginAllowlist mirrors the Rust
// `TOOL_SUGGEST_DISCOVERABLE_PLUGIN_ALLOWLIST`: the plugin keys that may be
// suggested to the model when discoverable. Callers receive a copy of the slice.
var ToolSuggestDiscoverablePluginAllowlist = []string{
	"github@openai-curated",
	"notion@openai-curated",
	"slack@openai-curated",
	"gmail@openai-curated",
	"google-calendar@openai-curated",
	"google-drive@openai-curated",
	"openai-developers@openai-curated",
	"canva@openai-curated",
	"teams@openai-curated",
	"sharepoint@openai-curated",
	"outlook-email@openai-curated",
	"outlook-calendar@openai-curated",
	"linear@openai-curated",
	"figma@openai-curated",
	"chrome@openai-bundled",
	"computer-use@openai-bundled",
}
