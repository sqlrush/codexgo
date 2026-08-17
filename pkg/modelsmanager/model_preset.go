package modelsmanager

import (
	"github.com/sqlrush/codexgo/internal/protocol"
)

// ModelPreset is the picker-ready model metadata derived from a ModelInfo.
// Rust: openai_models::ModelPreset.
//
// Field order matches the Rust struct declaration so the canonical JSON output
// (key order excepted) is byte-for-byte compatible.
type ModelPreset struct {
	// ID is the stable identifier for the preset.
	ID string `json:"id"`
	// Model is the model slug (e.g., "gpt-5").
	Model string `json:"model"`
	// DisplayName is the display name shown in UIs.
	DisplayName string `json:"display_name"`
	// Description is the short human description shown in UIs.
	Description string `json:"description"`
	// DefaultReasoningEffort is the reasoning effort applied when none is chosen.
	DefaultReasoningEffort protocol.ReasoningEffort `json:"default_reasoning_effort"`
	// SupportedReasoningEfforts lists the supported reasoning effort options.
	SupportedReasoningEfforts []ReasoningEffortPreset `json:"supported_reasoning_efforts"`
	// SupportsPersonality reports whether the model supports personality
	// instructions. serde default.
	SupportsPersonality bool `json:"supports_personality"`
	// AdditionalSpeedTiers is deprecated; use ServiceTiers instead. serde default.
	AdditionalSpeedTiers []string `json:"additional_speed_tiers"`
	// ServiceTiers lists the service tiers this model can run with. serde default.
	ServiceTiers []ModelServiceTier `json:"service_tiers"`
	// DefaultServiceTier is the catalog default service tier id, if any.
	// serde default + skip_serializing_if = Option::is_none.
	DefaultServiceTier *string `json:"default_service_tier,omitempty"`
	// IsDefault reports whether this is the default model for new users.
	IsDefault bool `json:"is_default"`
	// Upgrade is the recommended upgrade model, if any.
	Upgrade *ModelUpgrade `json:"upgrade"`
	// ShowInPicker reports whether this preset should appear in the picker UI.
	ShowInPicker bool `json:"show_in_picker"`
	// AvailabilityNux is the availability NUX shown when this preset becomes
	// accessible to the user.
	AvailabilityNux *ModelAvailabilityNux `json:"availability_nux"`
	// SupportedInAPI reports whether this model is supported in the api.
	SupportedInAPI bool `json:"supported_in_api"`
	// InputModalities lists the input modalities accepted when composing user
	// turns for this preset. serde default = default_input_modalities.
	InputModalities []protocol.InputModality `json:"input_modalities"`
}

// modelPresetFromInfo converts a ModelInfo into a ModelPreset, mirroring Rust's
// `impl From<ModelInfo> for ModelPreset`. The original ModelInfo is not mutated.
func modelPresetFromInfo(info ModelInfo) ModelPreset {
	defaultEffort := protocol.ReasoningEffortNone
	if info.DefaultReasoningLevel != nil {
		defaultEffort = *info.DefaultReasoningLevel
	}

	var upgrade *ModelUpgrade
	if info.Upgrade != nil {
		migration := info.Upgrade.MigrationMarkdown
		upgrade = &ModelUpgrade{
			ID:                     info.Upgrade.Model,
			ReasoningEffortMapping: reasoningEffortMappingFromPresets(info.SupportedReasoningLevels),
			MigrationConfigKey:     info.Slug,
			// todo(aibrahim): add the model link here.
			ModelLink:         nil,
			UpgradeCopy:       nil,
			MigrationMarkdown: &migration,
		}
	}

	return ModelPreset{
		ID:                        info.Slug,
		Model:                     info.Slug,
		DisplayName:               info.DisplayName,
		Description:               derefStringOr(info.Description, ""),
		DefaultReasoningEffort:    defaultEffort,
		SupportedReasoningEfforts: nonNilPresets(info.SupportedReasoningLevels),
		SupportsPersonality:       info.SupportsPersonality(),
		AdditionalSpeedTiers:      nonNilStrings(info.AdditionalSpeedTiers),
		ServiceTiers:              nonNilTiers(info.ServiceTiers),
		DefaultServiceTier:        cloneStringPtr(info.DefaultServiceTier),
		IsDefault:                 false, // default is the highest priority available model
		Upgrade:                   upgrade,
		ShowInPicker:              info.Visibility == ModelVisibilityList,
		AvailabilityNux:           info.AvailabilityNux,
		SupportedInAPI:            info.SupportedInAPI,
		InputModalities:           nonNilModalities(info.InputModalities),
	}
}

