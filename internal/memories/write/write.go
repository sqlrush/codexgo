// Package write provides write-path artifact helpers for Codex memories.
//
// It is a faithful, drop-in-compatible Go port of the file-backed memory
// artifact helpers in the Rust crate codex-memories-write (codex 0.136.0):
// layout creation, raw_memories.md rebuilding, rollout-summary syncing, and the
// canonical rollout-summary file-stem generation. JSON, on-disk, and filename
// formats match the reference byte-for-byte.
//
// The heavier startup pipeline (Phase 1/Phase 2 model runs, job leasing,
// workspace diffing) is owned by orchestration code outside this package; this
// package focuses on the deterministic, format-critical artifact surface.
package write

import (
	"context"
	"path/filepath"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// Subdirectory and filename constants for the memory artifact layout. They
// mirror the Rust `artifacts` module.
const (
	extensionsSubdir       = "extensions"
	rolloutSummariesSubdir = "rollout_summaries"
	rawMemoriesFilename    = "raw_memories.md"
)

// MemoryRoot returns the canonical memories directory under codexHome, mirroring
// codex_memories_write::memory_root.
func MemoryRoot(codexHome abspath.AbsolutePathBuf) abspath.AbsolutePathBuf {
	return codexHome.Join("memories")
}

// RolloutSummariesDir returns the rollout_summaries directory under root,
// mirroring rollout_summaries_dir.
func RolloutSummariesDir(root string) string {
	return filepath.Join(root, rolloutSummariesSubdir)
}

// MemoryExtensionsRoot returns the extensions directory under root, mirroring
// memory_extensions_root.
func MemoryExtensionsRoot(root string) string {
	return filepath.Join(root, extensionsSubdir)
}

// RawMemoriesFile returns the raw_memories.md path under root, mirroring
// raw_memories_file.
func RawMemoriesFile(root string) string {
	return filepath.Join(root, rawMemoriesFilename)
}

// EnsureLayout creates the rollout_summaries directory (and parents) under root,
// mirroring ensure_layout. The context is accepted for cancellation parity with
// the async Rust signature.
func EnsureLayout(ctx context.Context, root string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return mkdirAll(RolloutSummariesDir(root))
}
