// Package rollout is a faithful, drop-in-compatible Go port of the Rust
// `codex-rollout` crate. It persists and discovers Codex session rollout files
// (JSONL) so sessions can be replayed or inspected later.
//
// Rollout files live at:
//
//	~/.codex/sessions/<YYYY>/<MM>/<DD>/rollout-<YYYY-MM-DDThh-mm-ss>-<uuid>.jsonl
//
// (and the parallel archived_sessions/ tree). Each line is a RolloutLine: a
// timestamp plus a tagged RolloutItem (session_meta, response_item, event_msg,
// compacted, or turn_context). The on-disk format matches the Rust crate
// byte-for-byte after key-order canonicalization, since these files are shared
// with real Codex.
package rollout

// SessionsSubdir is the subdirectory under the Codex home that holds active
// session rollout files.
const SessionsSubdir = "sessions"

// ArchivedSessionsSubdir is the subdirectory under the Codex home that holds
// archived session rollout files.
const ArchivedSessionsSubdir = "archived_sessions"

// InteractiveSessionSources returns the session sources considered interactive.
//
// Mirrors the Rust `INTERACTIVE_SESSION_SOURCES` lazy static. A fresh slice is
// returned on each call so callers cannot mutate shared state.
func InteractiveSessionSources() []SessionSource {
	return []SessionSource{
		NewCliSource(),
		NewVSCodeSource(),
		NewCustomSource("atlas"),
		NewCustomSource("chatgpt"),
	}
}

// RolloutConfigView is the read-only configuration the rollout subsystem needs.
//
// Mirrors the Rust `RolloutConfigView` trait.
type RolloutConfigView interface {
	// CodexHome returns the Codex home directory (e.g. ~/.codex).
	CodexHome() string
	// Cwd returns the working directory of the session.
	Cwd() string
	// ModelProviderID returns the active model provider identifier.
	ModelProviderID() string
	// GenerateMemories reports whether memory generation is enabled.
	GenerateMemories() bool
}

// RolloutConfig is a concrete RolloutConfigView.
//
// Mirrors the Rust `RolloutConfig` struct.
type RolloutConfig struct {
	CodexHomeDir       string
	CwdDir             string
	ModelProvider      string
	GenerateMemoriesOn bool
}

// CodexHome returns the Codex home directory.
func (c RolloutConfig) CodexHome() string { return c.CodexHomeDir }

// Cwd returns the session working directory.
func (c RolloutConfig) Cwd() string { return c.CwdDir }

// ModelProviderID returns the model provider identifier.
func (c RolloutConfig) ModelProviderID() string { return c.ModelProvider }

// GenerateMemories reports whether memory generation is enabled.
func (c RolloutConfig) GenerateMemories() bool { return c.GenerateMemoriesOn }
