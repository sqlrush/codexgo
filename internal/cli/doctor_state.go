package cli

import (
	"context"
	"fmt"
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

	rolloutStatsDetails(b, dctx.CodexHome)

	if integrityFailed {
		b.fail("state database integrity check failed").
			remedy("Back up CODEX_HOME, then remove or repair the affected SQLite database.")
	} else {
		b.ok("state paths and databases are inspectable")
	}
	return b.build()
}

// rolloutStatsDetails emits the active/archived rollout-file inventory rows for
// state.paths, mirroring rollout_stats_details in doctor.rs. Each label reports
// file count, total bytes, and average bytes (or a scan-failure note).
func rolloutStatsDetails(b *checkBuilder, codexHome string) {
	active := collectRolloutStats(filepath.Join(codexHome, rollout.SessionsSubdir))
	archived := collectRolloutStats(filepath.Join(codexHome, rollout.ArchivedSessionsSubdir))
	pushRolloutStatsDetail(b, "active rollout files", active)
	pushRolloutStatsDetail(b, "archived rollout files", archived)
}

// pushRolloutStatsDetail renders one rollout-stats row, mirroring
// push_rollout_stats_detail in doctor.rs.
func pushRolloutStatsDetail(b *checkBuilder, label string, stats rolloutStats) {
	if stats.Err != "" {
		b.detail(fmt.Sprintf("%s: scan failed (%s)", label, stats.Err))
		return
	}
	b.detail(fmt.Sprintf("%s: %d files, %d total bytes, %d average bytes",
		label, stats.Files, stats.TotalBytes, stats.averageBytes()))
}

// stateRolloutParityCheck compares the on-disk rollout-file inventory against the
// state DB thread inventory, mirroring state.rollout_db_parity in doctor.rs.
// codexgo has no rollout/thread state DB, so the comparison degrades to the
// state-DB-missing path: it reports the on-disk rollout counts and a "rollout DB
// rows: skipped (state DB missing)" row. The malformed-name and scan-cap rows are
// emitted with their zero/false defaults. See DEVIATIONS.md (doctor).
func stateRolloutParityCheck(dctx doctorContext) doctorCheck {
	b := newCheck("state.rollout_db_parity", "threads")
	if !dctx.Loaded {
		b.warn("skipped: configuration did not load")
		return b.build()
	}

	active := collectRolloutStats(filepath.Join(dctx.CodexHome, rollout.SessionsSubdir))
	archived := collectRolloutStats(filepath.Join(dctx.CodexHome, rollout.ArchivedSessionsSubdir))
	scanErrors := 0
	if active.Err != "" {
		scanErrors++
	}
	if archived.Err != "" {
		scanErrors++
	}

	b.detail(fmt.Sprintf("default model provider: %s", resolveModelProviderID(dctx.ModelProvider)))
	b.detail(fmt.Sprintf("rollout DB active files: %d", active.Files))
	b.detail(fmt.Sprintf("rollout DB archived files: %d", archived.Files))
	b.detail(fmt.Sprintf("rollout DB scan errors: %d", scanErrors))
	b.detail("rollout DB malformed file names: 0")
	b.detail("rollout DB scan cap reached: false")
	b.detail("rollout DB rows: skipped (state DB missing)")

	b.ok("no rollout/state DB inventory to compare")
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

	// Detail emission order mirrors background_server_check in doctor.rs: daemon
	// state dir, settings, pid file, update-loop pid file, control socket, status,
	// then mode. JSON keys are sorted by the marshaler.
	stateDir := filepath.Join(dctx.CodexHome, "app-server-daemon")
	b.detail(fmt.Sprintf("daemon state dir: %s", stateDir))
	fileDetail(b, "settings", filepath.Join(stateDir, "settings.json"))
	fileDetail(b, "pid file", filepath.Join(stateDir, "app-server.pid"))
	fileDetail(b, "update-loop pid file", filepath.Join(stateDir, "app-server-updater.pid"))

	controlSocket := filepath.Join(dctx.CodexHome, "app-server-control", "app-server-control.sock")
	b.detail(fmt.Sprintf("control socket: %s", controlSocket))

	// codexgo does not probe the live control socket: a missing socket file is the
	// not-running case (matching codex's SocketStatus::NotRunning).
	running := false
	if info, err := os.Stat(controlSocket); err == nil && info.Mode().IsRegular() {
		running = true
	}
	if running {
		b.detail("status: running")
	} else {
		b.detail("status: not running")
	}

	mode := "ephemeral"
	if info, err := os.Stat(filepath.Join(stateDir, "settings.json")); err == nil && info.Mode().IsRegular() {
		mode = "persistent"
	}
	b.detail(fmt.Sprintf("mode: %s", mode))

	if running {
		b.ok("background server is running")
	} else {
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
