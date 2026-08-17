package core

import (
	"github.com/sqlrush/codexgo/internal/utils/truncation"
	"github.com/sqlrush/codexgo/pkg/features"
	"github.com/sqlrush/codexgo/pkg/modelsmanager"
	"github.com/sqlrush/codexgo/pkg/protocol"
	"github.com/sqlrush/codexgo/pkg/tools"
)

// Per-turn resolution helpers shared by the built-in executors and the
// executor packages layered on core (core/localexec). They read the read-only
// TurnContext snapshot and fall back to defaults when a field is absent.

// unifiedExecOutputMaxTokens is the retained-output token cap of the unified
// exec session manager (unified_exec/mod.rs UNIFIED_EXEC_OUTPUT_MAX_TOKENS =
// 1 MiB / 4). Kept here as the truncation fallback so core does not import
// the local exec engine.
const unifiedExecOutputMaxTokens = 1024 * 1024 / 4

// turnFeatures resolves the effective feature set for a turn, defaulting when
// the turn carries none. Mirrors the Rust `turn_context.features.get()`.
func turnFeatures(tc *TurnContext) *features.Features {
	if tc != nil && tc.Features != nil {
		return tc.Features
	}
	defaults := features.NewFeaturesWithDefaults()
	return &defaults
}

// turnModelInfo resolves the typed model metadata for a turn, falling back to
// slug-derived metadata when the opaque ModelInfo payload is absent or not a
// catalog entry.
func turnModelInfo(tc *TurnContext) modelsmanager.ModelInfo {
	if tc != nil {
		switch v := tc.ModelInfo.(type) {
		case modelsmanager.ModelInfo:
			return v
		case *modelsmanager.ModelInfo:
			if v != nil {
				return *v
			}
		}
	}
	slug := ""
	if tc != nil {
		slug = tc.ModelSlug
	}
	return modelsmanager.ModelInfoFromSlug(slug)
}

// TurnShellToolType resolves which shell tool family the model sees this turn,
// mirroring spec_plan's add_shell_tools selection via
// shell_type_for_model_and_features.
func TurnShellToolType(tc *TurnContext) modelsmanager.ConfigShellToolType {
	mi := turnModelInfo(tc)
	return tools.ShellTypeForModelAndFeatures(&mi, turnFeatures(tc))
}

// turnWebSearchMode resolves the effective web-search mode for a turn,
// mirroring `config.web_search_mode` with codex's default (Cached when
// unconfigured — resolve_web_search_mode falls back to WebSearchMode::Cached).
func turnWebSearchMode(tc *TurnContext) protocol.WebSearchMode {
	if tc != nil && tc.WebSearchMode != "" {
		return tc.WebSearchMode
	}
	return protocol.WebSearchModeCached
}

// turnRequestUserInputEnabled mirrors codex's
// config.experimental_request_user_input_enabled, which defaults to TRUE when
// the [tools.experimental_request_user_input] table is absent
// (resolve_experimental_request_user_input_enabled).
func turnRequestUserInputEnabled(tc *TurnContext) bool {
	if tc == nil || tc.ExperimentalRequestUserInput == nil {
		return true
	}
	return *tc.ExperimentalRequestUserInput
}

// TurnTruncationPolicy resolves the model-output truncation policy for a turn
// from the model's catalog metadata (the Rust `turn.truncation_policy`).
func TurnTruncationPolicy(tc *TurnContext) truncation.TruncationPolicy {
	mi := turnModelInfo(tc)
	switch mi.TruncationPolicy.Mode {
	case modelsmanager.TruncationModeBytes:
		return truncation.BytesPolicy(int(mi.TruncationPolicy.Limit))
	case modelsmanager.TruncationModeTokens:
		return truncation.TokensPolicy(int(mi.TruncationPolicy.Limit))
	default:
		// Unknown/zero policy: fall back to the unified-exec output cap so the
		// response stays bounded.
		return truncation.TokensPolicy(unifiedExecOutputMaxTokens)
	}
}

// ----------------------------------------------------------------------------
// exec_command (UnifiedExec) executor
// ----------------------------------------------------------------------------

// unifiedExecCommandArgs mirrors the Rust `ExecCommandArgs` (handlers/
