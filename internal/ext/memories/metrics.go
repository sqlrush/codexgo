package memories

import "strings"

// MemoriesToolCallMetric is the counter name emitted per memory tool call,
// mirroring MEMORIES_TOOL_CALL_METRIC.
const MemoriesToolCallMetric = "codex.memories.tool.call"

// ScopeFromPath classifies a memory path into a metric scope tag, mirroring
// scope_from_path.
func ScopeFromPath(path string) string {
	path = strings.Trim(path, "/")
	path = strings.TrimPrefix(path, "./")

	switch {
	case path == "":
		return "root"
	case path == "MEMORY.md":
		return "memory_md"
	case path == "memory_summary.md":
		return "memory_summary"
	case path == "raw_memories.md":
		return "raw_memories"
	case path == "rollout_summaries" || strings.HasPrefix(path, "rollout_summaries/"):
		return "rollout_summaries"
	case path == "skills" || strings.HasPrefix(path, "skills/"):
		return "skills"
	case path == "extensions/ad_hoc/notes" || strings.HasPrefix(path, "extensions/ad_hoc/notes/"):
		return "ad_hoc_notes"
	default:
		return "other"
	}
}

// ScopeFromOptionalPath classifies an optional path, returning the supplied
// default when path is nil, mirroring scope_from_optional_path.
func ScopeFromOptionalPath(path *string, def string) string {
	if path == nil {
		return def
	}
	return ScopeFromPath(*path)
}

// TruncatedTag renders the optional truncated flag as a metric tag, mirroring
// truncated_tag.
func TruncatedTag(truncated *bool) string {
	switch {
	case truncated == nil:
		return "unknown"
	case *truncated:
		return "true"
	default:
		return "false"
	}
}

// StatusTag renders a success flag as a metric tag, mirroring status_tag.
func StatusTag(success bool) string {
	if success {
		return "succeeded"
	}
	return "failed"
}
