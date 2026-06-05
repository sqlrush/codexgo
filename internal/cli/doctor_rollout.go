package cli

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// rolloutStats summarizes the on-disk rollout files under a directory tree,
// mirroring the RolloutStats accumulator in doctor.rs.
type rolloutStats struct {
	// Files is the count of rollout files discovered.
	Files uint64
	// TotalBytes is the summed size of those files.
	TotalBytes uint64
	// Err records the first scan error encountered, or "" on success.
	Err string
}

// averageBytes returns TotalBytes/Files, or 0 when there are no files, mirroring
// RolloutStats::average_bytes.
func (s rolloutStats) averageBytes() uint64 {
	if s.Files == 0 {
		return 0
	}
	return s.TotalBytes / s.Files
}

// collectRolloutStats walks root and accumulates rollout-file counts and sizes,
// mirroring collect_rollout_stats in doctor.rs. A missing directory yields empty
// stats (not an error); the first I/O error short-circuits the walk and is
// recorded in Err.
func collectRolloutStats(root string) rolloutStats {
	var stats rolloutStats
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if path == root {
				// Treat a missing root directory as empty stats, matching the
				// NotFound short-circuit in collect_rollout_stats_inner.
				return filepath.SkipDir
			}
			stats.Err = walkErr.Error()
			return filepath.SkipAll
		}
		if d.IsDir() {
			return nil
		}
		if !isRolloutFile(path) {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			stats.Err = infoErr.Error()
			return filepath.SkipAll
		}
		stats.Files++
		if size := info.Size(); size > 0 {
			stats.TotalBytes += uint64(size)
		}
		return nil
	})
	if err != nil && stats.Err == "" {
		stats.Err = err.Error()
	}
	return stats
}

// isRolloutFile reports whether path names a rollout file: a ".jsonl" file whose
// base name starts with "rollout-". Mirrors is_rollout_file in doctor.rs.
func isRolloutFile(path string) bool {
	base := filepath.Base(path)
	return strings.HasSuffix(base, ".jsonl") && strings.HasPrefix(base, "rollout-")
}
