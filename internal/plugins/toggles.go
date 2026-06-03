package plugins

// Per-plugin enabled/disabled toggle collection from config edits. Ports
// `codex-rs/core-plugins/src/toggles.rs`.

import (
	"encoding/json"
	"sort"
	"strings"
)

// PluginEnabledEdit is a single dotted-key config edit, mirroring the
// `(&String, &JsonValue)` pairs the Rust function iterates over. Value holds the
// raw JSON value at KeyPath.
type PluginEnabledEdit struct {
	KeyPath string
	Value   json.RawMessage
}

// CollectPluginEnabledCandidates mirrors the Rust
// `collect_plugin_enabled_candidates`.
//
// It inspects a sequence of dotted config edits and extracts the resulting
// enabled state per plugin id from three shapes:
//
//	plugins.<id>.enabled = <bool>
//	plugins.<id>          = { enabled: <bool>, ... }
//	plugins              = { "<id>": { enabled: <bool> }, ... }
//
// Later edits win for the same plugin id. The result is keyed by plugin id; the
// returned map is freshly allocated and owned by the caller. The input edits are
// not modified.
func CollectPluginEnabledCandidates(edits []PluginEnabledEdit) map[string]bool {
	pending := make(map[string]bool)
	for _, edit := range edits {
		segments := strings.Split(edit.KeyPath, ".")
		switch len(segments) {
		case 3:
			plugins, pluginID, enabled := segments[0], segments[1], segments[2]
			if plugins != "plugins" || enabled != "enabled" {
				continue
			}
			if b, ok := jsonAsBool(edit.Value); ok {
				pending[pluginID] = b
			}
		case 2:
			plugins, pluginID := segments[0], segments[1]
			if plugins != "plugins" {
				continue
			}
			if b, ok := objectEnabledBool(edit.Value); ok {
				pending[pluginID] = b
			}
		case 1:
			if segments[0] != "plugins" {
				continue
			}
			collectPluginsTable(edit.Value, pending)
		}
	}
	return pending
}

// collectPluginsTable handles the `plugins = { ... }` form, extracting an
// enabled bool from each nested object entry.
func collectPluginsTable(value json.RawMessage, pending map[string]bool) {
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(value, &entries); err != nil {
		return
	}
	// Deterministic ordering matches the Rust BTreeMap insertion semantics:
	// because each plugin id appears at most once in an object, ordering does
	// not change the result, but we sort for stable iteration regardless.
	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, pluginID := range keys {
		if b, ok := objectEnabledBool(entries[pluginID]); ok {
			pending[pluginID] = b
		}
	}
}

// jsonAsBool returns the bool value when raw is a JSON boolean.
func jsonAsBool(raw json.RawMessage) (bool, bool) {
	var b bool
	if err := json.Unmarshal(raw, &b); err != nil {
		return false, false
	}
	// Reject non-boolean values that happen to unmarshal (none do for bool), and
	// guard against numbers/strings by checking the literal token.
	trimmed := strings.TrimSpace(string(raw))
	if trimmed != "true" && trimmed != "false" {
		return false, false
	}
	return b, true
}

// objectEnabledBool extracts the "enabled" boolean from a JSON object, returning
// ok=false when absent or not a boolean.
func objectEnabledBool(raw json.RawMessage) (bool, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return false, false
	}
	enabled, ok := obj["enabled"]
	if !ok {
		return false, false
	}
	return jsonAsBool(enabled)
}
