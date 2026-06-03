package protocol

// This file ports `memory_citation.rs`: citation metadata attached to assistant
// messages. Rust: camelCase.

// MemoryCitation groups citation entries with their source rollout ids.
type MemoryCitation struct {
	Entries    []MemoryCitationEntry `json:"entries"`
	RolloutIDs []string              `json:"rolloutIds"`
}

// MemoryCitationEntry is a single cited memory span.
type MemoryCitationEntry struct {
	Path      string `json:"path"`
	LineStart uint32 `json:"lineStart"`
	LineEnd   uint32 `json:"lineEnd"`
	Note      string `json:"note"`
}
