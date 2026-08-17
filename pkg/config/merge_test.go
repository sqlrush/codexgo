package config

import (
	"reflect"
	"testing"
)

func TestMergeTomlValues(t *testing.T) {
	tests := []struct {
		name    string
		base    map[string]any
		overlay map[string]any
		want    map[string]any
	}{
		{
			name:    "overlay scalar overrides base",
			base:    map[string]any{"model": "a", "kept": int64(1)},
			overlay: map[string]any{"model": "b"},
			want:    map[string]any{"model": "b", "kept": int64(1)},
		},
		{
			name:    "nested tables merge recursively",
			base:    map[string]any{"t": map[string]any{"x": int64(1), "y": int64(2)}},
			overlay: map[string]any{"t": map[string]any{"y": int64(3), "z": int64(4)}},
			want:    map[string]any{"t": map[string]any{"x": int64(1), "y": int64(3), "z": int64(4)}},
		},
		{
			name:    "non-table overlay replaces table",
			base:    map[string]any{"t": map[string]any{"x": int64(1)}},
			overlay: map[string]any{"t": "scalar"},
			want:    map[string]any{"t": "scalar"},
		},
		{
			name: "memories legacy alias normalized",
			base: map[string]any{},
			overlay: map[string]any{
				"memories": map[string]any{"no_memories_if_mcp_or_web_search": true},
			},
			want: map[string]any{
				"memories": map[string]any{"disable_on_external_context": true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := TomlValue(tt.base)
			MergeTomlValues(&base, tt.overlay)
			if !reflect.DeepEqual(base, TomlValue(tt.want)) {
				t.Fatalf("got %#v, want %#v", base, tt.want)
			}
		})
	}
}

func TestMergeDoesNotMutateOverlay(t *testing.T) {
	overlay := map[string]any{"t": map[string]any{"x": int64(1)}}
	base := TomlValue(map[string]any{"t": map[string]any{"y": int64(2)}})
	MergeTomlValues(&base, overlay)
	inner := overlay["t"].(map[string]any)
	if _, leaked := inner["y"]; leaked {
		t.Fatalf("overlay was mutated: %#v", overlay)
	}
}

func TestBuildCliOverridesLayer(t *testing.T) {
	overrides := []CliOverride{
		{Path: "model", Value: "gpt-5"},
		{Path: "tui.animations", Value: false},
		{Path: "tui.theme", Value: "dark"},
	}
	got := BuildCliOverridesLayer(overrides).(map[string]any)
	if got["model"] != "gpt-5" {
		t.Fatalf("model = %v", got["model"])
	}
	tui := got["tui"].(map[string]any)
	if tui["animations"] != false || tui["theme"] != "dark" {
		t.Fatalf("tui = %#v", tui)
	}
}

func TestApplyOverrideReplacesScalarIntermediate(t *testing.T) {
	root := map[string]any{"a": "scalar"}
	applyOverride(root, "a.b.c", int64(5))
	a := root["a"].(map[string]any)
	b := a["b"].(map[string]any)
	if b["c"] != int64(5) {
		t.Fatalf("c = %v", b["c"])
	}
}
