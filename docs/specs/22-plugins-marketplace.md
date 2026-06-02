# 22 — Plugins & Marketplace

| | |
|---|---|
| **Phase** | 6 — Extensibility |
| **Status** | Not started |
| **Depends on** | 04, 23 |
| **Size** | L |
| **Drop-in critical** | ★ (manifest format + on-disk layout) |

## 目标 / Goal
Port `codex-plugin` + `codex-core-plugins`: the plugin system (manifest, discovery,
install/uninstall/upgrade), the marketplace, and aggregation of plugin-provided
skills/MCP servers/apps/hooks.

## 源参考 / Source reference
- `reference-codex/codex-rs/plugin/src/` (`plugin_id`, `LoadedPlugin`, validation).
- `reference-codex/codex-rs/core-plugins/src/manifest.rs` (`RawPluginManifest`,
  `PluginManifestInterface`), install/upgrade, marketplace sync.

## 功能需求 / Functional requirements
1. Plugin on disk: `<root>/.codex-plugin/plugin.json` (fallback `.claude-plugin/`);
   plugin ID `<name>@<marketplace>` validation (alphanumeric/`-`/`_`).
2. Manifest schema: `name`, `version`, `description`, `keywords`, `skills` path,
   `mcpServers`, `apps`, `hooks` (path or inline), `interface{displayName,
   shortDescription, category, defaultPrompt, logo}`.
3. Discovery + capabilities aggregation: collect plugin skills (spec 23), MCP
   servers (spec 21), app connectors (spec 26), hooks (spec 24); per-plugin disabled
   lists.
4. Marketplace: add/list/upgrade/remove sources; fetch curated archive, extract,
   cache under `$XDG_DATA_HOME/codex/plugin-cache/<marketplace>/<plugin>/`;
   install/uninstall/upgrade.

## 验收方案 / Acceptance criteria
- A Codex-installed plugin (with manifest + skills + hooks) loads in `codexgo` and
  contributes the same capabilities.
- Manifest parsing (incl. legacy `.claude-plugin` path) matches Codex (golden).
- Install/upgrade/remove produce the same on-disk cache layout.
- Plugin ID validation accepts/rejects the same identifiers.

## 风险与难点 / Risks
- Marketplace endpoints/format may be remote; mock for tests, capture real responses.
- Path normalization across OSes must match Codex.

## 非目标 / Non-goals
- The behavior of individual contributed skills/hooks (specs 23/24).
