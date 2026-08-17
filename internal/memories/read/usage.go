package read

import (
	"strings"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// MemoriesUsageKind classifies which memory artifact a read command touched. It
// mirrors the Rust MemoriesUsageKind enum.
type MemoriesUsageKind int

const (
	// MemoryMd identifies a read of memories/MEMORY.md.
	MemoryMd MemoriesUsageKind = iota
	// MemorySummary identifies a read of memories/memory_summary.md.
	MemorySummary
	// RawMemories identifies a read of memories/raw_memories.md.
	RawMemories
	// RolloutSummaries identifies a read under memories/rollout_summaries/.
	RolloutSummaries
	// Skills identifies a read under memories/skills/.
	Skills
)

// AsTag returns the metric tag string for the usage kind, mirroring
// MemoriesUsageKind::as_tag.
func (k MemoriesUsageKind) AsTag() string {
	switch k {
	case MemoryMd:
		return "memory_md"
	case MemorySummary:
		return "memory_summary"
	case RawMemories:
		return "raw_memories"
	case RolloutSummaries:
		return "rollout_summaries"
	case Skills:
		return "skills"
	default:
		return ""
	}
}

// MemoriesUsageKindsFromParsedCommands classifies the memory artifacts a parsed
// command sequence read. It mirrors memories_usage_kinds_from_command, but the
// up-front is_known_safe_command gate and the command->ParsedCommand parsing are
// injected by the caller (the codexgo shell-command parser is owned by another
// package). When knownSafe is false, the result is always empty, matching the
// Rust early return.
func MemoriesUsageKindsFromParsedCommands(commands []protocol.ParsedCommand, knownSafe bool) []MemoriesUsageKind {
	if !knownSafe {
		return nil
	}

	var kinds []MemoriesUsageKind
	for _, command := range commands {
		switch command.Type {
		case protocol.ParsedCommandRead:
			if kind, ok := getMemoryKind(command.ReadPath); ok {
				kinds = append(kinds, kind)
			}
		case protocol.ParsedCommandSearch:
			if command.OptPath != nil {
				if kind, ok := getMemoryKind(*command.OptPath); ok {
					kinds = append(kinds, kind)
				}
			}
		case protocol.ParsedCommandListFiles, protocol.ParsedCommandUnknown:
			// No memory usage classification for list/unknown commands.
		}
	}
	return kinds
}

// getMemoryKind classifies a single path, mirroring get_memory_kind.
func getMemoryKind(path string) (MemoriesUsageKind, bool) {
	switch {
	case strings.Contains(path, "memories/MEMORY.md"):
		return MemoryMd, true
	case strings.Contains(path, "memories/memory_summary.md"):
		return MemorySummary, true
	case strings.Contains(path, "memories/raw_memories.md"):
		return RawMemories, true
	case strings.Contains(path, "memories/rollout_summaries/"):
		return RolloutSummaries, true
	case strings.Contains(path, "memories/skills/"):
		return Skills, true
	default:
		return 0, false
	}
}
