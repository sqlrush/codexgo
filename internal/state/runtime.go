package state

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

const sqliteDriverName = "sqlite"

// dbSpec describes one runtime database: its filename and embedded migrations.
type dbSpec struct {
	label    string
	filename string
	migrator func() (*migrator, error)
}

// StateRuntime owns the SQLite databases under a Codex home directory and
// mirrors rollout metadata into them, faithfully porting the Rust
// `StateRuntime`. It opens (and migrates) the state, logs, goals, and memories
// databases and is the preferred entrypoint for callers.
type StateRuntime struct {
	codexHome       string
	defaultProvider string

	pool      *sql.DB
	logsPool  *sql.DB
	goalsPool *sql.DB
	memPool   *sql.DB

	// threadUpdatedAtMillis is a process-local monotonic high-water mark used to
	// allocate unique, ordered updated_at values for thread-list cursors.
	threadUpdatedAtMillis atomic.Int64

	closeOnce sync.Once
}

// InitRuntime opens and migrates all runtime databases under codexHome, using
// defaultProvider as the fallback model provider, mirroring `StateRuntime::init`.
func InitRuntime(ctx context.Context, codexHome, defaultProvider string) (*StateRuntime, error) {
	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		return nil, fmt.Errorf("create codex home %q: %w", codexHome, err)
	}

	specs := runtimeDBSpecs()
	statePool, err := openSQLite(ctx, StateDBPath(codexHome), specs[0])
	if err != nil {
		return nil, err
	}
	logsPool, err := openSQLite(ctx, LogsDBPath(codexHome), specs[1])
	if err != nil {
		_ = statePool.Close()
		return nil, err
	}
	goalsPool, err := openSQLite(ctx, GoalsDBPath(codexHome), specs[2])
	if err != nil {
		_ = statePool.Close()
		_ = logsPool.Close()
		return nil, err
	}
	memPool, err := openSQLite(ctx, MemoriesDBPath(codexHome), specs[3])
	if err != nil {
		_ = statePool.Close()
		_ = logsPool.Close()
		_ = goalsPool.Close()
		return nil, err
	}

	if err := ensureBackfillStateRow(ctx, statePool); err != nil {
		closeAll(statePool, logsPool, goalsPool, memPool)
		return nil, err
	}

	watermark, err := maxThreadUpdatedAtMillis(ctx, statePool)
	if err != nil {
		closeAll(statePool, logsPool, goalsPool, memPool)
		return nil, err
	}

	rt := &StateRuntime{
		codexHome:       codexHome,
		defaultProvider: defaultProvider,
		pool:            statePool,
		logsPool:        logsPool,
		goalsPool:       goalsPool,
		memPool:         memPool,
	}
	rt.threadUpdatedAtMillis.Store(watermark)
	return rt, nil
}

func runtimeDBSpecs() [4]dbSpec {
	return [4]dbSpec{
		{label: "state DB", filename: StateDBFilename, migrator: func() (*migrator, error) {
			return newMigrator(stateMigrationsFS, "migrations")
		}},
		{label: "log DB", filename: LogsDBFilename, migrator: func() (*migrator, error) {
			return newMigrator(logsMigrationsFS, "logs_migrations")
		}},
		{label: "goals DB", filename: GoalsDBFilename, migrator: func() (*migrator, error) {
			return newMigrator(goalsMigrationsFS, "goals_migrations")
		}},
		{label: "memories DB", filename: MemoriesDBFilename, migrator: func() (*migrator, error) {
			return newMigrator(memoriesMigrationsFS, "memory_migrations")
		}},
	}
}

// CodexHome returns the configured Codex home directory.
func (r *StateRuntime) CodexHome() string { return r.codexHome }

// DefaultProvider returns the fallback model provider identifier.
func (r *StateRuntime) DefaultProvider() string { return r.defaultProvider }

// Close closes all open database pools. It is safe to call multiple times.
func (r *StateRuntime) Close() error {
	var err error
	r.closeOnce.Do(func() {
		for _, db := range []*sql.DB{r.pool, r.logsPool, r.goalsPool, r.memPool} {
			if db == nil {
				continue
			}
			if cerr := db.Close(); cerr != nil && err == nil {
				err = cerr
			}
		}
	})
	return err
}

