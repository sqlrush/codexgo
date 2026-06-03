package applypatch

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// updateChunksFromPatch parses a patch expected to contain exactly one update
// hunk and returns its chunks.
func updateChunksFromPatch(t *testing.T, patch string) []UpdateFileChunk {
	t.Helper()
	res, err := ParsePatch(patch)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(res.Hunks) != 1 || res.Hunks[0].Kind != HunkUpdateFile {
		t.Fatalf("expected a single UpdateFile hunk, got %#v", res.Hunks)
	}
	return res.Hunks[0].Chunks
}

func TestUnifiedDiff(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "multi.txt")
	mustWriteDiff(t, path, "foo\nbar\nbaz\nqux\n")
	patch := wrapPatchDiff(fmt.Sprintf("*** Update File: %s\n@@\n foo\n-bar\n+BAR\n@@\n baz\n-qux\n+QUX", path))

	chunks := updateChunksFromPatch(t, patch)
	got, err := UnifiedDiffFromChunks(path, chunks, OSFileSystem{})
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	wantDiff := "@@ -1,4 +1,4 @@\n foo\n-bar\n+BAR\n baz\n-qux\n+QUX\n"
	assertUpdate(t, got, wantDiff, "foo\nbar\nbaz\nqux\n", "foo\nBAR\nbaz\nQUX\n")
}

func TestUnifiedDiffFirstLineReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "first.txt")
	mustWriteDiff(t, path, "foo\nbar\nbaz\n")
	patch := wrapPatchDiff(fmt.Sprintf("*** Update File: %s\n@@\n-foo\n+FOO\n bar\n", path))

	chunks := updateChunksFromPatch(t, patch)
	got, err := UnifiedDiffFromChunks(path, chunks, OSFileSystem{})
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	wantDiff := "@@ -1,2 +1,2 @@\n-foo\n+FOO\n bar\n"
	assertUpdate(t, got, wantDiff, "foo\nbar\nbaz\n", "FOO\nbar\nbaz\n")
}

func TestUnifiedDiffLastLineReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "last.txt")
	mustWriteDiff(t, path, "foo\nbar\nbaz\n")
	patch := wrapPatchDiff(fmt.Sprintf("*** Update File: %s\n@@\n foo\n bar\n-baz\n+BAZ\n", path))

	chunks := updateChunksFromPatch(t, patch)
	got, err := UnifiedDiffFromChunks(path, chunks, OSFileSystem{})
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	wantDiff := "@@ -2,2 +2,2 @@\n bar\n-baz\n+BAZ\n"
	assertUpdate(t, got, wantDiff, "foo\nbar\nbaz\n", "foo\nbar\nBAZ\n")
}

func TestUnifiedDiffInsertAtEOF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "insert.txt")
	mustWriteDiff(t, path, "foo\nbar\nbaz\n")
	patch := wrapPatchDiff(fmt.Sprintf("*** Update File: %s\n@@\n+quux\n*** End of File\n", path))

	chunks := updateChunksFromPatch(t, patch)
	got, err := UnifiedDiffFromChunks(path, chunks, OSFileSystem{})
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	wantDiff := "@@ -3 +3,2 @@\n baz\n+quux\n"
	assertUpdate(t, got, wantDiff, "foo\nbar\nbaz\n", "foo\nbar\nbaz\nquux\n")
}

func TestUnifiedDiffInterleavedChanges(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "interleaved.txt")
	mustWriteDiff(t, path, "a\nb\nc\nd\ne\nf\n")
	body := fmt.Sprintf("*** Update File: %s\n"+
		"@@\n a\n-b\n+B\n"+
		"@@\n d\n-e\n+E\n"+
		"@@\n f\n+g\n"+
		"*** End of File", path)
	patch := wrapPatchDiff(body)

	chunks := updateChunksFromPatch(t, patch)
	got, err := UnifiedDiffFromChunks(path, chunks, OSFileSystem{})
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	wantDiff := "@@ -1,6 +1,7 @@\n a\n-b\n+B\n c\n d\n-e\n+E\n f\n+g\n"
	assertUpdate(t, got, wantDiff, "a\nb\nc\nd\ne\nf\n", "a\nB\nc\nd\nE\nf\ng\n")
}

// --- helpers ---

func mustWriteDiff(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func wrapPatchDiff(body string) string {
	return fmt.Sprintf("*** Begin Patch\n%s\n*** End Patch", body)
}

func assertUpdate(t *testing.T, got ApplyPatchFileUpdate, wantDiff, wantOriginal, wantContent string) {
	t.Helper()
	if got.UnifiedDiff != wantDiff {
		t.Fatalf("unified diff:\n got %q\nwant %q", got.UnifiedDiff, wantDiff)
	}
	if got.OriginalContent != wantOriginal {
		t.Fatalf("original content got %q, want %q", got.OriginalContent, wantOriginal)
	}
	if got.Content != wantContent {
		t.Fatalf("content got %q, want %q", got.Content, wantContent)
	}
}
