package config

import (
	"strings"
	"testing"
)

func TestSetConfigValuePreservesComments(t *testing.T) {
	input := `# top comment
model = "old-model"  # inline comment
review_model = "keep"

[tui]
theme = "dark"
`
	out, err := SetConfigValue([]byte(input), "model", "new-model")
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, `model = "new-model"`) {
		t.Fatalf("value not updated: %q", got)
	}
	if !strings.Contains(got, "# top comment") {
		t.Fatalf("leading comment lost: %q", got)
	}
	if !strings.Contains(got, "# inline comment") {
		t.Fatalf("inline comment lost: %q", got)
	}
	if !strings.Contains(got, `review_model = "keep"`) {
		t.Fatalf("other key changed: %q", got)
	}
	if !strings.Contains(got, "[tui]") || !strings.Contains(got, `theme = "dark"`) {
		t.Fatalf("table section lost: %q", got)
	}
}

func TestSetConfigValueAppendsBeforeTable(t *testing.T) {
	input := `model = "m"

[tui]
theme = "dark"
`
	out, err := SetConfigValue([]byte(input), "review_model", "added")
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, `review_model = "added"`) {
		t.Fatalf("new key not added: %q", got)
	}
	// The new key must precede the [tui] header to stay in the root table.
	if strings.Index(got, "review_model") > strings.Index(got, "[tui]") {
		t.Fatalf("new key placed after table header: %q", got)
	}
}

func TestSetConfigValueAppendsToEmptyAndScalars(t *testing.T) {
	out, err := SetConfigValue([]byte(""), "model", "x")
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if !strings.Contains(string(out), `model = "x"`) {
		t.Fatalf("append to empty failed: %q", out)
	}

	out, err = SetConfigValue([]byte("count = 1\n"), "enabled", true)
	if err != nil {
		t.Fatalf("set bool: %v", err)
	}
	if !strings.Contains(string(out), "enabled = true") {
		t.Fatalf("bool not encoded: %q", out)
	}

	out, err = SetConfigValue([]byte(""), "roots", []any{"a", "b"})
	if err != nil {
		t.Fatalf("set array: %v", err)
	}
	if !strings.Contains(string(out), `roots = ['a', 'b']`) && !strings.Contains(string(out), `roots = ["a", "b"]`) {
		t.Fatalf("array not encoded: %q", out)
	}
}

func TestProjectRootMarkers(t *testing.T) {
	value, err := ParseTomlValue([]byte("project_root_markers = [\".git\", \".hg\"]\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	markers, err := ProjectRootMarkersFromConfig(value)
	if err != nil {
		t.Fatalf("markers: %v", err)
	}
	if len(markers) != 2 || markers[0] != ".git" || markers[1] != ".hg" {
		t.Fatalf("markers = %v", markers)
	}

	empty, err := ProjectRootMarkersFromConfig(map[string]any{"project_root_markers": []any{}})
	if err != nil {
		t.Fatalf("empty markers: %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty markers = %v", empty)
	}

	none, err := ProjectRootMarkersFromConfig(map[string]any{})
	if err != nil || none != nil {
		t.Fatalf("none markers = %v, err %v", none, err)
	}

	if _, err := ProjectRootMarkersFromConfig(map[string]any{"project_root_markers": "bad"}); err == nil {
		t.Fatalf("expected error for non-array markers")
	}

	if def := DefaultProjectRootMarkers(); len(def) != 1 || def[0] != ".git" {
		t.Fatalf("default markers = %v", def)
	}
}
