package modelsmanager

import "sort"

// This file ports `test_support.rs`: test-only helpers exposed for dependent
// packages. Production code should not depend on these helpers.

// GetModelOfflineForTests returns the model identifier without consulting remote
// state or cache. Rust: get_model_offline_for_tests.
func GetModelOfflineForTests(model *string) string {
	if model != nil {
		return *model
	}
	response, err := BundledModelsResponse()
	if err != nil {
		return ""
	}
	models := response.Models
	sort.SliceStable(models, func(i, j int) bool {
		return models[i].Priority < models[j].Priority
	})
	presets := make([]ModelPreset, 0, len(models))
	for _, info := range models {
		presets = append(presets, modelPresetFromInfo(info))
	}
	for _, preset := range presets {
		if preset.ShowInPicker {
			return preset.Model
		}
	}
	if len(presets) > 0 {
		return presets[0].Model
	}
	return ""
}

// ConstructModelInfoOfflineForTests builds a ModelInfo without consulting remote
// state or cache. Rust: construct_model_info_offline_for_tests.
func ConstructModelInfoOfflineForTests(model string, config *ModelsManagerConfig) ModelInfo {
	var candidates []ModelInfo
	if config.ModelCatalog != nil {
		candidates = config.ModelCatalog.Models
	}
	return constructModelInfoFromCandidates(model, candidates, config)
}
