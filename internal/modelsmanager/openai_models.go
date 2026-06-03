// Package modelsmanager is a faithful, drop-in-compatible Go port of the
// codex-rs `models-manager` crate (codex 0.136.0). It owns the model catalog,
// refresh strategies (Online / Offline / OnlineIfUncached), merging of the
// bundled models.json with a remote catalog, the on-disk
// `~/.codex/models_cache.json` cache (300s TTL with ETag conditional refresh),
// and filtering by auth mode.
//
// The shared model-metadata types (ModelInfo, ModelPreset, ModelsResponse, and
// friends) originate from Rust's `codex_protocol::openai_models` module. They
// are defined here because the Go `internal/protocol` package does not yet port
// them; the on-the-wire JSON shapes match the Rust serde output byte-for-byte
// (after key-order canonicalization).
package modelsmanager

import (
	"github.com/sqlrush/codexgo/internal/protocol"
)

// SpeedTierFast is the legacy "fast" speed-tier identifier. Rust:
// `openai_models::SPEED_TIER_FAST`.
const SpeedTierFast = "fast"

// serviceTierFastRequestValue is the request value reported by
// protocol.ServiceTierFast.request_value() in Rust ("priority", not "fast").
// It is duplicated here to avoid redefining protocol types.
const serviceTierFastRequestValue = "priority"

// personalityPlaceholder is the token replaced in instruction templates by the
// resolved personality message.
const personalityPlaceholder = "{{ personality }}"

// defaultEffectiveContextWindowPercent is the serde default for
// ModelInfo.EffectiveContextWindowPercent.
const defaultEffectiveContextWindowPercent int64 = 95

// ReasoningEffortPreset is a reasoning-effort option surfaced for a model.
// Rust: openai_models::ReasoningEffortPreset.
type ReasoningEffortPreset struct {
	Effort      protocol.ReasoningEffort `json:"effort"`
	Description string                   `json:"description"`
}

// ModelUpgrade describes a recommended upgrade preset on a ModelPreset.
// Rust: openai_models::ModelUpgrade.
type ModelUpgrade struct {
	ID                     string                                                `json:"id"`
	ReasoningEffortMapping map[protocol.ReasoningEffort]protocol.ReasoningEffort `json:"reasoning_effort_mapping"`
	MigrationConfigKey     string                                                `json:"migration_config_key"`
	ModelLink              *string                                               `json:"model_link"`
	UpgradeCopy            *string                                               `json:"upgrade_copy"`
	MigrationMarkdown      *string                                               `json:"migration_markdown"`
}

// ModelAvailabilityNux is the availability NUX message shown when a model
// becomes accessible. Rust: openai_models::ModelAvailabilityNux.
type ModelAvailabilityNux struct {
	Message string `json:"message"`
}

// ModelServiceTier describes a service tier a model can run with.
// Rust: openai_models::ModelServiceTier.
type ModelServiceTier struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ModelVisibility describes a model's visibility in the picker or APIs.
// Rust: openai_models::ModelVisibility, serde rename_all = "lowercase".
type ModelVisibility string

const (
	// ModelVisibilityList shows the model in the picker.
	ModelVisibilityList ModelVisibility = "list"
	// ModelVisibilityHide hides the model from the picker but keeps it usable.
	ModelVisibilityHide ModelVisibility = "hide"
	// ModelVisibilityNone hides the model entirely.
	ModelVisibilityNone ModelVisibility = "none"
)

// ConfigShellToolType is a model's shell execution capability.
// Rust: openai_models::ConfigShellToolType, serde rename_all = "snake_case".
type ConfigShellToolType string

const (
	// ConfigShellToolTypeDefault is the default shell tool.
	ConfigShellToolTypeDefault ConfigShellToolType = "default"
	// ConfigShellToolTypeLocal is the local shell tool.
	ConfigShellToolTypeLocal ConfigShellToolType = "local"
	// ConfigShellToolTypeUnifiedExec is the unified exec tool.
	ConfigShellToolTypeUnifiedExec ConfigShellToolType = "unified_exec"
	// ConfigShellToolTypeDisabled disables shell tooling.
	ConfigShellToolTypeDisabled ConfigShellToolType = "disabled"
	// ConfigShellToolTypeShellCommand is the shell_command tool.
	ConfigShellToolTypeShellCommand ConfigShellToolType = "shell_command"
)

// ApplyPatchToolType selects the apply_patch tool dialect.
// Rust: openai_models::ApplyPatchToolType, serde rename_all = "snake_case".
type ApplyPatchToolType string

const (
	// ApplyPatchToolTypeFreeform is the freeform apply_patch tool.
	ApplyPatchToolTypeFreeform ApplyPatchToolType = "freeform"
)

// WebSearchToolType selects the web-search tool capability.
// Rust: openai_models::WebSearchToolType, serde rename_all = "snake_case",
// default = Text.
type WebSearchToolType string

