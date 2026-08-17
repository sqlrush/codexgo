package modelsmanager

import (
	"reflect"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

func TestModelPresetFromInfo(t *testing.T) {
	info := remoteModel(t, "gpt-x", "GPT X", 7)
	preset := modelPresetFromInfo(info)

	if preset.ID != "gpt-x" || preset.Model != "gpt-x" {
		t.Fatalf("id/model mismatch: %q / %q", preset.ID, preset.Model)
	}
	if preset.DisplayName != "GPT X" {
		t.Fatalf("display name = %q", preset.DisplayName)
	}
	if preset.Description != "GPT X desc" {
		t.Fatalf("description = %q", preset.Description)
	}
	if preset.DefaultReasoningEffort != protocol.ReasoningEffortMedium {
		t.Fatalf("default reasoning effort = %q", preset.DefaultReasoningEffort)
	}
	if !preset.ShowInPicker {
		t.Fatalf("list-visible model should show in picker")
	}
	if preset.IsDefault {
		t.Fatalf("freshly converted preset should not be default")
	}
}

func TestModelPresetFromInfoNoDefaultReasoning(t *testing.T) {
	info := ModelInfoFromSlug("fallback")
	preset := modelPresetFromInfo(info)
	if preset.DefaultReasoningEffort != protocol.ReasoningEffortNone {
		t.Fatalf("missing default reasoning level should map to None, got %q", preset.DefaultReasoningEffort)
	}
	if preset.ShowInPicker {
		t.Fatalf("visibility=none model should not show in picker")
	}
}

func TestFilterByAuth(t *testing.T) {
	apiModel := modelPresetFromInfo(remoteModel(t, "api", "API", 0))
	chatgptOnly := remoteModel(t, "chatgpt", "ChatGPT", 1)
	chatgptOnly.SupportedInAPI = false
	chatgptPreset := modelPresetFromInfo(chatgptOnly)

	models := []ModelPreset{apiModel, chatgptPreset}

	chatgptFiltered := FilterByAuth(models, true)
	if len(chatgptFiltered) != 2 {
		t.Fatalf("chatgpt mode should keep all, got %d", len(chatgptFiltered))
	}

	apiFiltered := FilterByAuth(models, false)
	if len(apiFiltered) != 1 || apiFiltered[0].Model != "api" {
		t.Fatalf("api mode should keep only api-supported models, got %v", presetModels(apiFiltered))
	}
	// Original slice unchanged.
	if len(models) != 2 {
		t.Fatalf("input slice was mutated")
	}
}

func TestMarkDefaultByPickerVisibility(t *testing.T) {
	hidden := modelPresetFromInfo(remoteModelWithVisibility(t, "hidden", "Hidden", 0, "hide"))
	visible := modelPresetFromInfo(remoteModelWithVisibility(t, "visible", "Visible", 1, "list"))

	presets := []ModelPreset{hidden, visible}
	MarkDefaultByPickerVisibility(presets)
	if presets[0].IsDefault {
		t.Fatalf("hidden preset should not be default")
	}
	if !presets[1].IsDefault {
		t.Fatalf("first picker-visible preset should be default")
	}

	// All hidden -> first wins.
	allHidden := []ModelPreset{hidden, modelPresetFromInfo(remoteModelWithVisibility(t, "hidden2", "Hidden2", 2, "hide"))}
	MarkDefaultByPickerVisibility(allHidden)
	if !allHidden[0].IsDefault {
		t.Fatalf("first preset should be default when none are picker-visible")
	}
}

func TestReasoningEffortMappingFromPresets(t *testing.T) {
	// Empty presets -> nil mapping.
	if reasoningEffortMappingFromPresets(nil) != nil {
		t.Fatalf("empty presets should produce nil mapping")
	}

	presets := []ReasoningEffortPreset{
		{Effort: protocol.ReasoningEffortLow},
		{Effort: protocol.ReasoningEffortHigh},
	}
	mapping := reasoningEffortMappingFromPresets(presets)
	want := map[protocol.ReasoningEffort]protocol.ReasoningEffort{
		protocol.ReasoningEffortNone:    protocol.ReasoningEffortLow,
		protocol.ReasoningEffortMinimal: protocol.ReasoningEffortLow,
		protocol.ReasoningEffortLow:     protocol.ReasoningEffortLow,
		protocol.ReasoningEffortMedium:  protocol.ReasoningEffortLow, // tie at distance 1; first supported (Low) wins
		protocol.ReasoningEffortHigh:    protocol.ReasoningEffortHigh,
		protocol.ReasoningEffortXHigh:   protocol.ReasoningEffortHigh,
	}
	if !reflect.DeepEqual(mapping, want) {
		t.Fatalf("mapping mismatch:\n got %v\nwant %v", mapping, want)
	}
}

func TestSupportsFastMode(t *testing.T) {
	preset := modelPresetFromInfo(remoteModel(t, "m", "M", 0))
	if preset.SupportsFastMode() {
		t.Fatalf("plain preset should not support fast mode")
	}
	preset.AdditionalSpeedTiers = []string{SpeedTierFast}
	if !preset.SupportsFastMode() {
		t.Fatalf("legacy fast speed tier should enable fast mode")
	}
	preset2 := modelPresetFromInfo(remoteModel(t, "m2", "M2", 0))
	preset2.ServiceTiers = []ModelServiceTier{{ID: serviceTierFastRequestValue}}
	if !preset2.SupportsFastMode() {
		t.Fatalf("priority service tier should enable fast mode")
	}
}
