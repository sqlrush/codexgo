package read

import (
	"strconv"
	"strings"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// ParseMemoryCitation parses one or more raw citation blocks emitted by the
// model into a structured MemoryCitation. It mirrors parse_memory_citation and
// returns nil when no entries and no rollout ids were found.
func ParseMemoryCitation(citations []string) *protocol.MemoryCitation {
	var entries []protocol.MemoryCitationEntry
	var rolloutIDs []string
	seenRolloutIDs := make(map[string]struct{})

	for _, citation := range citations {
		if entriesBlock, ok := extractBlock(citation, "<citation_entries>", "</citation_entries>"); ok {
			for _, line := range strings.Split(entriesBlock, "\n") {
				if entry, ok := parseMemoryCitationEntry(line); ok {
					entries = append(entries, entry)
				}
			}
		}

		if idsBlock, ok := extractIDsBlock(citation); ok {
			for _, line := range strings.Split(idsBlock, "\n") {
				id := strings.TrimSpace(line)
				if id == "" {
					continue
				}
				if _, exists := seenRolloutIDs[id]; !exists {
					seenRolloutIDs[id] = struct{}{}
					rolloutIDs = append(rolloutIDs, id)
				}
			}
		}
	}

	if len(entries) == 0 && len(rolloutIDs) == 0 {
		return nil
	}
	return &protocol.MemoryCitation{
		Entries:    entries,
		RolloutIDs: rolloutIDs,
	}
}

// ThreadIDsFromMemoryCitation extracts the rollout ids that parse as valid UUIDs
// into ThreadIDs, mirroring thread_ids_from_memory_citation. Ids that do not
// parse as UUIDs are dropped.
func ThreadIDsFromMemoryCitation(citation *protocol.MemoryCitation) []protocol.ThreadID {
	if citation == nil {
		return nil
	}
	var ids []protocol.ThreadID
	for _, id := range citation.RolloutIDs {
		if isUUID(id) {
			ids = append(ids, protocol.NewThreadID(id))
		}
	}
	return ids
}

// parseMemoryCitationEntry parses a single citation entry line in the form
// `<file>:<line_start>-<line_end>|note=[<note>]`, mirroring
// parse_memory_citation_entry.
func parseMemoryCitationEntry(line string) (protocol.MemoryCitationEntry, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return protocol.MemoryCitationEntry{}, false
	}

	location, note, ok := rsplitOnce(line, "|note=[")
	if !ok {
		return protocol.MemoryCitationEntry{}, false
	}
	noteTrimmed, ok := strings.CutSuffix(note, "]")
	if !ok {
		return protocol.MemoryCitationEntry{}, false
	}
	noteTrimmed = strings.TrimSpace(noteTrimmed)

	path, lineRange, ok := rsplitOnce(location, ":")
	if !ok {
		return protocol.MemoryCitationEntry{}, false
	}
	lineStartStr, lineEndStr, ok := strings.Cut(lineRange, "-")
	if !ok {
		return protocol.MemoryCitationEntry{}, false
	}

	lineStart, err := strconv.ParseUint(strings.TrimSpace(lineStartStr), 10, 32)
	if err != nil {
		return protocol.MemoryCitationEntry{}, false
	}
	lineEnd, err := strconv.ParseUint(strings.TrimSpace(lineEndStr), 10, 32)
	if err != nil {
		return protocol.MemoryCitationEntry{}, false
	}

	return protocol.MemoryCitationEntry{
		Path:      strings.TrimSpace(path),
		LineStart: uint32(lineStart),
		LineEnd:   uint32(lineEnd),
		Note:      noteTrimmed,
	}, true
}

// extractBlock returns the text between open and close delimiters, mirroring
// extract_block. The ok flag is false when either delimiter is absent.
func extractBlock(text, open, close string) (string, bool) {
	_, rest, ok := strings.Cut(text, open)
	if !ok {
		return "", false
	}
	body, _, ok := strings.Cut(rest, close)
	if !ok {
		return "", false
	}
	return body, true
}

// extractIDsBlock prefers the <rollout_ids> block and falls back to the legacy
// <thread_ids> block, mirroring extract_ids_block.
func extractIDsBlock(text string) (string, bool) {
	if body, ok := extractBlock(text, "<rollout_ids>", "</rollout_ids>"); ok {
		return body, true
	}
	return extractBlock(text, "<thread_ids>", "</thread_ids>")
}

// rsplitOnce splits s on the last occurrence of sep, mirroring Rust's
// str::rsplit_once: it returns (before, after, true) with sep removed.
func rsplitOnce(s, sep string) (string, string, bool) {
	idx := strings.LastIndex(s, sep)
	if idx < 0 {
		return "", "", false
	}
	return s[:idx], s[idx+len(sep):], true
}
