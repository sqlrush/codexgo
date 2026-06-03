// Package pluginutil ports the OpenAI Codex Rust crate
// `codex-rs/utils/plugins` to Go as part of a faithful, drop-in-compatible
// reimplementation of Codex 0.136.0.
//
// It provides plugin path/id helper utilities shared by plugin subsystems:
//
//   - [PluginSkillRoot]: a value describing a discovered skill root together with
//     the plugin it belongs to.
//   - Plugin manifest discovery and namespace resolution (see plugin_namespace.go).
//   - Plaintext mention sigils for tools and plugins (see mention_syntax.go).
//   - MCP connector id allow-listing and name sanitization (see mcp_connector.go).
//
// All values produced by this package are immutable: every operation returns a
// new value and never mutates its receiver or its arguments.
package pluginutil

import "github.com/sqlrush/codexgo/internal/utils/abspath"

// PluginSkillRoot mirrors the Rust struct `PluginSkillRoot`.
//
// It pairs a skill root directory (Path) with the owning plugin's identifier
// (PluginID) and the plugin's root directory (PluginRoot).
//
// The Rust type derives Clone/PartialEq/Eq/Hash. In Go the struct is comparable
// with == because all fields are comparable, and it is safe to copy because
// [abspath.AbsolutePathBuf] is an immutable value type.
type PluginSkillRoot struct {
	// Path is the directory that contains the skill(s).
	Path abspath.AbsolutePathBuf
	// PluginID is the owning plugin's identifier (its manifest name).
	PluginID string
	// PluginRoot is the owning plugin's root directory.
	PluginRoot abspath.AbsolutePathBuf
}