// openSQLite opens a database with codex's connection settings (WAL journal,
// normal synchronous, 5s busy timeout, incremental auto-vacuum, foreign keys)
// and runs its embedded migrations.
func openSQLite(ctx context.Context, path string, spec dbSpec) (*sql.DB, error) {
	dsn := sqliteDSN(path, true)
	db, err := sql.Open(sqliteDriverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s at %q: %w", spec.label, path, err)
	}
	// modernc sqlite is safe for concurrent use but per-connection PRAGMAs must
	// apply to every connection; the _pragma DSN params handle that. Cap the pool
	// to mirror the Rust max_connections(5) bound.
	db.SetMaxOpenConns(5)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect %s at %q: %w", spec.label, path, err)
	}
	mig, err := spec.migrator()
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("load %s migrations: %w", spec.label, err)
	}
	if len(mig.migrations) == 0 {
		_ = db.Close()
		return nil, fmt.Errorf("%s: %w", spec.label, errNoMigrations)
	}
	if err := mig.run(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate %s at %q: %w", spec.label, path, err)
	}
	return db, nil
}

// sqliteDSN builds a modernc.org/sqlite DSN matching codex's connect options.
func sqliteDSN(path string, createIfMissing bool) string {
	q := url.Values{}
	// busy_timeout must be pushed first by the driver; supplying it via _pragma
	// applies it on every connection.
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "synchronous(NORMAL)")
	q.Add("_pragma", "auto_vacuum(INCREMENTAL)")
	q.Add("_pragma", "foreign_keys(ON)")
	if !createIfMissing {
		q.Set("mode", "ro")
	}
	return "file:" + path + "?" + q.Encode()
}

// ensureBackfillStateRow inserts the singleton backfill_state row if absent,
// mirroring the Rust `ensure_backfill_state_row_in_pool`.
func ensureBackfillStateRow(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
INSERT INTO backfill_state (id, status, last_watermark, last_success_at, updated_at)
VALUES (?, ?, NULL, NULL, ?)
ON CONFLICT(id) DO NOTHING
`, int64(1), BackfillStatusPending.String(), time.Now().UTC().Unix())
	if err != nil {
		return fmt.Errorf("ensure backfill_state row: %w", err)
	}
	return nil
}

func maxThreadUpdatedAtMillis(ctx context.Context, db *sql.DB) (int64, error) {
	var watermark sql.NullInt64
	row := db.QueryRowContext(ctx, "SELECT MAX(threads.updated_at_ms) FROM threads")
	if err := row.Scan(&watermark); err != nil {
		return 0, fmt.Errorf("query max updated_at_ms: %w", err)
	}
	if !watermark.Valid {
		return 0, nil
	}
	return watermark.Int64, nil
}

func closeAll(dbs ...*sql.DB) {
	for _, db := range dbs {
		if db != nil {
			_ = db.Close()
		}
	}
}

// SQLiteIntegrityCheck runs SQLite's built-in integrity check against an
// existing database file, mirroring the Rust `sqlite_integrity_check`. The
// returned slice is `["ok"]` for a healthy database.
func SQLiteIntegrityCheck(ctx context.Context, path string) ([]string, error) {
	db, err := sql.Open(sqliteDriverName, sqliteDSN(path, false))
	if err != nil {
		return nil, fmt.Errorf("open %q for integrity check: %w", path, err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	rows, err := db.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return nil, fmt.Errorf("run integrity check on %q: %w", path, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return nil, fmt.Errorf("scan integrity check row: %w", err)
		}
		out = append(out, line)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate integrity check rows: %w", err)
	}
	return out, nil
}

// allocateThreadUpdatedAt allocates a persisted updated_at value for thread-list
// cursor ordering, mirroring the Rust `allocate_thread_updated_at`. It keeps a
// process-local monotonic high-water mark so hot rollout writes get unique,
// ordered millisecond timestamps, while older backfill/repair timestamps pass
// through unchanged to preserve historical ordering.
func (r *StateRuntime) allocateThreadUpdatedAt(updatedAt time.Time) (time.Time, error) {
	candidate := datetimeToEpochMillis(updatedAt)
	var allocated int64
	for {
		current := r.threadUpdatedAtMillis.Load()
		if candidate > current {
			if r.threadUpdatedAtMillis.CompareAndSwap(current, candidate) {
				allocated = candidate
				break
			}
			continue
		}
		if saturatingAdd(candidate, 1000) <= current {
			allocated = candidate
			break
		}
		bumped := saturatingAdd(current, 1)
		if r.threadUpdatedAtMillis.CompareAndSwap(current, bumped) {
			allocated = bumped
			break
		}
	}
	return epochMillisToDatetime(allocated)
}

func saturatingAdd(a, b int64) int64 {
	const maxI64 = int64(^uint64(0) >> 1)
	const minI64 = -maxI64 - 1
	sum := a + b
	if b > 0 && sum < a {
		return maxI64
	}
	if b < 0 && sum > a {
		return minI64
	}
	return sum
}
