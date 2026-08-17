package config

import (
	"github.com/sqlrush/codexgo/internal/networkproxy"
)

// TomlValue is the in-memory representation of a parsed TOML document, mirroring
// Rust's toml::Value tree. Tables are map[string]any, arrays are []any, and
// scalars keep their decoded Go types (string, int64, float64, bool, time.Time).
type TomlValue = any

// configKeyAlias renames a legacy key to its canonical key within a specific
// table path during merge. Mirrors the Rust CONFIG_KEY_ALIASES table.
type configKeyAlias struct {
	tablePath    []string
	legacyKey    string
	canonicalKey string
}

var configKeyAliases = []configKeyAlias{
	{
		tablePath:    []string{"memories"},
		legacyKey:    "no_memories_if_mcp_or_web_search",
		canonicalKey: "disable_on_external_context",
	},
}

// MergeTomlValues merges overlay into base, giving overlay precedence. base is
// modified in place. Tables are merged recursively; non-table values from the
// overlay replace the base value entirely. Key aliases and network-domain host
// normalization are applied as part of the merge, matching the Rust
// merge_toml_values implementation.
func MergeTomlValues(base *TomlValue, overlay TomlValue) {
	mergeAtPath(base, overlay, nil)
}

func mergeAtPath(base *TomlValue, overlay TomlValue, path []string) {
	overlayTable, overlayIsTable := overlay.(map[string]any)
	baseTable, baseIsTable := (*base).(map[string]any)
	if overlayIsTable && baseIsTable {
		normalizeKeyAliases(path, baseTable)
		overlayCopy := cloneTable(overlayTable)
		normalizeKeyAliases(path, overlayCopy)
		if isPermissionNetworkDomainsPath(path) {
			normalizeNetworkDomainKeys(baseTable)
			normalizeNetworkDomainKeys(overlayCopy)
		}
		for _, key := range sortedKeys(overlayCopy) {
			value := overlayCopy[key]
			childPath := append(append([]string(nil), path...), key)
			if existing, ok := baseTable[key]; ok {
				child := existing
				mergeAtPath(&child, value, childPath)
				baseTable[key] = child
			} else {
				baseTable[key] = normalizedWithKeyAliases(value, childPath)
			}
		}
		return
	}
	*base = normalizedWithKeyAliases(overlay, path)
}

// normalizeKeyAliases renames legacy keys to canonical keys in table when the
// current path matches an alias entry. If the canonical key already exists, the
// legacy value is dropped (the canonical value wins).
func normalizeKeyAliases(path []string, table map[string]any) {
	for _, alias := range configKeyAliases {
		if !pathEquals(path, alias.tablePath) {
			continue
		}
		value, ok := table[alias.legacyKey]
		if !ok {
			continue
		}
		delete(table, alias.legacyKey)
		if _, exists := table[alias.canonicalKey]; !exists {
			table[alias.canonicalKey] = value
		}
	}
}

// normalizedWithKeyAliases returns a deep copy of value with key aliases applied
// recursively to every nested table.
func normalizedWithKeyAliases(value TomlValue, path []string) TomlValue {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for _, key := range sortedKeys(v) {
			childPath := append(append([]string(nil), path...), key)
			out[key] = normalizedWithKeyAliases(v[key], childPath)
		}
		normalizeKeyAliases(path, out)
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = normalizedWithKeyAliases(item, path)
		}
		return out
	default:
		return value
	}
}

func isPermissionNetworkDomainsPath(path []string) bool {
	return len(path) == 4 &&
		path[0] == "permissions" &&
		path[2] == "network" &&
		path[3] == "domains"
}

func normalizeNetworkDomainKeys(table map[string]any) {
	entries := make(map[string]any, len(table))
	for k, v := range table {
		entries[k] = v
		delete(table, k)
	}
	for pattern, value := range entries {
		table[networkproxy.NormalizeHost(pattern)] = value
	}
}

func cloneTable(table map[string]any) map[string]any {
	out := make(map[string]any, len(table))
	for k, v := range table {
		out[k] = cloneValue(v)
	}
	return out
}

func cloneValue(value TomlValue) TomlValue {
	switch v := value.(type) {
	case map[string]any:
		return cloneTable(v)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = cloneValue(item)
		}
		return out
	default:
		return value
	}
}

func pathEquals(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
