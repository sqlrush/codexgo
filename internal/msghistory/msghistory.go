// Package msghistory is the persistence layer for the global, append-only
// *message history* file, a faithful Go port of the Rust `codex-message-history`
// crate.
//
// The history is stored at `~/.codex/history.jsonl` with one JSON object per
// line so it can be efficiently appended to and parsed with standard JSON-Lines
// tooling. Each record has the following schema:
//
//	{"session_id":"<uuid>","ts":<unix_seconds>,"text":"<message>"}
//
// To minimize the chance of interleaved writes when multiple processes are
// appending concurrently, callers prepare the full line (record + trailing "\n")
// and write it with a single write(2) syscall while the file descriptor is
// opened with O_APPEND. POSIX guarantees that writes up to PIPE_BUF bytes are
// atomic in that case.
package msghistory

import (
	"path/filepath"
)

// HistoryFilename is the filename that stores the message history inside
// `~/.codex`. Mirrors the Rust `HISTORY_FILENAME` constant.
const HistoryFilename = "history.jsonl"

// historySoftCapRatio is the fraction of max_bytes the history is trimmed down
// to once it exceeds the hard cap. Mirrors `HISTORY_SOFT_CAP_RATIO`.
const historySoftCapRatio = 0.8

// HistoryPersistence controls whether history entries are written to disk.
//
// Mirrors the Rust `codex_config::types::HistoryPersistence` enum, which uses
// serde `rename_all = "kebab-case"`. It is reproduced here so the package does
// not depend on the config crate.
type HistoryPersistence string

const (
	// HistoryPersistenceSaveAll persists every history entry. This is the
	// serde default.
	HistoryPersistenceSaveAll HistoryPersistence = "save-all"
	// HistoryPersistenceNone disables history persistence entirely.
	HistoryPersistenceNone HistoryPersistence = "none"
)

// HistoryEntry is a single record in the message history file.
//
// Mirrors the Rust `HistoryEntry` struct; field names are emitted verbatim so
// the JSON encoding matches byte-for-byte.
type HistoryEntry struct {
	SessionID string `json:"session_id"`
	// Ts is the Unix timestamp in seconds when the entry was recorded.
	Ts   uint64 `json:"ts"`
	Text string `json:"text"`
}

// HistoryConfig captures the inputs required to locate and bound the history
// file. Mirrors the Rust `HistoryConfig` struct.
type HistoryConfig struct {
	// CodexHome is the `~/.codex` directory that contains the history file.
	CodexHome string
	// Persistence selects whether entries are written at all.
	Persistence HistoryPersistence
	// MaxBytes is the optional hard cap on the history file size. nil means no
	// cap.
	MaxBytes *uint64
}

// NewHistoryConfig builds a HistoryConfig from a codex home directory and the
// persistence/max-bytes settings, mirroring the Rust `HistoryConfig::new`.
func NewHistoryConfig(codexHome string, persistence HistoryPersistence, maxBytes *uint64) HistoryConfig {
	return HistoryConfig{
		CodexHome:   codexHome,
		Persistence: persistence,
		MaxBytes:    maxBytes,
	}
}

// historyFilepath resolves the history file path for the config.
func historyFilepath(config HistoryConfig) string {
	return filepath.Join(config.CodexHome, HistoryFilename)
}
