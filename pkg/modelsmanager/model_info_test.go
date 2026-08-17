package modelsmanager

import (
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// boolPtr returns a pointer to v.
func boolPtr(v bool) *bool { return &v }

func TestReasoningSummariesOverrideTrueEnablesSupport(t *testing.T) {
	model := ModelInfoFromSlug("unknown-model")
	config := &ModelsManagerConfig{
		ModelSupportsReasoningSummaries: boolPtr(true),
	}

	updated := WithConfigOverrides(model, config)
	if !updated.SupportsReasoningSummary {
		t.Fatalf("expected reasoning summaries to be enabled")
	}
	// The original must be unchanged (immutability).
	if model.SupportsReasoningSummary {
		t.Fatalf("original model was mutated")
	}
}

func TestReasoningSummariesOverrideFalseDoesNotDisableSupport(t *testing.T) {
	model := ModelInfoFromSlug("unknown-model")
	model.SupportsReasoningSummary = true
	config := &ModelsManagerConfig{
		ModelSupportsReasoningSummaries: boolPtr(false),
	}

	updated := WithConfigOverrides(model, config)
	if !updated.SupportsReasoningSummary {
		t.Fatalf("Some(false) override should be a no-op when model already supports")
	}
}

func TestReasoningSummariesOverrideFalseIsNoopWhenModelIsFalse(t *testing.T) {
	model := ModelInfoFromSlug("unknown-model")
	config := &ModelsManagerConfig{
		ModelSupportsReasoningSummaries: boolPtr(false),
	}

	updated := WithConfigOverrides(model, config)
	if updated.SupportsReasoningSummary {
		t.Fatalf("Some(false) override should not enable support")
	}
}

func TestModelContextWindowOverrideClampsToMaxContextWindow(t *testing.T) {
	model := ModelInfoFromSlug("unknown-model")
	model.ContextWindow = int64Ptr(273_000)
	model.MaxContextWindow = int64Ptr(400_000)
	config := &ModelsManagerConfig{
		ModelContextWindow: int64Ptr(500_000),
	}

	updated := WithConfigOverrides(model, config)
	if updated.ContextWindow == nil || *updated.ContextWindow != 400_000 {
		t.Fatalf("expected context window clamped to 400000, got %v", updated.ContextWindow)
	}
}

func TestModelContextWindowUsesModelValueWithoutOverride(t *testing.T) {
	model := ModelInfoFromSlug("unknown-model")
	model.ContextWindow = int64Ptr(273_000)
	model.MaxContextWindow = int64Ptr(400_000)
	config := &ModelsManagerConfig{}

	updated := WithConfigOverrides(model, config)
	if updated.ContextWindow == nil || *updated.ContextWindow != 273_000 {
		t.Fatalf("expected context window unchanged at 273000, got %v", updated.ContextWindow)
	}
}

func TestWithConfigOverridesToolOutputTokenLimit(t *testing.T) {
	tests := []struct {
		name      string
		mode      TruncationMode
		limit     int
		wantMode  TruncationMode
		wantLimit int64
	}{
		{
			name:      "bytes mode multiplies by bytes-per-token",
			mode:      TruncationModeBytes,
			limit:     100,
			wantMode:  TruncationModeBytes,
			wantLimit: 400,
		},
		{
			name:      "tokens mode keeps token limit",
			mode:      TruncationModeTokens,
			limit:     123,
			wantMode:  TruncationModeTokens,
			wantLimit: 123,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := ModelInfoFromSlug("unknown-model")
			model.TruncationPolicy = TruncationPolicyConfig{Mode: tt.mode, Limit: 10}
			limit := tt.limit
			config := &ModelsManagerConfig{ToolOutputTokenLimit: &limit}

			updated := WithConfigOverrides(model, config)
			if updated.TruncationPolicy.Mode != tt.wantMode {
				t.Fatalf("mode = %v, want %v", updated.TruncationPolicy.Mode, tt.wantMode)
			}
			if updated.TruncationPolicy.Limit != tt.wantLimit {
				t.Fatalf("limit = %d, want %d", updated.TruncationPolicy.Limit, tt.wantLimit)
			}
		})
	}
}

func TestWithConfigOverridesBaseInstructionsClearsMessages(t *testing.T) {
	model := ModelInfoFromSlug("gpt-5.2-codex")
	if model.ModelMessages == nil {
		t.Fatalf("gpt-5.2-codex should carry local personality messages")
	}
	custom := "custom base instructions"
	config := &ModelsManagerConfig{BaseInstructions: &custom}

	updated := WithConfigOverrides(model, config)
	if updated.BaseInstructions != custom {
		t.Fatalf("base instructions not overridden")
	}
	if updated.ModelMessages != nil {
		t.Fatalf("model messages should be cleared when base instructions override is set")
	}
}

func TestWithConfigOverridesPersonalityDisabledClearsMessages(t *testing.T) {
	model := ModelInfoFromSlug("gpt-5.2-codex")
	config := &ModelsManagerConfig{PersonalityEnabled: false}

	updated := WithConfigOverrides(model, config)
	if updated.ModelMessages != nil {
		t.Fatalf("model messages should be cleared when personality is disabled")
	}
}

func TestModelInfoFromSlugFallbackMetadata(t *testing.T) {
	model := ModelInfoFromSlug("some-unknown-slug")
	if !model.UsedFallbackModelMetadata {
		t.Fatalf("fallback metadata flag should be set")
	}
	if model.Slug != "some-unknown-slug" || model.DisplayName != "some-unknown-slug" {
		t.Fatalf("slug/display name mismatch: %q / %q", model.Slug, model.DisplayName)
	}
	if model.Priority != 99 {
		t.Fatalf("fallback priority should be 99, got %d", model.Priority)
	}
	if model.Visibility != ModelVisibilityNone {
		t.Fatalf("fallback visibility should be None, got %q", model.Visibility)
	}
	if model.DefaultReasoningSummary != protocol.ReasoningSummaryAuto {
		t.Fatalf("fallback reasoning summary should be Auto, got %q", model.DefaultReasoningSummary)
	}
	if model.TruncationPolicy != TruncationPolicyBytes(10_000) {
		t.Fatalf("fallback truncation policy mismatch: %+v", model.TruncationPolicy)
	}
}

func TestModelInfoFromSlugLocalPersonality(t *testing.T) {
	for _, slug := range []string{"gpt-5.2-codex", "exp-codex-personality"} {
		model := ModelInfoFromSlug(slug)
		if model.ModelMessages == nil {
			t.Fatalf("%s should carry local personality messages", slug)
		}
		if !model.SupportsPersonality() {
			t.Fatalf("%s should support personality", slug)
		}
	}
	other := ModelInfoFromSlug("gpt-other")
	if other.ModelMessages != nil {
		t.Fatalf("non-personality slug should not carry messages")
	}
}
