package write

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// defaultMaxRawMemories mirrors
// DEFAULT_MEMORIES_MAX_RAW_MEMORIES_FOR_CONSOLIDATION; the exact value only needs
// to exceed the small fixtures used here.
const defaultMaxRawMemories = 50

func TestSyncAndRebuildKeepsLatestMemoriesOnly(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "memory")
	if err := EnsureLayout(ctx, root); err != nil {
		t.Fatalf("ensure layout: %v", err)
	}

	keepID := "0194f5a6-89ab-7cde-8123-456789abcdef"
	dropID := "0194f5a6-89ab-7cde-8123-456789abcde0"
	keepStalePath := filepath.Join(RolloutSummariesDir(root), keepID+".md")
	dropStalePath := filepath.Join(RolloutSummariesDir(root), dropID+".md")
	if err := os.WriteFile(keepStalePath, []byte("keep"), 0o644); err != nil {
		t.Fatalf("write keep: %v", err)
	}
	if err := os.WriteFile(dropStalePath, []byte("drop"), 0o644); err != nil {
		t.Fatalf("write drop: %v", err)
	}

	memories := []Stage1Output{{
		ThreadID:        protocol.NewThreadID(keepID),
		SourceUpdatedAt: time.Unix(100, 0).UTC(),
		RawMemory:       "raw memory",
		RolloutSummary:  "short summary",
		RolloutPath:     "/tmp/rollout-100.jsonl",
		CWD:             "/tmp/workspace",
		GeneratedAt:     time.Unix(101, 0).UTC(),
	}}

	if err := SyncRolloutSummariesFromMemories(ctx, root, memories, defaultMaxRawMemories); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if err := RebuildRawMemoriesFileFromMemories(ctx, root, memories, defaultMaxRawMemories); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	if _, err := os.Stat(keepStalePath); !os.IsNotExist(err) {
		t.Fatalf("stale keep path should be pruned, stat err = %v", err)
	}
	if _, err := os.Stat(dropStalePath); !os.IsNotExist(err) {
		t.Fatalf("stale drop path should be pruned, stat err = %v", err)
	}

	entries, err := os.ReadDir(RolloutSummariesDir(root))
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	var files []string
	for _, e := range entries {
		files = append(files, e.Name())
	}
	sort.Strings(files)
	if len(files) != 1 {
		t.Fatalf("expected 1 rollout summary file, got %v", files)
	}
	canonicalFile := files[0]

	raw, err := os.ReadFile(RawMemoriesFile(root))
	if err != nil {
		t.Fatalf("read raw memories: %v", err)
	}
	rawStr := string(raw)
	for _, want := range []string{
		"raw memory",
		keepID,
		"cwd: /tmp/workspace",
		"rollout_path: /tmp/rollout-100.jsonl",
		"rollout_summary_file: " + canonicalFile,
	} {
		if !strings.Contains(rawStr, want) {
			t.Fatalf("raw_memories.md missing %q\n---\n%s", want, rawStr)
		}
	}
}

func TestRebuildRawMemoriesEmpty(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "memory")
	if err := RebuildRawMemoriesFileFromMemories(ctx, root, nil, defaultMaxRawMemories); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	raw, err := os.ReadFile(RawMemoriesFile(root))
	if err != nil {
		t.Fatalf("read raw memories: %v", err)
	}
	want := "# Raw Memories\n\nNo raw memories yet.\n"
	if string(raw) != want {
		t.Fatalf("raw = %q, want %q", string(raw), want)
	}
}

func TestWriteRolloutSummaryWithGitBranch(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "memory")
	if err := EnsureLayout(ctx, root); err != nil {
		t.Fatalf("ensure layout: %v", err)
	}
	branch := "main"
	memory := Stage1Output{
		ThreadID:        fixedThreadID(),
		SourceUpdatedAt: time.Unix(123, 0).UTC(),
		RawMemory:       "raw",
		RolloutSummary:  "the summary body",
		RolloutPath:     "/tmp/r.jsonl",
		CWD:             "/tmp/ws",
		GitBranch:       &branch,
		GeneratedAt:     time.Unix(124, 0).UTC(),
	}
	if err := writeRolloutSummaryForThread(root, &memory); err != nil {
		t.Fatalf("write: %v", err)
	}
	path := filepath.Join(RolloutSummariesDir(root), RolloutSummaryFileStem(&memory)+".md")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := "thread_id: 0194f5a6-89ab-7cde-8123-456789abcdef\n" +
		"updated_at: 1970-01-01T00:02:03+00:00\n" +
		"rollout_path: /tmp/r.jsonl\n" +
		"cwd: /tmp/ws\n" +
		"git_branch: main\n" +
		"\n" +
		"the summary body\n"
	if string(got) != want {
		t.Fatalf("summary = %q, want %q", string(got), want)
	}
}
