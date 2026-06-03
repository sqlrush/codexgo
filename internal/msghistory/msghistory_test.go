package msghistory

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeEntries writes the supplied entries to path, one JSON object per line, as
// the Rust tests do with `writeln!`.
func writeEntries(t *testing.T, path string, entries []HistoryEntry) {
	t.Helper()
	var b strings.Builder
	for _, entry := range entries {
		encoded, err := json.Marshal(entry)
		if err != nil {
			t.Fatalf("serialize history entry: %v", err)
		}
		b.Write(encoded)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("write history file: %v", err)
	}
}

func readEntries(t *testing.T, path string) []HistoryEntry {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read history file: %v", err)
	}
	var out []HistoryEntry
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		var entry HistoryEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("parse history entry %q: %v", line, err)
		}
		out = append(out, entry)
	}
	return out
}

func fileLen(t *testing.T, path string) uint64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %q: %v", path, err)
	}
	return uint64(info.Size())
}

// TestHistoryEntryJSONByteFormat verifies the on-disk byte format matches the
// Rust `HistoryEntry` serde encoding: {"session_id":...,"ts":...,"text":...}.
func TestHistoryEntryJSONByteFormat(t *testing.T) {
	entry := HistoryEntry{SessionID: "sess-1", Ts: 42, Text: "hello"}
	got, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"session_id":"sess-1","ts":42,"text":"hello"}`
	if string(got) != want {
		t.Fatalf("json = %s, want %s", got, want)
	}

	var round HistoryEntry
	if err := json.Unmarshal(got, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if round != entry {
		t.Fatalf("roundtrip = %+v, want %+v", round, entry)
	}
}

// TestLookupReadsHistoryEntries mirrors the Rust `lookup_reads_history_entries`.
func TestLookupReadsHistoryEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, HistoryFilename)
	entries := []HistoryEntry{
		{SessionID: "first-session", Ts: 1, Text: "first"},
		{SessionID: "second-session", Ts: 2, Text: "second"},
	}
	writeEntries(t, path, entries)

	logID, count := historyMetadataForFile(path)
	if count != len(entries) {
		t.Fatalf("count = %d, want %d", count, len(entries))
	}

	second, ok := lookupHistoryEntry(context.Background(), path, logID, 1)
	if !ok {
		t.Fatalf("expected to fetch second history entry")
	}
	if second != entries[1] {
		t.Fatalf("second entry = %+v, want %+v", second, entries[1])
	}
}

// TestLookupUsesStableLogIDAfterAppends mirrors the Rust
// `lookup_uses_stable_log_id_after_appends`: appending to the file keeps the
// same inode so an earlier-captured log id still resolves new offsets.
func TestLookupUsesStableLogIDAfterAppends(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, HistoryFilename)
	initial := HistoryEntry{SessionID: "first-session", Ts: 1, Text: "first"}
	appended := HistoryEntry{SessionID: "second-session", Ts: 2, Text: "second"}

	writeEntries(t, path, []HistoryEntry{initial})

	logID, count := historyMetadataForFile(path)
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open for append: %v", err)
	}
	encoded, _ := json.Marshal(appended)
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	fetched, ok := lookupHistoryEntry(context.Background(), path, logID, 1)
	if !ok {
		t.Fatalf("expected to look up appended entry")
	}
	if fetched != appended {
		t.Fatalf("fetched = %+v, want %+v", fetched, appended)
	}
}

// TestLookupOffsetOutOfRange verifies that a non-existent offset returns false.
func TestLookupOffsetOutOfRange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, HistoryFilename)
	writeEntries(t, path, []HistoryEntry{{SessionID: "s", Ts: 1, Text: "only"}})
	logID, _ := historyMetadataForFile(path)
	if _, ok := lookupHistoryEntry(context.Background(), path, logID, 5); ok {
		t.Fatalf("expected no entry at offset 5")
	}
}

// TestLookupRejectsMismatchedLogID verifies that a non-zero, mismatched log id
// returns false even when an entry exists at the offset.
func TestLookupRejectsMismatchedLogID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, HistoryFilename)
	writeEntries(t, path, []HistoryEntry{{SessionID: "s", Ts: 1, Text: "only"}})
	if _, ok := lookupHistoryEntry(context.Background(), path, 999999999, 0); ok {
		t.Fatalf("expected mismatched log id to reject lookup")
	}
}

// TestHistoryMetadataMissingFile verifies (0, 0) for a missing file.
func TestHistoryMetadataMissingFile(t *testing.T) {
	dir := t.TempDir()
	logID, count := historyMetadataForFile(filepath.Join(dir, "nope.jsonl"))
	if logID != 0 || count != 0 {
		t.Fatalf("metadata = (%d, %d), want (0, 0)", logID, count)
	}
}

// TestAppendEntryPersistenceNoneIsNoOp verifies that None persistence never
// creates the file.
func TestAppendEntryPersistenceNoneIsNoOp(t *testing.T) {
	dir := t.TempDir()
	config := NewHistoryConfig(dir, HistoryPersistenceNone, nil)
	if err := AppendEntry(context.Background(), "ignored", "conv", config); err != nil {
		t.Fatalf("append with None persistence: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, HistoryFilename)); !os.IsNotExist(err) {
		t.Fatalf("expected no history file, stat err = %v", err)
	}
}

// TestAppendEntryWritesAndCounts verifies a basic append writes a parseable
// entry and that HistoryMetadata counts it.
func TestAppendEntryWritesAndCounts(t *testing.T) {
	dir := t.TempDir()
	config := NewHistoryConfig(dir, HistoryPersistenceSaveAll, nil)
	if err := AppendEntry(context.Background(), "hello world", "conv-1", config); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := AppendEntry(context.Background(), "second", "conv-1", config); err != nil {
		t.Fatalf("append second: %v", err)
	}

	logID, count := HistoryMetadata(config)
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
	entry, ok := Lookup(context.Background(), logID, 0, config)
	if !ok {
		t.Fatalf("expected entry at offset 0")
	}
	if entry.SessionID != "conv-1" || entry.Text != "hello world" {
		t.Fatalf("entry = %+v", entry)
	}
}

// TestAppendEntryOwnerOnlyPermissions verifies the history file is created with
// 0o600 permissions, matching the Rust unix arm.
func TestAppendEntryOwnerOnlyPermissions(t *testing.T) {
	dir := t.TempDir()
	config := NewHistoryConfig(dir, HistoryPersistenceSaveAll, nil)
	if err := AppendEntry(context.Background(), "x", "conv", config); err != nil {
		t.Fatalf("append: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, HistoryFilename))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("perm = %o, want 600", got)
	}
}

// TestAppendEntryTrimsHistoryWhenBeyondMaxBytes mirrors the Rust
// `append_entry_trims_history_when_beyond_max_bytes`.
func TestAppendEntryTrimsHistoryWhenBeyondMaxBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, HistoryFilename)
	conv := "conversation-id"

	entryOne := strings.Repeat("a", 200)
	entryTwo := strings.Repeat("b", 200)

	config := NewHistoryConfig(dir, HistoryPersistenceSaveAll, nil)
	if err := AppendEntry(context.Background(), entryOne, conv, config); err != nil {
		t.Fatalf("append first: %v", err)
	}

	firstLen := fileLen(t, path)
	limitBytes := firstLen + 10

	config = NewHistoryConfig(dir, HistoryPersistenceSaveAll, &limitBytes)
	if err := AppendEntry(context.Background(), entryTwo, conv, config); err != nil {
		t.Fatalf("append second: %v", err)
	}

	entries := readEntries(t, path)
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1 (entry_one should be evicted)", len(entries))
	}
	if entries[0].Text != entryTwo {
		t.Fatalf("remaining entry text = %q, want entry_two", entries[0].Text)
	}
	if fileLen(t, path) > limitBytes {
		t.Fatalf("file len %d > limit %d", fileLen(t, path), limitBytes)
	}
}

// TestAppendEntryTrimsHistoryToSoftCap mirrors the Rust
// `append_entry_trims_history_to_soft_cap`: when the hard cap is exceeded,
// trimming targets the soft cap so the next write does not trim again.
func TestAppendEntryTrimsHistoryToSoftCap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, HistoryFilename)
	conv := "conversation-id"

	shortEntry := strings.Repeat("a", 200)
	longEntry := strings.Repeat("b", 400)

	config := NewHistoryConfig(dir, HistoryPersistenceSaveAll, nil)
	if err := AppendEntry(context.Background(), shortEntry, conv, config); err != nil {
		t.Fatalf("append short: %v", err)
	}
	shortEntryLen := fileLen(t, path)

	if err := AppendEntry(context.Background(), longEntry, conv, config); err != nil {
		t.Fatalf("append long: %v", err)
	}
	twoEntryLen := fileLen(t, path)

	if twoEntryLen <= shortEntryLen {
		t.Fatalf("two-entry len %d should exceed short-entry len %d", twoEntryLen, shortEntryLen)
	}
	longEntryLen := twoEntryLen - shortEntryLen

	maxBytes := (2 * longEntryLen) + (shortEntryLen / 2)
	config = NewHistoryConfig(dir, HistoryPersistenceSaveAll, &maxBytes)

	if err := AppendEntry(context.Background(), longEntry, conv, config); err != nil {
		t.Fatalf("append third: %v", err)
	}

	entries := readEntries(t, path)
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].Text != longEntry {
		t.Fatalf("remaining entry text mismatch")
	}

	prunedLen := fileLen(t, path)
	if prunedLen > maxBytes {
		t.Fatalf("pruned len %d > max %d", prunedLen, maxBytes)
	}

	softCap := uint64(math.Floor(float64(maxBytes) * historySoftCapRatio))
	lenWithoutFirst := 2 * longEntryLen
	if lenWithoutFirst > maxBytes {
		t.Fatalf("dropping only the first entry should satisfy the hard cap")
	}
	if lenWithoutFirst <= softCap {
		t.Fatalf("soft cap should require more aggressive trimming than the hard cap")
	}
	if prunedLen != longEntryLen {
		t.Fatalf("pruned len = %d, want %d (single long entry)", prunedLen, longEntryLen)
	}
}

// TestEnforceHistoryLimitZeroAndNil verifies that nil and zero limits are no-ops.
func TestEnforceHistoryLimitNoCap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, HistoryFilename)
	conv := "conv"

	zero := uint64(0)
	for _, tc := range []struct {
		name  string
		limit *uint64
	}{
		{"nil", nil},
		{"zero", &zero},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_ = os.Remove(path)
			config := NewHistoryConfig(dir, HistoryPersistenceSaveAll, tc.limit)
			for i := 0; i < 5; i++ {
				if err := AppendEntry(context.Background(), strings.Repeat("x", 100), conv, config); err != nil {
					t.Fatalf("append: %v", err)
				}
			}
			if got := len(readEntries(t, path)); got != 5 {
				t.Fatalf("entries = %d, want 5 (no cap)", got)
			}
		})
	}
}

// TestTrimTargetBytes is a unit table for the soft-cap arithmetic.
func TestTrimTargetBytes(t *testing.T) {
	tests := []struct {
		name           string
		maxBytes       uint64
		newestEntryLen uint64
		want           uint64
	}{
		{"soft cap above newest", 1000, 100, 800},
		{"newest above soft cap", 1000, 900, 900},
		{"clamp to one", 1, 0, 1},
		{"newest equals soft cap", 1000, 800, 800},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := trimTargetBytes(tc.maxBytes, tc.newestEntryLen); got != tc.want {
				t.Fatalf("trimTargetBytes(%d, %d) = %d, want %d", tc.maxBytes, tc.newestEntryLen, got, tc.want)
			}
		})
	}
}
