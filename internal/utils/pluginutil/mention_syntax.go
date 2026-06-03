package pluginutil

// Sigils for tool/plugin mentions in plaintext (shared across Codex crates).
//
// These mirror the Rust constants in `mention_syntax.rs`. They are typed as rune
// to match Rust's `char`.
const (
	// ToolMentionSigil is the default plaintext sigil for tools.
	ToolMentionSigil rune = '$'

	// PluginTextMentionSigil is the sigil plugins use in linked plaintext outside
	// the TUI.
	PluginTextMentionSigil rune = '@'
)
