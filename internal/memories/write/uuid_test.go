package write

import (
	"testing"
	"time"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

func TestParseUUIDRejectsMalformed(t *testing.T) {
	bad := []string{
		"not-a-uuid",
		"",
		"0194f5a689ab7cde8123456789abcdef",     // no hyphens
		"0194f5a6-89ab-7cde-8123-456789abcdeg", // non-hex
		"0194f5a6-89ab-7cde-8123-456789abcde",  // short final group
		"0194f5a6-89ab-7cde-8123-456789abcdef-extra", // extra group
	}
	for _, s := range bad {
		if _, ok := parseUUID(s); ok {
			t.Errorf("parseUUID(%q) unexpectedly succeeded", s)
		}
	}
}

func TestUUIDVersion7Timestamp(t *testing.T) {
	u, ok := parseUUID("0194f5a6-89ab-7cde-8123-456789abcdef")
	if !ok {
		t.Fatal("expected valid uuid")
	}
	if u.version() != 7 {
		t.Fatalf("version = %d, want 7", u.version())
	}
	ts, ok := uuidTimestamp(u)
	if !ok {
		t.Fatal("expected v7 timestamp")
	}
	if got := ts.UTC().Format("2006-01-02T15-04-05"); got != "2025-02-11T15-35-19" {
		t.Fatalf("timestamp = %q, want 2025-02-11T15-35-19", got)
	}
}

func TestUUIDVersion4HasNoTimestamp(t *testing.T) {
	// Version nibble 4 at byte 6 high nibble.
	u, ok := parseUUID("0194f5a6-89ab-4cde-8123-456789abcdef")
	if !ok {
		t.Fatal("expected valid uuid")
	}
	if u.version() != 4 {
		t.Fatalf("version = %d, want 4", u.version())
	}
	if _, ok := uuidTimestamp(u); ok {
		t.Fatal("v4 uuid must not yield a timestamp")
	}
}

func TestUUIDVersion1Timestamp(t *testing.T) {
	// Canonical RFC 4122 v1 example timestamp.
	u, ok := parseUUID("00000000-0000-1000-8000-000000000000")
	if !ok {
		t.Fatal("expected valid uuid")
	}
	if u.version() != 1 {
		t.Fatalf("version = %d, want 1", u.version())
	}
	ts, ok := uuidTimestamp(u)
	if !ok {
		t.Fatal("expected v1 timestamp")
	}
	// All-zero gregorian ticks corresponds to the 1582-10-15 epoch.
	if got := ts.UTC().Year(); got != 1582 {
		t.Fatalf("v1 epoch year = %d, want 1582", got)
	}
}

func TestUUIDVersion6Timestamp(t *testing.T) {
	u, ok := parseUUID("1ec9414c-232a-6b00-b3c8-9e6bdeced846")
	if !ok {
		t.Fatal("expected valid uuid")
	}
	if u.version() != 6 {
		t.Fatalf("version = %d, want 6", u.version())
	}
	if _, ok := uuidTimestamp(u); !ok {
		t.Fatal("expected v6 timestamp")
	}
}

func TestPathHelpers(t *testing.T) {
	home := abspath.ResolvePathAgainstBase("/home/u/.codex", "/")
	if got := MemoryRoot(home).Path(); got != "/home/u/.codex/memories" {
		t.Fatalf("MemoryRoot = %q", got)
	}
	root := "/r/memories"
	if got := MemoryExtensionsRoot(root); got != "/r/memories/extensions" {
		t.Fatalf("MemoryExtensionsRoot = %q", got)
	}
	if got := RolloutSummariesDir(root); got != "/r/memories/rollout_summaries" {
		t.Fatalf("RolloutSummariesDir = %q", got)
	}
	if got := RawMemoriesFile(root); got != "/r/memories/raw_memories.md" {
		t.Fatalf("RawMemoriesFile = %q", got)
	}
}

func TestRetainedMemoriesTruncates(t *testing.T) {
	memories := make([]Stage1Output, 5)
	if got := len(retainedMemories(memories, 3)); got != 3 {
		t.Fatalf("retained = %d, want 3", got)
	}
	if got := len(retainedMemories(memories, 10)); got != 5 {
		t.Fatalf("retained = %d, want 5", got)
	}
}

func TestAutoSiFractionZero(t *testing.T) {
	if got := autoSiFraction(0); got != "" {
		t.Fatalf("autoSiFraction(0) = %q, want empty", got)
	}
}

func TestRFC3339UsesUTC(t *testing.T) {
	loc := time.FixedZone("UTC+5", 5*3600)
	ts := time.Date(2025, 1, 1, 5, 0, 0, 0, loc)
	if got := rfc3339(ts); got != "2025-01-01T00:00:00+00:00" {
		t.Fatalf("rfc3339 = %q, want 2025-01-01T00:00:00+00:00", got)
	}
}