const (
	// WebSearchToolTypeText is the text-only web-search tool.
	WebSearchToolTypeText WebSearchToolType = "text"
	// WebSearchToolTypeTextAndImage is the text-and-image web-search tool.
	WebSearchToolTypeTextAndImage WebSearchToolType = "text_and_image"
)

// TruncationMode selects how output should be truncated.
// Rust: openai_models::TruncationMode, serde rename_all = "snake_case".
type TruncationMode string

const (
	// TruncationModeBytes enforces a byte budget.
	TruncationModeBytes TruncationMode = "bytes"
	// TruncationModeTokens enforces a token budget.
	TruncationModeTokens TruncationMode = "tokens"
)

// ToolMode selects the model's tool execution mode.
// Rust: openai_models::ToolMode, serde rename_all = "snake_case".
type ToolMode string

const (
	// ToolModeDirect uses direct tool calls.
	ToolModeDirect ToolMode = "direct"
	// ToolModeCodeMode uses code-mode tool calls.
	ToolModeCodeMode ToolMode = "code_mode"
	// ToolModeCodeModeOnly uses code-mode-only tool calls.
	ToolModeCodeModeOnly ToolMode = "code_mode_only"
)

// isKnownToolMode reports whether v is a recognized ToolMode value. Unknown
// values deserialize to nil (mirroring Rust's
// deserialize_optional_model_selector behavior).
func isKnownToolMode(v string) bool {
	switch ToolMode(v) {
	case ToolModeDirect, ToolModeCodeMode, ToolModeCodeModeOnly:
		return true
	default:
		return false
	}
}

// TruncationPolicyConfig is server-provided truncation policy metadata for a
// model. Rust: openai_models::TruncationPolicyConfig.
type TruncationPolicyConfig struct {
	Mode  TruncationMode `json:"mode"`
	Limit int64          `json:"limit"`
}

// TruncationPolicyBytes builds a byte-budgeted truncation policy.
func TruncationPolicyBytes(limit int64) TruncationPolicyConfig {
	return TruncationPolicyConfig{Mode: TruncationModeBytes, Limit: limit}
}

// TruncationPolicyTokens builds a token-budgeted truncation policy.
func TruncationPolicyTokens(limit int64) TruncationPolicyConfig {
	return TruncationPolicyConfig{Mode: TruncationModeTokens, Limit: limit}
}

// ModelInstructionsVariables holds the personality template variables.
// Rust: openai_models::ModelInstructionsVariables.
type ModelInstructionsVariables struct {
	PersonalityDefault   *string `json:"personality_default"`
	PersonalityFriendly  *string `json:"personality_friendly"`
	PersonalityPragmatic *string `json:"personality_pragmatic"`
}

// IsComplete reports whether all three personality variants are present.
func (v *ModelInstructionsVariables) IsComplete() bool {
	return v.PersonalityDefault != nil &&
		v.PersonalityFriendly != nil &&
		v.PersonalityPragmatic != nil
}

// GetPersonalityMessage returns the personality message for the given
// personality, or nil when unavailable. A nil personality returns the default.
func (v *ModelInstructionsVariables) GetPersonalityMessage(personality *protocol.Personality) *string {
	if personality == nil {
		return cloneStringPtr(v.PersonalityDefault)
	}
	switch *personality {
	case protocol.PersonalityNone:
		empty := ""
		return &empty
	case protocol.PersonalityFriendly:
		return cloneStringPtr(v.PersonalityFriendly)
	case protocol.PersonalityPragmatic:
		return cloneStringPtr(v.PersonalityPragmatic)
	default:
		return nil
	}
}

// ModelMessages is a strongly-typed template for assembling model instructions
// and developer messages. Rust: openai_models::ModelMessages.
type ModelMessages struct {
	InstructionsTemplate  *string                     `json:"instructions_template"`
	InstructionsVariables *ModelInstructionsVariables `json:"instructions_variables"`
}

func (m *ModelMessages) hasPersonalityPlaceholder() bool {
	return m.InstructionsTemplate != nil &&
		containsSubstring(*m.InstructionsTemplate, personalityPlaceholder)
}

// SupportsPersonality reports whether the message template supports
// personality-specific instructions.
func (m *ModelMessages) SupportsPersonality() bool {
	return m.hasPersonalityPlaceholder() &&
		m.InstructionsVariables != nil &&
		m.InstructionsVariables.IsComplete()
}

// GetPersonalityMessage returns the personality message for the given
// personality, or nil.
func (m *ModelMessages) GetPersonalityMessage(personality *protocol.Personality) *string {
	if m.InstructionsVariables == nil {
		return nil
	}
	return m.InstructionsVariables.GetPersonalityMessage(personality)
}

// ModelInfoUpgrade is the upgrade reference embedded in a ModelInfo.
// Rust: openai_models::ModelInfoUpgrade.
type ModelInfoUpgrade struct {
	Model             string `json:"model"`
	MigrationMarkdown string `json:"migration_markdown"`
}

// ModelsResponse is the response wrapper for the `/models` endpoint.
// Rust: openai_models::ModelsResponse.
type ModelsResponse struct {
	Models []ModelInfo `json:"models"`
}
