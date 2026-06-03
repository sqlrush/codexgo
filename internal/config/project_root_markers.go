package config

import "fmt"

var defaultProjectRootMarkersValue = []string{".git"}

// ProjectRootMarkersFromConfig reads project_root_markers from a merged TOML
// value tree.
//
// Returns (nil, nil) when unset. Returns ([], nil) for an empty array (root
// detection disabled). Returns an error when present but not an array of
// strings. Mirrors the Rust project_root_markers_from_config.
func ProjectRootMarkersFromConfig(config TomlValue) ([]string, error) {
	table, ok := config.(map[string]any)
	if !ok {
		return nil, nil
	}
	value, ok := table["project_root_markers"]
	if !ok {
		return nil, nil
	}
	entries, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("project_root_markers must be an array of strings")
	}
	if len(entries) == 0 {
		return []string{}, nil
	}
	markers := make([]string, 0, len(entries))
	for _, entry := range entries {
		s, ok := entry.(string)
		if !ok {
			return nil, fmt.Errorf("project_root_markers must be an array of strings")
		}
		markers = append(markers, s)
	}
	return markers, nil
}

// DefaultProjectRootMarkers returns the default markers ([".git"]).
func DefaultProjectRootMarkers() []string {
	out := make([]string, len(defaultProjectRootMarkersValue))
	copy(out, defaultProjectRootMarkersValue)
	return out
}
