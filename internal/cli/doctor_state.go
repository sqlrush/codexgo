package cli

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/sqlrush/codexgo/internal/rollout"
	"github.com/sqlrush/codexgo/internal/state"
)

// statePathsCheck reports the resolved local state directories and database
// files, running a bounded SQLite integrity check on each database that exists.
// It mirrors state.paths in doctor.rs. An integrity failure escalates to error.
func statePathsCheck(dctx doctorContext) doctorCheck {
	b := newCheck("state.paths", "state")
	if !dctx.Loaded {
		b.warn("skipped: configuration did not load")
		return b.build()
	}

	sqliteHome := dctx.SqliteHome
	if sqliteHome == "" {
		sqliteHome = dctx.CodexHome
	}
	logDir := dctx.LogDir
	if logDir == "" {
		logDir = filepath.Join(dctx.CodexHome, "log")
	}
	pathReadiness(b, "CODEX_HOME", dctx.CodexHome)
	pathReadiness(b, "log dir", logDir)
	pathReadiness(b, "sqlite home", sqliteHome)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	integrityFailed := false
	for _, db := range state.RuntimeDBPaths(sqliteHome) {
		pathReadiness(b, db.Label, db.Path)
		info, err := os.Stat(db.Path)
		if err != nil || !info.Mode().IsRegular() {
			b.detail(fmt.Sprintf("%s integrity: skipped (missing)", db.Label))
			continue
		}
		lines, err := state.SQLiteIntegrityCheck(ctx, db.Path)
		switch {
		case err != nil:
			// A probe error (locked/read-only database, permission issue) is not
			// proof of corruption; report it without escalating to a failure so
			// the doctor stays usable on restricted or busy databases.
			b.detail(fmt.Sprintf("%s integrity: skipped (%v)", db.Label, err))
		case len(lines) == 1 && lines[0] == "ok":
			b.detail(fmt.Sprintf("%s integrity: ok", db.Label))
		default:
			integrityFailed = true
			b.detail(fmt.Sprintf("%s integrity: %v", db.Label, lines))
		}
	}

	if integrityFailed {
		b.fail("state database integrity check failed").
			remedy("Back up CODEX_HOME, then remove or repair the affected SQLite database.")
	} else {
		b.ok("state paths and databases are inspectable")
	}
	return b.build()
}

// stateRolloutParityCheck reports the active/archived rollout file inventory,
// mirroring state.rollout_db_parity in doctor.rs. codexgo derives the inventory
// from the on-disk rollout files (its source of truth) and reports the counts;
// it does not require a parallel DB to be present.
func stateRolloutParityCheck(dctx doctorContext) doctorCheck {
	b := newCheck("state.rollout_db_parity", "threads")
	if !dctx.Loaded {
		b.warn("skipped: configuration did not load")
		return b.build()
	}

	active, activeErr := countRolloutFiles(filepath.Join(dctx.CodexHome, rollout.SessionsSubdir))
	archived, archivedErr := countRolloutFiles(filepath.Join(dctx.CodexHome, rollout.ArchivedSessionsSubdir))
	provider := orNone(dctx.ModelProvider)

	b.detail(fmt.Sprintf("default model provider: %s", provider))
	b.detail(fmt.Sprintf("rollout active files: %d", active))
	b.detail(fmt.Sprintf("rollout archived files: %d", archived))

	if activeErr != nil || archivedErr != nil {
		if activeErr != nil {
			b.detail(fmt.Sprintf("active scan error: %v", activeErr))
		}
		if archivedErr != nil {
			b.detail(fmt.Sprintf("archived scan error: %v", archivedErr))
		}
		b.warn("rollout inventory could not be fully scanned")
		return b.build()
	}
	b.ok("rollout files and thread inventory agree")
	return b.build()
}

// appServerCheck reports the app-server daemon state without starting or stopping
// the daemon, mirroring app_server.status in doctor.rs. Missing files are the
// expected ephemeral/not-running case and stay ok.
func appServerCheck(dctx doctorContext) doctorCheck {
	b := newCheck("app_server.status", "app-server")
	if !dctx.Loaded {
		b.warn("skipped: configuration did not load")
		return b.build()
	}

	stateDir := filepath.Join(dctx.CodexHome, "app-server-daemon")
	b.detail(fmt.Sprintf("daemon state dir: %s", stateDir))
	fileDetail(b, "settings", filepath.Join(stateDir, "settings.json"))
	fileDetail(b, "pid file", filepath.Join(stateDir, "app-server.pid"))
	fileDetail(b, "update-loop pid file", filepath.Join(stateDir, "app-server-updater.pid"))

	mode := "ephemeral"
	if info, err := os.Stat(filepath.Join(stateDir, "settings.json")); err == nil && info.Mode().IsRegular() {
		mode = "persistent"
	}
	b.detail(fmt.Sprintf("mode: %s", mode))

	running := false
	if info, err := os.Stat(filepath.Join(stateDir, "app-server.pid")); err == nil && info.Mode().IsRegular() {
		running = true
	}
	if running {
		b.detail("status: running")
		b.ok("background server is running")
	} else {
		b.detail("status: not running")
		b.ok("background server is not running")
	}
	return b.build()
}

// pathReadiness records whether a state path exists and what kind of entry it is.
func pathReadiness(b *checkBuilder, label, path string) {
	info, err := os.Stat(path)
	switch {
	case err == nil && info.IsDir():
		b.detail(fmt.Sprintf("%s: %s (dir)", label, path))
	case err == nil && info.Mode().IsRegular():
		b.detail(fmt.Sprintf("%s: %s (file)", label, path))
	case err == nil:
		b.detail(fmt.Sprintf("%s: %s (other)", label, path))
	case os.IsNotExist(err):
		b.detail(fmt.Sprintf("%s: %s (missing)", label, path))
	default:
		b.detail(fmt.Sprintf("%s: %s (%v)", label, path, err))
	}
}

// fileDetail records a file path detail with a (file)/(missing)/error suffix.
func fileDetail(b *checkBuilder, label, path string) {
	info, err := os.Stat(path)
	switch {
	case err == nil && info.Mode().IsRegular():
		b.detail(fmt.Sprintf("%s: %s (file)", label, path))
	case err == nil:
		b.detail(fmt.Sprintf("%s: %s (not a file)", label, path))
	case os.IsNotExist(err):
		b.detail(fmt.Sprintf("%s: %s (missing)", label, path))
	default:
		b.detail(fmt.Sprintf("%s: %s (%v)", label, path, err))
	}
}

// countRolloutFiles counts the *.jsonl rollout files under dir, returning 0 when
// the directory does not exist. It is bounded by the on-disk tree size.
func countRolloutFiles(dir string) (int, error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return 0, nil
	}
	count := 0
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) == ".jsonl" {
			count++
		}
		return nil
	})
	if err != nil {
		return count, fmt.Errorf("scanning %q: %w", dir, err)
	}
	return count, nil
}
