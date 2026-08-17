package write

import (
	"time"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// Stage1Output is the DB-backed stage-1 memory extraction row used to rebuild
// on-disk memory artifacts. It mirrors the fields of the Rust
// codex_state::Stage1Output that the artifact helpers consume. The codexgo
// internal/state package owns the SQLite schema (memories_1.sqlite,
// stage1_outputs table); this struct is the read model that storage helpers
// operate on so the helpers stay decoupled from the persistence layer.
type Stage1Output struct {
	// ThreadID identifies the source thread.
	ThreadID protocol.ThreadID
	// SourceUpdatedAt is the source rollout's last-updated timestamp.
	SourceUpdatedAt time.Time
	// RawMemory is the verbatim stage-1 raw memory text.
	RawMemory string
	// RolloutSummary is the human-readable rollout recap.
	RolloutSummary string
	// RolloutSlug is the optional slug used in the canonical filename.
	RolloutSlug *string
	// RolloutPath is the absolute path to the source rollout file.
	RolloutPath string
	// CWD is the working directory recorded for the thread.
	CWD string
	// GitBranch is the optional git branch recorded for the thread.
	GitBranch *string
	// GeneratedAt is when the stage-1 output was produced.
	GeneratedAt time.Time
}
