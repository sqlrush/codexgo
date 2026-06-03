// Package read provides read-path helpers for Codex memories.
//
// It is a faithful, drop-in-compatible Go port of the Rust crate
// codex-memories-read (codex 0.136.0). The package owns memory citation
// parsing, telemetry classification for read access to the memory folder, and
// the memory-root path helper. It intentionally does not depend on the memory
// write pipeline.
package read

import "github.com/sqlrush/codexgo/internal/utils/abspath"

// MemoriesUsageMetric is the OpenTelemetry counter name emitted when a model
// reads from the memory folder. It mirrors MEMORIES_USAGE_METRIC.
const MemoriesUsageMetric = "codex.memories.usage"

// MemoryRoot returns the canonical memories directory under codexHome, mirroring
// codex_memories_read::memory_root.
func MemoryRoot(codexHome abspath.AbsolutePathBuf) abspath.AbsolutePathBuf {
	return codexHome.Join("memories")
}
