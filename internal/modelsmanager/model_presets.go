package modelsmanager

// Legacy notice keys kept for config compatibility with older migration prompts.
//
// Hardcoded model presets were removed; model listings are now derived from the
// active catalog. Rust: model_presets.rs.
const (
	// HideGPT51MigrationPromptConfig is the legacy "hide GPT-5.1 migration
	// prompt" config key.
	HideGPT51MigrationPromptConfig = "hide_gpt5_1_migration_prompt"
	// HideGPT51CodexMaxMigrationPromptConfig is the legacy "hide
	// gpt-5.1-codex-max migration prompt" config key.
	HideGPT51CodexMaxMigrationPromptConfig = "hide_gpt-5.1-codex-max_migration_prompt"
)
