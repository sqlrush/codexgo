package modelsmanager

import (
	"math"

	_ "embed"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// BaseInstructions is the bundled base prompt. Rust: BASE_INSTRUCTIONS =
// include_str!("../prompt.md").
//
//go:embed prompt.md
var BaseInstructions string

const (
	// defaultPersonalityHeader is prepended to local personality templates.
	defaultPersonalityHeader = "You are Codex, a coding agent based on GPT-5. You and the user share the same workspace and collaborate to achieve the user's goals."
	// localFriendlyTemplate is the local "friendly" personality message.
	localFriendlyTemplate = "You optimize for team morale and being a supportive teammate as much as code quality."
	// localPragmaticTemplate is the local "pragmatic" personality message.
	localPragmaticTemplate = "You are a deeply pragmatic, effective software engineer."
)

// approxBytesPerToken is the bytes-per-token ratio used for token->byte
// estimation. Rust: codex_utils_string APPROX_BYTES_PER_TOKEN.
const approxBytesPerToken = 4

// approxBytesForTokens returns the approximate number of bytes occupied by the
// given number of tokens, saturating at math.MaxInt on overflow. Rust:
// approx_bytes_for_tokens (tokens.saturating_mul(4)).
func approxBytesForTokens(tokens int) int {
	if tokens <= 0 {
		return 0
	}
	if tokens > math.MaxInt/approxBytesPerToken {
		return math.MaxInt
	}
	return tokens * approxBytesPerToken
}

// WithConfigOverrides applies the configuration overrides onto a ModelInfo and
// returns a new value; the input is not mutated. Rust: with_config_overrides.
func WithConfigOverrides(model ModelInfo, config *ModelsManagerConfig) ModelInfo {
	updated := model

	if config.ModelSupportsReasoningSummaries != nil && *config.ModelSupportsReasoningSummaries {
		updated.SupportsReasoningSummary = true
	}

	if config.ModelContextWindow != nil {
		contextWindow := *config.ModelContextWindow
		if updated.MaxContextWindow != nil {
			contextWindow = min64(contextWindow, *updated.MaxContextWindow)
		}
		updated.ContextWindow = int64Ptr(contextWindow)
	}

	if config.ModelAutoCompactTokenLimit != nil {
		updated.AutoCompactTokenLimit = int64Ptr(*config.ModelAutoCompactTokenLimit)
	}

	if config.ToolOutputTokenLimit != nil {
		tokenLimit := *config.ToolOutputTokenLimit
		switch updated.TruncationPolicy.Mode {
		case TruncationModeBytes:
			byteLimit := clampToInt64(approxBytesForTokens(tokenLimit))
			updated.TruncationPolicy = TruncationPolicyBytes(byteLimit)
		case TruncationModeTokens:
			updated.TruncationPolicy = TruncationPolicyTokens(clampToInt64(tokenLimit))
		}
	}

	if config.BaseInstructions != nil {
		updated.BaseInstructions = *config.BaseInstructions
		updated.ModelMessages = nil
	} else if !config.PersonalityEnabled {
		updated.ModelMessages = nil
	}

	return updated
}

// ModelInfoFromSlug builds a minimal fallback model descriptor for
// missing/unknown slugs. Rust: model_info_from_slug.
func ModelInfoFromSlug(slug string) ModelInfo {
	defaultReasoningSummary := protocol.ReasoningSummaryAuto
	return ModelInfo{
		Slug:                      slug,
		DisplayName:               slug,
		Description:               nil,
		DefaultReasoningLevel:     nil,
		SupportedReasoningLevels:  []ReasoningEffortPreset{},
		ShellType:                 ConfigShellToolTypeDefault,
		Visibility:                ModelVisibilityNone,
		SupportedInAPI:            true,
		Priority:                  99,
		AdditionalSpeedTiers:      []string{},
		ServiceTiers:              []ModelServiceTier{},
		DefaultServiceTier:        nil,
		AvailabilityNux:           nil,
		Upgrade:                   nil,
		BaseInstructions:          BaseInstructions,
		ModelMessages:             localPersonalityMessagesForSlug(slug),
		SupportsReasoningSummary:  false,
		DefaultReasoningSummary:   defaultReasoningSummary,
		SupportVerbosity:          false,
		DefaultVerbosity:          nil,
		ApplyPatchToolType:        nil,
		WebSearchToolType:         WebSearchToolTypeText,
		TruncationPolicy:          TruncationPolicyBytes(10_000),
		SupportsParallelToolCalls: false,
		SupportsImageDetailOrig:   false,
		ContextWindow:             int64Ptr(272_000),
		MaxContextWindow:          int64Ptr(272_000),
		AutoCompactTokenLimit:     nil,
		EffectiveCtxWindowPercent: 95,
		ExperimentalSupportedTool: []string{},
		InputModalities:           protocol.DefaultInputModalities(),
		UsedFallbackModelMetadata: true, // this is the fallback model metadata
		SupportsSearchTool:        false,
		ToolMode:                  nil,
	}
}

// localPersonalityMessagesForSlug returns the local personality message template
// for slugs that carry one, or nil. Rust: local_personality_messages_for_slug.
func localPersonalityMessagesForSlug(slug string) *ModelMessages {
	switch slug {
	case "gpt-5.2-codex", "exp-codex-personality":
		template := defaultPersonalityHeader + "\n\n" + personalityPlaceholder + "\n\n" + BaseInstructions
		empty := ""
		friendly := localFriendlyTemplate
		pragmatic := localPragmaticTemplate
		return &ModelMessages{
			InstructionsTemplate: &template,
			InstructionsVariables: &ModelInstructionsVariables{
				PersonalityDefault:   &empty,
				PersonalityFriendly:  &friendly,
				PersonalityPragmatic: &pragmatic,
			},
		}
	default:
		return nil
	}
}

// min64 returns the smaller of a and b.
func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// clampToInt64 converts an int to int64, saturating at math.MaxInt64. This
// mirrors Rust's `i64::try_from(...).unwrap_or(i64::MAX)`.
func clampToInt64(v int) int64 {
	if int64(v) < 0 {
		// On 64-bit platforms int == int64, so this is unreachable for v >= 0;
		// guard anyway in case of platform differences.
		return math.MaxInt64
	}
	return int64(v)
}