// BuildAvailableModelsForCatalog builds picker-ready presets from a catalog
// snapshot with no auth manager (the bare-run derivation: API-supported models
// only, since no ChatGPT backend is authenticated). Mirrors the Rust
// `ModelsManager::build_available_models` call path with an unauthenticated
// session. The input slice is not mutated.
func BuildAvailableModelsForCatalog(remoteModels []ModelInfo) []ModelPreset {
	return buildAvailableModels(nil, remoteModels)
}

// SupportsFastMode reports whether the preset advertises the "fast"/"priority"
// service tier (either via ServiceTiers or the legacy AdditionalSpeedTiers).
func (p ModelPreset) SupportsFastMode() bool {
	for _, tier := range p.ServiceTiers {
		if tier.ID == serviceTierFastRequestValue {
			return true
		}
	}
	for _, tier := range p.AdditionalSpeedTiers {
		if tier == SpeedTierFast {
			return true
		}
	}
	return false
}

// FilterByAuth filters presets based on authentication mode. In ChatGPT mode all
// models are visible; otherwise only API-supported models are shown. A new slice
// is returned; the input is not mutated.
func FilterByAuth(models []ModelPreset, chatgptMode bool) []ModelPreset {
	filtered := make([]ModelPreset, 0, len(models))
	for _, model := range models {
		if chatgptMode || model.SupportedInAPI {
			filtered = append(filtered, model)
		}
	}
	return filtered
}

// MarkDefaultByPickerVisibility recomputes the single default preset using picker
// visibility. The first picker-visible model wins; if none are picker-visible,
// the first model wins. The slice contents are mutated in place (matching Rust's
// `&mut [ModelPreset]` signature).
func MarkDefaultByPickerVisibility(models []ModelPreset) {
	for i := range models {
		models[i].IsDefault = false
	}
	for i := range models {
		if models[i].ShowInPicker {
			models[i].IsDefault = true
			return
		}
	}
	if len(models) > 0 {
		models[0].IsDefault = true
	}
}

// reasoningEffortMappingFromPresets maps every canonical reasoning effort to the
// closest supported effort for the new model. Returns nil when presets is empty.
func reasoningEffortMappingFromPresets(presets []ReasoningEffortPreset) map[protocol.ReasoningEffort]protocol.ReasoningEffort {
	if len(presets) == 0 {
		return nil
	}
	supported := make([]protocol.ReasoningEffort, 0, len(presets))
	for _, p := range presets {
		supported = append(supported, p.Effort)
	}
	mapping := make(map[protocol.ReasoningEffort]protocol.ReasoningEffort, len(allReasoningEfforts))
	for _, effort := range allReasoningEfforts {
		mapping[effort] = nearestEffort(effort, supported)
	}
	return mapping
}

// allReasoningEfforts mirrors the Rust `ReasoningEffort::iter()` order.
var allReasoningEfforts = []protocol.ReasoningEffort{
	protocol.ReasoningEffortNone,
	protocol.ReasoningEffortMinimal,
	protocol.ReasoningEffortLow,
	protocol.ReasoningEffortMedium,
	protocol.ReasoningEffortHigh,
	protocol.ReasoningEffortXHigh,
}

// effortRank assigns an ordinal rank to a reasoning effort. Rust: effort_rank.
func effortRank(effort protocol.ReasoningEffort) int {
	switch effort {
	case protocol.ReasoningEffortNone:
		return 0
	case protocol.ReasoningEffortMinimal:
		return 1
	case protocol.ReasoningEffortLow:
		return 2
	case protocol.ReasoningEffortMedium:
		return 3
	case protocol.ReasoningEffortHigh:
		return 4
	case protocol.ReasoningEffortXHigh:
		return 5
	default:
		return 0
	}
}

// nearestEffort returns the supported effort whose rank is closest to target.
// Rust's `min_by_key` keeps the first minimum encountered, so iteration order of
// `supported` is preserved here. Falls back to target when supported is empty.
func nearestEffort(target protocol.ReasoningEffort, supported []protocol.ReasoningEffort) protocol.ReasoningEffort {
	targetRank := effortRank(target)
	best := target
	bestDist := -1
	for _, candidate := range supported {
		dist := effortRank(candidate) - targetRank
		if dist < 0 {
			dist = -dist
		}
		if bestDist == -1 || dist < bestDist {
			best = candidate
			bestDist = dist
		}
	}
	return best
}

// derefStringOr returns the pointed-to string, or fallback when ptr is nil.
func derefStringOr(ptr *string, fallback string) string {
	if ptr == nil {
		return fallback
	}
	return *ptr
}
