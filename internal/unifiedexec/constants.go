package unifiedexec

import (
	"crypto/rand"
	"encoding/hex"
)

// Yield-time bounds (milliseconds) governing how long an exec/write call blocks
// collecting output. Mirror the consts in unified_exec/mod.rs.
const (
	// MinYieldTimeMS is the floor for any output-collection window.
	MinYieldTimeMS uint64 = 250
	// MinEmptyYieldTimeMS is the floor for an empty (poll-only) write_stdin.
	MinEmptyYieldTimeMS uint64 = 5_000
	// MaxYieldTimeMS caps a single output-collection window.
	MaxYieldTimeMS uint64 = 30_000
	// DefaultMaxBackgroundTerminalTimeoutMS is the default cap for empty polls.
	DefaultMaxBackgroundTerminalTimeoutMS uint64 = 300_000
)

// Output-size and token caps. Mirror the consts in unified_exec/mod.rs.
const (
	// DefaultMaxOutputTokens is used when a request does not set a token cap.
	DefaultMaxOutputTokens int = 10_000
	// UnifiedExecOutputMaxBytes is the retained-output cap (1 MiB).
	UnifiedExecOutputMaxBytes int = 1024 * 1024
	// UnifiedExecOutputMaxTokens is the retained-output token cap.
	UnifiedExecOutputMaxTokens int = UnifiedExecOutputMaxBytes / 4
	// MaxUnifiedExecProcesses caps how many live sessions the manager retains.
	MaxUnifiedExecProcesses int = 64
)

// unifiedExecEnv is the fixed environment overlay applied to every unified-exec
// process so output is deterministic and pager-free. Mirrors UNIFIED_EXEC_ENV.
var unifiedExecEnv = [...][2]string{
	{"NO_COLOR", "1"},
	{"TERM", "dumb"},
	{"LANG", "C.UTF-8"},
	{"LC_CTYPE", "C.UTF-8"},
	{"LC_ALL", "C.UTF-8"},
	{"COLORTERM", ""},
	{"PAGER", "cat"},
	{"GIT_PAGER", "cat"},
	{"GH_PAGER", "cat"},
	{"CODEX_CI", "1"},
}

// applyUnifiedExecEnv returns a new map equal to env with the unified-exec
// overlay applied on top. The input map is not mutated. Mirrors
// apply_unified_exec_env.
func applyUnifiedExecEnv(env map[string]string) map[string]string {
	out := make(map[string]string, len(env)+len(unifiedExecEnv))
	for k, v := range env {
		out[k] = v
	}
	for _, kv := range unifiedExecEnv {
		out[kv[0]] = kv[1]
	}
	return out
}

// clampYieldTime clamps yield_time_ms into [MinYieldTimeMS, MaxYieldTimeMS].
// Mirrors clamp_yield_time.
func clampYieldTime(yieldTimeMS uint64) uint64 {
	if yieldTimeMS < MinYieldTimeMS {
		return MinYieldTimeMS
	}
	if yieldTimeMS > MaxYieldTimeMS {
		return MaxYieldTimeMS
	}
	return yieldTimeMS
}

// resolveMaxTokens returns the requested token cap or the default when unset.
// Mirrors resolve_max_tokens (Option<usize>).
func resolveMaxTokens(maxTokens *int) int {
	if maxTokens != nil {
		return *maxTokens
	}
	return DefaultMaxOutputTokens
}

// GenerateChunkID returns a fresh 6-hex-digit identifier for an output chunk.
// It is exported for the core tool handlers, which stamp a chunk id onto
// sandbox-denial responses (mirroring the Rust handler's generate_chunk_id use).
func GenerateChunkID() string { return generateChunkID() }

// generateChunkID returns a fresh 6-hex-digit identifier for an output chunk.
// Mirrors generate_chunk_id, which draws 6 random hex nibbles.
func generateChunkID() string {
	var b [3]byte
	if _, err := rand.Read(b[:]); err != nil {
		// rand.Read never fails on supported platforms; fall back to zeros so
		// the contract (a 6-char hex string) still holds.
		return "000000"
	}
	return hex.EncodeToString(b[:])
}
