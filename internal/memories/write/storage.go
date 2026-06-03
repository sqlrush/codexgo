package write

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// RebuildRawMemoriesFileFromMemories rebuilds raw_memories.md from DB-backed
// stage-1 outputs, mirroring rebuild_raw_memories_file_from_memories.
func RebuildRawMemoriesFileFromMemories(
	ctx context.Context,
	root string,
	memories []Stage1Output,
	maxRawMemoriesForConsolidation int,
) error {
	if err := EnsureLayout(ctx, root); err != nil {
		return fmt.Errorf("ensure layout: %w", err)
	}
	return rebuildRawMemoriesFile(root, memories, maxRawMemoriesForConsolidation)
}

// SyncRolloutSummariesFromMemories syncs canonical rollout-summary files from
// DB-backed stage-1 output rows, mirroring sync_rollout_summaries_from_memories.
func SyncRolloutSummariesFromMemories(
	ctx context.Context,
	root string,
	memories []Stage1Output,
	maxRawMemoriesForConsolidation int,
) error {
	if err := EnsureLayout(ctx, root); err != nil {
		return fmt.Errorf("ensure layout: %w", err)
	}

	retained := retainedMemories(memories, maxRawMemoriesForConsolidation)
	keep := make(map[string]struct{}, len(retained))
	for i := range retained {
		keep[RolloutSummaryFileStem(&retained[i])] = struct{}{}
	}
	if err := pruneRolloutSummaries(root, keep); err != nil {
		return err
	}

	for i := range retained {
		if err := writeRolloutSummaryForThread(root, &retained[i]); err != nil {
			return err
		}
	}
	return nil
}

func rebuildRawMemoriesFile(root string, memories []Stage1Output, maxRawMemoriesForConsolidation int) error {
	retained := retainedMemories(memories, maxRawMemoriesForConsolidation)
	var body strings.Builder
	body.WriteString("# Raw Memories\n\n")

	if len(retained) == 0 {
		body.WriteString("No raw memories yet.\n")
		return writeFile(RawMemoriesFile(root), body.String())
	}

	body.WriteString("Merged stage-1 raw memories (stable ascending thread-id order):\n\n")
	for i := range retained {
		memory := &retained[i]
		fmt.Fprintf(&body, "## Thread `%s`\n", memory.ThreadID.String())
		fmt.Fprintf(&body, "updated_at: %s\n", rfc3339(memory.SourceUpdatedAt))
		fmt.Fprintf(&body, "cwd: %s\n", memory.CWD)
		fmt.Fprintf(&body, "rollout_path: %s\n", memory.RolloutPath)
		rolloutSummaryFile := fmt.Sprintf("%s.md", RolloutSummaryFileStem(memory))
		fmt.Fprintf(&body, "rollout_summary_file: %s\n", rolloutSummaryFile)
		body.WriteString("\n")
		body.WriteString(strings.TrimSpace(memory.RawMemory))
		body.WriteString("\n\n")
	}

	return writeFile(RawMemoriesFile(root), body.String())
}

func pruneRolloutSummaries(root string, keep map[string]struct{}) error {
	dirPath := RolloutSummariesDir(root)
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read rollout summaries dir: %w", err)
	}

	for _, entry := range entries {
		name := entry.Name()
		stem, ok := strings.CutSuffix(name, ".md")
		if !ok {
			continue
		}
		if _, kept := keep[stem]; kept {
			continue
		}
		path := joinPath(dirPath, name)
		if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
			// Match the Rust warn-and-continue behavior: a failed prune of one
			// stale summary must not abort the whole sync.
			continue
		}
	}
	return nil
}

func writeRolloutSummaryForThread(root string, memory *Stage1Output) error {
	fileStem := RolloutSummaryFileStem(memory)
	path := joinPath(RolloutSummariesDir(root), fmt.Sprintf("%s.md", fileStem))

	var body strings.Builder
	fmt.Fprintf(&body, "thread_id: %s\n", memory.ThreadID.String())
	fmt.Fprintf(&body, "updated_at: %s\n", rfc3339(memory.SourceUpdatedAt))
	fmt.Fprintf(&body, "rollout_path: %s\n", memory.RolloutPath)
	fmt.Fprintf(&body, "cwd: %s\n", memory.CWD)
	if memory.GitBranch != nil {
		fmt.Fprintf(&body, "git_branch: %s\n", *memory.GitBranch)
	}
	body.WriteString("\n")
	body.WriteString(memory.RolloutSummary)
	body.WriteString("\n")

	return writeFile(path, body.String())
}

// retainedMemories returns the prefix of memories kept for consolidation,
// mirroring retained_memories.
func retainedMemories(memories []Stage1Output, maxRawMemoriesForConsolidation int) []Stage1Output {
	limit := len(memories)
	if maxRawMemoriesForConsolidation < limit {
		limit = maxRawMemoriesForConsolidation
	}
	return memories[:limit]
}
