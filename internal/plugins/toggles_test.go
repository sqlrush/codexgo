package plugins

import (
	"encoding/json"
	"reflect"
	"testing"
)

func raw(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

func TestCollectPluginEnabledCandidatesTracksDirectAndTableWrites(t *testing.T) {
	candidates := CollectPluginEnabledCandidates([]PluginEnabledEdit{
		{KeyPath: "plugins.sample@test.enabled", Value: raw(true)},
		{KeyPath: "plugins.other@test", Value: raw(map[string]any{"enabled": false, "ignored": true})},
		{KeyPath: "plugins", Value: raw(map[string]any{
			"nested@test": map[string]any{"enabled": true},
			"skip@test":   map[string]any{"name": "skip"},
		})},
	})

	want := map[string]bool{
		"nested@test": true,
		"other@test":  false,
		"sample@test": true,
	}
	if !reflect.DeepEqual(candidates, want) {
		t.Fatalf("got %v, want %v", candidates, want)
	}
}

func TestCollectPluginEnabledCandidatesUsesLastWriteForSamePlugin(t *testing.T) {
	candidates := CollectPluginEnabledCandidates([]PluginEnabledEdit{
		{KeyPath: "plugins.sample@test.enabled", Value: raw(true)},
		{KeyPath: "plugins.sample@test", Value: raw(map[string]any{"enabled": false})},
	})

	want := map[string]bool{"sample@test": false}
	if !reflect.DeepEqual(candidates, want) {
		t.Fatalf("got %v, want %v", candidates, want)
	}
}

func TestCollectPluginEnabledCandidatesIgnoresNonBoolean(t *testing.T) {
	candidates := CollectPluginEnabledCandidates([]PluginEnabledEdit{
		{KeyPath: "plugins.sample@test.enabled", Value: raw("yes")},
		{KeyPath: "plugins.sample@test.enabled", Value: raw(1)},
		{KeyPath: "other.key", Value: raw(true)},
		{KeyPath: "plugins.x@test", Value: raw([]int{1})},
	})
	if len(candidates) != 0 {
		t.Fatalf("expected no candidates, got %v", candidates)
	}
}
