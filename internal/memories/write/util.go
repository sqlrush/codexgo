package write

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// rfc3339 formats t the way chrono's DateTime::<Utc>::to_rfc3339 does: a fixed
// "+00:00" offset for UTC (never "Z") and subsecond digits emitted only when
// non-zero, padded to 3, 6, or 9 fractional digits (SecondsFormat::AutoSi).
//
// Matching this exactly keeps the on-disk `updated_at:` lines byte-for-byte
// identical to the Rust write path.
func rfc3339(t time.Time) string {
	t = t.UTC()
	base := t.Format("2006-01-02T15:04:05")
	nanos := t.Nanosecond()
	frac := autoSiFraction(nanos)
	return base + frac + "+00:00"
}

// autoSiFraction renders the subsecond fraction using chrono's AutoSi rule:
// empty when zero, otherwise the shortest of 3, 6, or 9 fractional digits that
// preserves all significant digits.
func autoSiFraction(nanos int) string {
	if nanos == 0 {
		return ""
	}
	if nanos%1_000_000 == 0 {
		return fmt.Sprintf(".%03d", nanos/1_000_000)
	}
	if nanos%1_000 == 0 {
		return fmt.Sprintf(".%06d", nanos/1_000)
	}
	return fmt.Sprintf(".%09d", nanos)
}

// writeFile writes content to path, truncating any existing file, mirroring
// tokio::fs::write.
func writeFile(path, content string) error {
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %q: %w", path, err)
	}
	return nil
}

// mkdirAll creates dir and any missing parents, mirroring create_dir_all.
func mkdirAll(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create dir %q: %w", dir, err)
	}
	return nil
}

// joinPath joins path elements, mirroring PathBuf::join.
func joinPath(elem ...string) string {
	return filepath.Join(elem...)
}
