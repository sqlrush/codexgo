package cliutil

import (
	"sort"
	"strings"
)

// envRedaction is the placeholder shown in place of every environment value so
// secrets are never rendered.
const envRedaction = "*****"

// FormatEnvDisplay renders an environment map and an extra list of variable
// names as a single redacted display string, mirroring
// codex_utils_cli::format_env_display.
//
// The env map (pass nil for absent) contributes one "KEY=*****" entry per key,
// in ascending key order. The envVars slice contributes one "NAME=*****" entry
// per element, in the given order, appended after the map entries. Values are
// never shown.
//
// When there are no entries at all the function returns "-". Otherwise entries
// are joined with ", ".
//
// The inputs are treated as read-only: neither the map nor the slice is mutated,
// and sorting is performed on an internal copy of the keys.
func FormatEnvDisplay(env map[string]string, envVars []string) string {
	parts := make([]string, 0, len(env)+len(envVars))

	if env != nil {
		keys := make([]string, 0, len(env))
		for key := range env {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			parts = append(parts, key+"="+envRedaction)
		}
	}

	for _, v := range envVars {
		parts = append(parts, v+"="+envRedaction)
	}

	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ", ")
}
