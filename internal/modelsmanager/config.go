package modelsmanager

// ModelsManagerConfig carries the configuration knobs that influence model
// metadata resolution. Rust: config::ModelsManagerConfig (Debug, Clone, Default).
//
// All fields are optional; the zero value mirrors Rust's `Default` (every
// Option is None and PersonalityEnabled is false).
type ModelsManagerConfig struct {
	// ModelContextWindow overrides the model context window when set, clamped to
	// MaxContextWindow.
	ModelContextWindow *int64
	// ModelAutoCompactTokenLimit overrides the auto-compaction token limit.
	ModelAutoCompactTokenLimit *int64
	// ToolOutputTokenLimit overrides the truncation policy budget. Modeled as a
	// *int to mirror Rust's Option<usize>.
	ToolOutputTokenLimit *int
	// BaseInstructions replaces the model's base instructions and clears the
	// strongly-typed message template when set.
	BaseInstructions *string
	// PersonalityEnabled keeps the strongly-typed message template when no
	// BaseInstructions override is present.
	PersonalityEnabled bool
	// ModelSupportsReasoningSummaries, when Some(true), forces reasoning-summary
	// support on. Some(false) is a no-op (matching Rust semantics).
	ModelSupportsReasoningSummaries *bool
	// ModelCatalog is an authoritative catalog used by offline test helpers.
	ModelCatalog *ModelsResponse
}
