package state

import "path/filepath"

// Environment variable and database filename constants, mirroring the Rust
// `codex-state` crate. These names are part of the on-disk contract shared with
// real codex and must not change.
const (
	// SQLiteHomeEnv overrides the SQLite state home directory.
	SQLiteHomeEnv = "CODEX_SQLITE_HOME"

	// LogsDBFilename is the logs database filename under the SQLite home.
	LogsDBFilename = "logs_2.sqlite"
	// GoalsDBFilename is the goals database filename under the SQLite home.
	GoalsDBFilename = "goals_1.sqlite"
	// MemoriesDBFilename is the memories database filename under the SQLite home.
	MemoriesDBFilename = "memories_1.sqlite"
	// StateDBFilename is the primary state database filename under the SQLite home.
	StateDBFilename = "state_5.sqlite"
)

// Metric name constants mirroring the Rust crate. They are exported so callers
// emitting telemetry can use the exact same metric identifiers as real codex.
const (
	// DBErrorMetric counts DB operation errors. Tags: [stage]
	DBErrorMetric = "codex.db.error"
	// DBMetricBackfill counts backfill outcomes. Tags: [status]
	DBMetricBackfill = "codex.db.backfill"
	// DBMetricBackfillDurationMS records backfill duration. Tags: [status]
	DBMetricBackfillDurationMS = "codex.db.backfill.duration_ms"
	// DBInitMetric counts SQLite init attempts. Tags: [status, phase, db, error]
	DBInitMetric = "codex.sqlite.init.count"
	// DBInitDurationMetric records SQLite init latency. Tags: [status, phase, db, error]
	DBInitDurationMetric = "codex.sqlite.init.duration_ms"
	// DBFallbackMetric counts rollout fallback attempts. Tags: [caller, reason]
	DBFallbackMetric = "codex.sqlite.fallback.count"
)

// StateDBFilenameFunc returns the state database filename.
func StateDBFilenameFunc() string { return StateDBFilename }

// StateDBPath returns the absolute path to the state database under codexHome.
func StateDBPath(codexHome string) string {
	return filepath.Join(codexHome, StateDBFilename)
}

// LogsDBFilenameFunc returns the logs database filename.
func LogsDBFilenameFunc() string { return LogsDBFilename }

// LogsDBPath returns the absolute path to the logs database under codexHome.
func LogsDBPath(codexHome string) string {
	return filepath.Join(codexHome, LogsDBFilename)
}

// GoalsDBFilenameFunc returns the goals database filename.
func GoalsDBFilenameFunc() string { return GoalsDBFilename }

// GoalsDBPath returns the absolute path to the goals database under codexHome.
func GoalsDBPath(codexHome string) string {
	return filepath.Join(codexHome, GoalsDBFilename)
}

// MemoriesDBFilenameFunc returns the memories database filename.
func MemoriesDBFilenameFunc() string { return MemoriesDBFilename }

// MemoriesDBPath returns the absolute path to the memories database under codexHome.
func MemoriesDBPath(codexHome string) string {
	return filepath.Join(codexHome, MemoriesDBFilename)
}

// RuntimeDBPath identifies one of the runtime databases by label and path.
type RuntimeDBPath struct {
	// Label is the human-readable database label.
	Label string
	// Path is the absolute database path.
	Path string
}

// RuntimeDBPaths returns the labeled paths of all four runtime databases under
// codexHome, in the same order as the Rust `runtime_db_paths`.
func RuntimeDBPaths(codexHome string) []RuntimeDBPath {
	return []RuntimeDBPath{
		{Label: "state DB", Path: StateDBPath(codexHome)},
		{Label: "log DB", Path: LogsDBPath(codexHome)},
		{Label: "goals DB", Path: GoalsDBPath(codexHome)},
		{Label: "memories DB", Path: MemoriesDBPath(codexHome)},
	}
}
