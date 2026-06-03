package config

import (
	"sort"
	"strings"
)

// CliOverride is a single dotted-path override of the form key.path=value, as
// produced by `-c key=value` flags. Value is a decoded TOML value tree.
type CliOverride struct {
	Path  string
	Value TomlValue
}

// BuildCliOverridesLayer folds a list of dotted-path overrides into a single
// TOML table value, mirroring the Rust build_cli_overrides_layer. Later entries
// override earlier ones at the same path.
func BuildCliOverridesLayer(overrides []CliOverride) TomlValue {
	root := map[string]any{}
	for _, o := range overrides {
		applyOverride(root, o.Path, o.Value)
	}
	return root
}

// applyOverride writes value at the dotted path within root, creating
// intermediate tables and replacing non-table intermediates as needed. This
// matches the Rust apply_toml_override behavior.
func applyOverride(root map[string]any, path string, value TomlValue) {
	segments := strings.Split(path, ".")
	current := root
	for i, segment := range segments {
		isLast := i == len(segments)-1
		if isLast {
			current[segment] = value
			return
		}
		existing, ok := current[segment]
		if !ok {
			next := map[string]any{}
			current[segment] = next
			current = next
			continue
		}
		nextTable, isTable := existing.(map[string]any)
		if !isTable {
			nextTable = map[string]any{}
			current[segment] = nextTable
		}
		current = nextTable
	}
}

func sortedKeys(table map[string]any) []string {
	keys := make([]string, 0, len(table))
	for k := range table {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
