// Package state provides a SQLite-backed metadata mirror for rollout data,
// faithfully porting the codex-rs `codex-state` crate.
//
// The migration engine is wire-compatible with sqlx's `Migrator`: it stores
// applied migrations in a `_sqlx_migrations` table using the same schema,
// version/description parsing, and SHA-384 checksum algorithm so that databases
// created here can be opened by real codex (and vice versa).
package state

import (
	"context"
	"crypto/sha512"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var stateMigrationsFS embed.FS

//go:embed logs_migrations/*.sql
var logsMigrationsFS embed.FS

//go:embed goals_migrations/*.sql
var goalsMigrationsFS embed.FS

//go:embed memory_migrations/*.sql
var memoriesMigrationsFS embed.FS

// migration is a single embedded SQL migration parsed from an embedded file.
//
// It mirrors sqlx's notion of a migration: a version (the numeric filename
// prefix), a human description (the remainder of the filename with underscores
// replaced by spaces), the raw SQL, and the SHA-384 checksum of the SQL bytes.
type migration struct {
	version     int64
	description string
	sql         string
	checksum    []byte
}

// migrator is an ordered, immutable set of migrations for one database.
type migrator struct {
	migrations []migration
}

// newMigrator loads and parses the embedded migrations from dir within fsys.
//
// The returned migrator is sorted by ascending version. Parsing errors (bad
// filenames, duplicate versions) are returned eagerly so misconfiguration fails
// fast at startup rather than mid-migration.
func newMigrator(fsys embed.FS, dir string) (*migrator, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations dir %q: %w", dir, err)
	}
	migrations := make([]migration, 0, len(entries))
	seen := make(map[int64]struct{}, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version, description, err := parseMigrationFilename(entry.Name())
		if err != nil {
			return nil, err
		}
		if _, dup := seen[version]; dup {
			return nil, fmt.Errorf("duplicate migration version %d in %q", version, dir)
		}
		seen[version] = struct{}{}
		data, err := fsys.ReadFile(dir + "/" + entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", entry.Name(), err)
		}
		sum := sha512.Sum384(data)
		migrations = append(migrations, migration{
			version:     version,
			description: description,
			sql:         string(data),
			checksum:    sum[:],
		})
	}
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].version < migrations[j].version
	})
	return &migrator{migrations: migrations}, nil
}

// parseMigrationFilename splits a `<version>_<description>.sql` filename.
//
// The version is the numeric prefix before the first underscore; the
// description is the remainder with underscores converted to spaces, matching
// sqlx's `Migration::description` formatting.
func parseMigrationFilename(name string) (int64, string, error) {
	base := strings.TrimSuffix(name, ".sql")
	idx := strings.IndexByte(base, '_')
	if idx <= 0 {
		return 0, "", fmt.Errorf("invalid migration filename %q: expected <version>_<description>.sql", name)
	}
	version, err := strconv.ParseInt(base[:idx], 10, 64)
	if err != nil {
		return 0, "", fmt.Errorf("invalid migration version in %q: %w", name, err)
	}
	description := strings.ReplaceAll(base[idx+1:], "_", " ")
	return version, description, nil
}

const createSqlxMigrationsTable = `
CREATE TABLE IF NOT EXISTS _sqlx_migrations (
    version BIGINT PRIMARY KEY,
    description TEXT NOT NULL,
    installed_on TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    success BOOLEAN NOT NULL,
    checksum BLOB NOT NULL,
    execution_time BIGINT NOT NULL
);
`

// run applies all pending migrations to db.
//
// It tolerates a database that has already been migrated by a newer binary:
// applied versions that are not part of the embedded set are ignored (mirroring
// sqlx's `ignore_missing = true` runtime migrator). Known applied versions are
// still validated by checksum so divergent SQL is detected.
func (m *migrator) run(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, createSqlxMigrationsTable); err != nil {
		return fmt.Errorf("create _sqlx_migrations table: %w", err)
	}
	applied, err := m.fetchApplied(ctx, db)
	if err != nil {
		return err
	}
	for _, mig := range m.migrations {
		existing, ok := applied[mig.version]
		if ok {
			if !existing.success {
				return fmt.Errorf("migration %d previously failed", mig.version)
			}
			if !checksumsEqual(existing.checksum, mig.checksum) {
				return fmt.Errorf("migration %d was previously applied but has been modified", mig.version)
			}
			continue
		}
		if err := m.apply(ctx, db, mig); err != nil {
			return err
		}
	}
	return nil
}

type appliedMigration struct {
	success  bool
	checksum []byte
}

func (m *migrator) fetchApplied(ctx context.Context, db *sql.DB) (map[int64]appliedMigration, error) {
	rows, err := db.QueryContext(ctx, "SELECT version, success, checksum FROM _sqlx_migrations")
	if err != nil {
		return nil, fmt.Errorf("query applied migrations: %w", err)
	}
	defer rows.Close()
	applied := make(map[int64]appliedMigration)
	for rows.Next() {
		var (
			version  int64
			success  bool
			checksum []byte
		)
		if err := rows.Scan(&version, &success, &checksum); err != nil {
			return nil, fmt.Errorf("scan applied migration: %w", err)
		}
		applied[version] = appliedMigration{success: success, checksum: checksum}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate applied migrations: %w", err)
	}
	return applied, nil
}

// apply runs a single migration inside a transaction and records it.
//
// sqlx records execution_time in nanoseconds; the exact value is not
// load-bearing for compatibility (only version/description/success/checksum
// matter for the migration gate), so a deterministic placeholder is stored.
func (m *migrator) apply(ctx context.Context, db *sql.DB, mig migration) (err error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %d: %w", mig.version, err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if _, err = tx.ExecContext(ctx, mig.sql); err != nil {
		return fmt.Errorf("apply migration %d (%s): %w", mig.version, mig.description, err)
	}
	if _, err = tx.ExecContext(ctx,
		`INSERT INTO _sqlx_migrations (version, description, success, checksum, execution_time) VALUES (?, ?, ?, ?, ?)`,
		mig.version, mig.description, true, mig.checksum, int64(0),
	); err != nil {
		return fmt.Errorf("record migration %d: %w", mig.version, err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %d: %w", mig.version, err)
	}
	return nil
}

func checksumsEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// errNoMigrations indicates an embedded migration set was unexpectedly empty.
var errNoMigrations = errors.New("no embedded migrations found")
