package threadstore

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/rollout"
	"github.com/sqlrush/codexgo/internal/state"
)

// rolloutFilenameUUIDLen is the length of a canonical UUID with hyphens.
const rolloutFilenameUUIDLen = 36

// uuidPattern matches the canonical hyphenated UUID that terminates a rollout
// filename (`rollout-<timestamp>-<uuid>.jsonl`).
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// rolloutPathIsArchived reports whether path lives under the archived-sessions
// tree of codexHome, mirroring the Rust `rollout_path_is_archived`. It matches
// either a direct prefix or any `archived_sessions` path component so symlinked
// or relative paths are still classified correctly.
func rolloutPathIsArchived(codexHome, path string) bool {
	archivedRoot := filepath.Join(codexHome, rollout.ArchivedSessionsSubdir)
	if pathHasPrefix(path, archivedRoot) {
		return true
	}
	for _, component := range strings.Split(filepath.ToSlash(path), "/") {
		if component == rollout.ArchivedSessionsSubdir {
			return true
		}
	}
	return false
}

// pathHasPrefix reports whether path is prefix or lives under prefix, comparing
// cleaned absolute-style paths component-wise.
func pathHasPrefix(path, prefix string) bool {
	cleanPath := filepath.Clean(path)
	cleanPrefix := filepath.Clean(prefix)
	if cleanPath == cleanPrefix {
		return true
	}
	return strings.HasPrefix(cleanPath, cleanPrefix+string(filepath.Separator))
}

// threadIDFromRolloutPath extracts the thread id encoded in a rollout file name
// (`rollout-<timestamp>-<uuid>.jsonl`), mirroring the Rust
// `thread_id_from_rollout_path`. It returns the id and true when the trailing
// 36-character UUID component is present.
func threadIDFromRolloutPath(path string) (protocol.ThreadID, bool) {
	name := filepath.Base(path)
	stem, ok := strings.CutSuffix(name, ".jsonl")
	if !ok {
		return protocol.ThreadID{}, false
	}
	if len(stem) < rolloutFilenameUUIDLen+1 {
		return protocol.ThreadID{}, false
	}
	uuidStart := len(stem) - rolloutFilenameUUIDLen
	if stem[uuidStart-1] != '-' {
		return protocol.ThreadID{}, false
	}
	candidate := stem[uuidStart:]
	if !uuidPattern.MatchString(candidate) {
		return protocol.ThreadID{}, false
	}
	return protocol.NewThreadID(candidate), true
}

// distinctThreadMetadataTitle returns the metadata title only when it differs
// from the first user message, mirroring the Rust
// `distinct_thread_metadata_title`.
func distinctThreadMetadataTitle(metadata *state.ThreadMetadata) *string {
	title := strings.TrimSpace(metadata.Title)
	if title == "" {
		return nil
	}
	if metadata.FirstUserMessage != nil && strings.TrimSpace(*metadata.FirstUserMessage) == title {
		return nil
	}
	return &title
}

// setThreadNameFromTitle assigns a distinct title to a thread name, mirroring
// the Rust `set_thread_name_from_title`. It skips empty titles and titles that
// merely echo the preview.
func setThreadNameFromTitle(thread *StoredThread, title string) {
	trimmed := strings.TrimSpace(title)
	if trimmed == "" || strings.TrimSpace(thread.Preview) == trimmed {
		return
	}
	thread.Name = &title
}

// parseRFC3339 parses an optional RFC3339 timestamp into a UTC time.
func parseRFC3339(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, false
	}
	return parsed.UTC(), true
}

// fileModTime returns the modification time of path, falling back to fallback
// when it cannot be read.
func fileModTime(path string, fallback time.Time) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return fallback
	}
	return info.ModTime().UTC()
}

// parseSessionSource resolves a stored session-source string into a typed
// [rollout.SessionSource], falling back to Unknown, mirroring the Rust
// `parse_session_source`.
func parseSessionSource(source string) rollout.SessionSource {
	parsed, err := rollout.SessionSourceFromStartupArg(source)
	if err != nil {
		return rollout.SessionSource{Kind: rollout.SessionSourceKindUnknown}
	}
	return parsed
}

// parseApprovalMode resolves a stored approval-mode string into a typed
// [protocol.AskForApproval], falling back to on-request.
func parseApprovalMode(value string) protocol.AskForApproval {
	switch protocol.AskForApprovalKind(strings.Trim(value, `"`)) {
	case protocol.AskForApprovalUnlessTrusted:
		return protocol.AskForApproval{Kind: protocol.AskForApprovalUnlessTrusted}
	case protocol.AskForApprovalOnFailure:
		return protocol.AskForApproval{Kind: protocol.AskForApprovalOnFailure}
	case protocol.AskForApprovalOnRequest:
		return protocol.AskForApproval{Kind: protocol.AskForApprovalOnRequest}
	case protocol.AskForApprovalGranular:
		return protocol.AskForApproval{Kind: protocol.AskForApprovalGranular}
	case protocol.AskForApprovalNever:
		return protocol.AskForApproval{Kind: protocol.AskForApprovalNever}
	default:
		return onRequestApproval()
	}
}
