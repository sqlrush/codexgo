package modelsmanager

import (
	"encoding/json"
	"fmt"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// ModelInfo is the model metadata returned by the Codex backend `/models`
// endpoint and bundled in models.json. Rust: openai_models::ModelInfo.
//
// Field order matches the Rust struct declaration so the canonical JSON output
// (key order excepted) is byte-for-byte compatible. Custom (Un)MarshalJSON is
// used to replicate serde semantics: skip_serializing_if on Option fields,
// defaults on omitted fields, the never-serialized used_fallback_model_metadata
// marker, and the unknown-value-dropping deserialize for tool_mode.
type ModelInfo struct {
	Slug                      string
	DisplayName               string
	Description               *string
	DefaultReasoningLevel     *protocol.ReasoningEffort
	SupportedReasoningLevels  []ReasoningEffortPreset
	ShellType                 ConfigShellToolType
	Visibility                ModelVisibility
	SupportedInAPI            bool
	Priority                  int32
	AdditionalSpeedTiers      []string
	ServiceTiers              []ModelServiceTier
	DefaultServiceTier        *string
	AvailabilityNux           *ModelAvailabilityNux
	Upgrade                   *ModelInfoUpgrade
	BaseInstructions          string
	ModelMessages             *ModelMessages
	SupportsReasoningSummary  bool
	DefaultReasoningSummary   protocol.ReasoningSummary
	SupportVerbosity          bool
	DefaultVerbosity          *protocol.Verbosity
	ApplyPatchToolType        *ApplyPatchToolType
	WebSearchToolType         WebSearchToolType
	TruncationPolicy          TruncationPolicyConfig
	SupportsParallelToolCalls bool
	SupportsImageDetailOrig   bool
	ContextWindow             *int64
	MaxContextWindow          *int64
	AutoCompactTokenLimit     *int64
	EffectiveCtxWindowPercent int64
	ExperimentalSupportedTool []string
	InputModalities           []protocol.InputModality
	// UsedFallbackModelMetadata is an internal marker set by core when a slug
	// resolved to fallback metadata. Rust: skip_serializing + skip_deserializing.
	UsedFallbackModelMetadata bool
	SupportsSearchTool        bool
	ToolMode                  *ToolMode
}

// modelInfoJSON mirrors the on-the-wire field order and serde attributes of
// ModelInfo. Pointer-typed optional fields with skip_serializing_if use
// omitempty; the others are always emitted (null when absent).
type modelInfoJSON struct {
	Slug                      string                    `json:"slug"`
	DisplayName               string                    `json:"display_name"`
	Description               *string                   `json:"description"`
	DefaultReasoningLevel     *protocol.ReasoningEffort `json:"default_reasoning_level,omitempty"`
	SupportedReasoningLevels  []ReasoningEffortPreset   `json:"supported_reasoning_levels"`
	ShellType                 ConfigShellToolType       `json:"shell_type"`
	Visibility                ModelVisibility           `json:"visibility"`
	SupportedInAPI            bool                      `json:"supported_in_api"`
	Priority                  int32                     `json:"priority"`
	AdditionalSpeedTiers      []string                  `json:"additional_speed_tiers"`
	ServiceTiers              []ModelServiceTier        `json:"service_tiers"`
	DefaultServiceTier        *string                   `json:"default_service_tier,omitempty"`
	AvailabilityNux           *ModelAvailabilityNux     `json:"availability_nux"`
	Upgrade                   *ModelInfoUpgrade         `json:"upgrade"`
	BaseInstructions          string                    `json:"base_instructions"`
	ModelMessages             *ModelMessages            `json:"model_messages,omitempty"`
	SupportsReasoningSummary  bool                      `json:"supports_reasoning_summaries"`
	DefaultReasoningSummary   protocol.ReasoningSummary `json:"default_reasoning_summary"`
	SupportVerbosity          bool                      `json:"support_verbosity"`
	DefaultVerbosity          *protocol.Verbosity       `json:"default_verbosity"`
	ApplyPatchToolType        *ApplyPatchToolType       `json:"apply_patch_tool_type"`
	WebSearchToolType         WebSearchToolType         `json:"web_search_tool_type"`
	TruncationPolicy          TruncationPolicyConfig    `json:"truncation_policy"`
	SupportsParallelToolCalls bool                      `json:"supports_parallel_tool_calls"`
	SupportsImageDetailOrig   bool                      `json:"supports_image_detail_original"`
	ContextWindow             *int64                    `json:"context_window,omitempty"`
	MaxContextWindow          *int64                    `json:"max_context_window,omitempty"`
	AutoCompactTokenLimit     *int64                    `json:"auto_compact_token_limit,omitempty"`
	EffectiveCtxWindowPercent int64                     `json:"effective_context_window_percent"`
	ExperimentalSupportedTool []string                  `json:"experimental_supported_tools"`
	InputModalities           []protocol.InputModality  `json:"input_modalities"`
	SupportsSearchTool        bool                      `json:"supports_search_tool"`
	ToolMode                  *ToolMode                 `json:"tool_mode,omitempty"`
}

// MarshalJSON serializes ModelInfo in the canonical field order, applying the
// serde skip/default semantics. used_fallback_model_metadata is never emitted.
func (m ModelInfo) MarshalJSON() ([]byte, error) {
	raw := modelInfoJSON{
		Slug:                      m.Slug,
		DisplayName:               m.DisplayName,
		Description:               m.Description,
		DefaultReasoningLevel:     m.DefaultReasoningLevel,
		SupportedReasoningLevels:  nonNilPresets(m.SupportedReasoningLevels),
		ShellType:                 m.ShellType,
		Visibility:                m.Visibility,
		SupportedInAPI:            m.SupportedInAPI,
		Priority:                  m.Priority,
		AdditionalSpeedTiers:      nonNilStrings(m.AdditionalSpeedTiers),
		ServiceTiers:              nonNilTiers(m.ServiceTiers),
		DefaultServiceTier:        m.DefaultServiceTier,
		AvailabilityNux:           m.AvailabilityNux,
		Upgrade:                   m.Upgrade,
		BaseInstructions:          m.BaseInstructions,
		ModelMessages:             m.ModelMessages,
		SupportsReasoningSummary:  m.SupportsReasoningSummary,
		DefaultReasoningSummary:   m.DefaultReasoningSummary,
		SupportVerbosity:          m.SupportVerbosity,
		DefaultVerbosity:          m.DefaultVerbosity,
		ApplyPatchToolType:        m.ApplyPatchToolType,
		WebSearchToolType:         m.WebSearchToolType,
		TruncationPolicy:          m.TruncationPolicy,
		SupportsParallelToolCalls: m.SupportsParallelToolCalls,
		SupportsImageDetailOrig:   m.SupportsImageDetailOrig,
		ContextWindow:             m.ContextWindow,
		MaxContextWindow:          m.MaxContextWindow,
		AutoCompactTokenLimit:     m.AutoCompactTokenLimit,
		EffectiveCtxWindowPercent: m.EffectiveCtxWindowPercent,
		ExperimentalSupportedTool: nonNilStrings(m.ExperimentalSupportedTool),
		InputModalities:           nonNilModalities(m.InputModalities),
		SupportsSearchTool:        m.SupportsSearchTool,
		ToolMode:                  m.ToolMode,
	}
	return json.Marshal(raw)
}

// UnmarshalJSON deserializes ModelInfo, applying serde defaults for omitted
// fields and dropping unknown tool_mode values. Unknown top-level fields are
// ignored (Rust has no deny_unknown_fields).
func (m *ModelInfo) UnmarshalJSON(data []byte) error {
	// Defaults applied before decoding so omitted serde-default fields match.
	raw := modelInfoJSON{
		WebSearchToolType:         WebSearchToolTypeText,
		EffectiveCtxWindowPercent: defaultEffectiveContextWindowPercent,
		InputModalities:           protocol.DefaultInputModalities(),
	}
	// tool_mode needs custom handling (unknown values -> nil) so decode it
	// separately via a probe struct.
	var probe struct {
		ToolMode *json.RawMessage `json:"tool_mode"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return fmt.Errorf("decode model_info tool_mode probe: %w", err)
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("decode model_info: %w", err)
	}

	toolMode := decodeToolMode(probe.ToolMode)

	*m = ModelInfo{
		Slug:                      raw.Slug,
		DisplayName:               raw.DisplayName,
		Description:               raw.Description,
		DefaultReasoningLevel:     raw.DefaultReasoningLevel,
		SupportedReasoningLevels:  nonNilPresets(raw.SupportedReasoningLevels),
		ShellType:                 raw.ShellType,
		Visibility:                raw.Visibility,
		SupportedInAPI:            raw.SupportedInAPI,
		Priority:                  raw.Priority,
		AdditionalSpeedTiers:      nonNilStrings(raw.AdditionalSpeedTiers),
		ServiceTiers:              nonNilTiers(raw.ServiceTiers),
		DefaultServiceTier:        raw.DefaultServiceTier,
		AvailabilityNux:           raw.AvailabilityNux,
		Upgrade:                   raw.Upgrade,
		BaseInstructions:          raw.BaseInstructions,
		ModelMessages:             raw.ModelMessages,
		SupportsReasoningSummary:  raw.SupportsReasoningSummary,
		DefaultReasoningSummary:   raw.DefaultReasoningSummary,
		SupportVerbosity:          raw.SupportVerbosity,
		DefaultVerbosity:          raw.DefaultVerbosity,
		ApplyPatchToolType:        raw.ApplyPatchToolType,
		WebSearchToolType:         raw.WebSearchToolType,
		TruncationPolicy:          raw.TruncationPolicy,
		SupportsParallelToolCalls: raw.SupportsParallelToolCalls,
		SupportsImageDetailOrig:   raw.SupportsImageDetailOrig,
		ContextWindow:             raw.ContextWindow,
		MaxContextWindow:          raw.MaxContextWindow,
		AutoCompactTokenLimit:     raw.AutoCompactTokenLimit,
		EffectiveCtxWindowPercent: raw.EffectiveCtxWindowPercent,
		ExperimentalSupportedTool: nonNilStrings(raw.ExperimentalSupportedTool),
		InputModalities:           nonNilModalities(raw.InputModalities),
		UsedFallbackModelMetadata: false,
		SupportsSearchTool:        raw.SupportsSearchTool,
		ToolMode:                  toolMode,
	}
	return nil
}

// decodeToolMode interprets the raw tool_mode value, returning nil for null,
// absent, or unrecognized values (matching deserialize_optional_model_selector).
func decodeToolMode(raw *json.RawMessage) *ToolMode {
	if raw == nil {
		return nil
	}
	var s *string
	if err := json.Unmarshal(*raw, &s); err != nil || s == nil {
		return nil
	}
	if !isKnownToolMode(*s) {
		return nil
	}
	mode := ToolMode(*s)
	return &mode
}

// ResolvedContextWindow returns the effective context window, preferring
// ContextWindow over MaxContextWindow.
func (m ModelInfo) ResolvedContextWindow() *int64 {
	if m.ContextWindow != nil {
		return m.ContextWindow
	}
	return m.MaxContextWindow
}

// AutoCompactTokenLimitResolved derives the auto-compaction token threshold,
// clamping any configured limit to 90% of the resolved context window.
func (m ModelInfo) AutoCompactTokenLimitResolved() *int64 {
	var contextLimit *int64
	if cw := m.ResolvedContextWindow(); cw != nil {
		v := (*cw * 9) / 10
		contextLimit = &v
	}
	configLimit := m.AutoCompactTokenLimit
	if contextLimit != nil {
		if configLimit != nil && *configLimit < *contextLimit {
			return configLimit
		}
		return contextLimit
	}
	return configLimit
}

// SupportsPersonality reports whether the model's message template supports
// personality-specific instructions.
func (m ModelInfo) SupportsPersonality() bool {
	return m.ModelMessages != nil && m.ModelMessages.SupportsPersonality()
}

// SupportsServiceTier reports whether the model advertises the given tier id.
func (m ModelInfo) SupportsServiceTier(serviceTier string) bool {
	for _, tier := range m.ServiceTiers {
		if tier.ID == serviceTier {
			return true
		}
	}
	return false
}

// ServiceTierForRequest filters a requested service tier, dropping the explicit
// default sentinel and unsupported tiers.
func (m ModelInfo) ServiceTierForRequest(serviceTier *string) *string {
	if serviceTier == nil {
		return nil
	}
	if *serviceTier == protocol.ServiceTierDefaultRequestValue || !m.SupportsServiceTier(*serviceTier) {
		return nil
	}
	return serviceTier
}

// GetModelInstructions renders the model instructions for the given personality,
// falling back to BaseInstructions when no usable template is present.
func (m ModelInfo) GetModelInstructions(personality *protocol.Personality) string {
	if m.ModelMessages != nil && m.ModelMessages.InstructionsTemplate != nil {
		template := *m.ModelMessages.InstructionsTemplate
		msg := m.ModelMessages.GetPersonalityMessage(personality)
		replacement := ""
		if msg != nil {
			replacement = *msg
		}
		return replaceAll(template, personalityPlaceholder, replacement)
	}
	return m.BaseInstructions
}
