package rollout

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/sqlrush/codexgo/internal/filesearch"
)

// headRecordLimit caps the number of records read when summarizing a rollout
// head. Mirrors the Rust `HEAD_RECORD_LIMIT`.
const headRecordLimit = 10

// rolloutFilenameTimestampLayout parses the filename timestamp component
// (`YYYY-MM-DDThh-mm-ss`).
const rolloutFilenameTimestampLayout = "2006-01-02T15-04-05"

// uuidPattern matches a canonical UUID (used to validate the trailing id in a
// rollout filename and id lookups).
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// ParsedRolloutFilename holds the timestamp and UUID parsed from a rollout
// filename.
type ParsedRolloutFilename struct {
	Timestamp time.Time
	UUID      string
}

// ParseTimestampUUIDFromFilename parses `rollout-YYYY-MM-DDThh-mm-ss-<uuid>.jsonl`.
//
// It scans from the right for a '-' such that the suffix parses as a UUID, then
// parses the preceding timestamp as UTC. It returns false when the name is not a
// valid rollout filename. Mirrors the Rust `parse_timestamp_uuid_from_filename`.
func ParseTimestampUUIDFromFilename(name string) (ParsedRolloutFilename, bool) {
	core, ok := strings.CutPrefix(name, "rollout-")
	if !ok {
		return ParsedRolloutFilename{}, false
	}
	core, ok = strings.CutSuffix(core, ".jsonl")
	if !ok {
		return ParsedRolloutFilename{}, false
	}

	// Scan from the right for a '-' whose suffix is a UUID.
	for i := len(core) - 1; i >= 0; i-- {
		if core[i] != '-' {
			continue
		}
		candidate := core[i+1:]
		if !uuidPattern.MatchString(candidate) {
			continue
		}
		tsStr := core[:i]
		ts, err := time.ParseInLocation(rolloutFilenameTimestampLayout, tsStr, time.UTC)
		if err != nil {
			continue
		}
		return ParsedRolloutFilename{Timestamp: ts, UUID: candidate}, true
	}
	return ParsedRolloutFilename{}, false
}

// RolloutDateParts extracts the `YYYY`, `MM`, `DD` directory components from a
// rollout filename. Mirrors the Rust `rollout_date_parts`.
func RolloutDateParts(fileName string) (year, month, day string, ok bool) {
	name, has := strings.CutPrefix(fileName, "rollout-")
	if !has {
		return "", "", "", false
	}
	if len(name) < 10 {
		return "", "", "", false
	}
	date := name[:10]
	// date is `YYYY-MM-DD`.
	if len(date) < 10 {
		return "", "", "", false
	}
	return date[0:4], date[5:7], date[8:10], true
}

// ReadHeadForSummary reads up to headRecordLimit records from the head of a
// rollout file, returning session-meta lines and response items as raw JSON
// values. Compacted, turn-context, and event-message lines are skipped. Mirrors
// the Rust `read_head_for_summary`.
func ReadHeadForSummary(path string) ([]json.RawMessage, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open rollout file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	head := make([]json.RawMessage, 0, headRecordLimit)
	for len(head) < headRecordLimit && scanner.Scan() {
		trimmed := strings.TrimSpace(scanner.Text())
		if trimmed == "" {
			continue
		}
		var line RolloutLine
		if err := json.Unmarshal([]byte(trimmed), &line); err != nil {
			continue
		}
		switch line.Item.Kind {
		case RolloutItemKindSessionMeta:
			if line.Item.SessionMeta == nil {
				continue
			}
			value, err := json.Marshal(line.Item.SessionMeta)
			if err != nil {
				continue
			}
			head = append(head, value)
		case RolloutItemKindResponseItem:
			if line.Item.ResponseItem == nil {
				continue
			}
			value, err := json.Marshal(line.Item.ResponseItem)
			if err != nil {
				continue
			}
			head = append(head, value)
		default:
			// Compacted / TurnContext / EventMsg are not part of the summary.
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan rollout file: %w", err)
	}
	return head, nil
}

// ReadSessionMetaLine reads the SessionMetaLine from the head of a rollout file.
// Mirrors the Rust `read_session_meta_line`.
func ReadSessionMetaLine(path string) (SessionMetaLine, error) {
	head, err := ReadHeadForSummary(path)
	if err != nil {
		return SessionMetaLine{}, err
	}
	if len(head) == 0 {
		return SessionMetaLine{}, fmt.Errorf("rollout at %s is empty", path)
	}
	var line SessionMetaLine
	if err := json.Unmarshal(head[0], &line); err != nil {
		return SessionMetaLine{}, fmt.Errorf("rollout at %s does not start with session metadata", path)
	}
	return line, nil
}

// FindThreadPathByIDStr locates an active thread rollout file by its UUID string
// by walking the sessions directory. Returns ("", nil) when not found or the id
// is invalid. This is the filesystem-only path (the Rust crate additionally
// consults a SQLite index, which is not part of this port).
func FindThreadPathByIDStr(codexHome, idStr string) (string, error) {
	return findThreadPathByIDStrInSubdir(codexHome, SessionsSubdir, idStr)
}

// FindArchivedThreadPathByIDStr locates an archived thread rollout file by its
// UUID string.
func FindArchivedThreadPathByIDStr(codexHome, idStr string) (string, error) {
	return findThreadPathByIDStrInSubdir(codexHome, ArchivedSessionsSubdir, idStr)
}

func findThreadPathByIDStrInSubdir(codexHome, subdir, idStr string) (string, error) {
	if !uuidPattern.MatchString(idStr) {
		return "", nil
	}
	root := filepath.Join(codexHome, subdir)
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("stat sessions root: %w", err)
	}

	options := filesearch.FileSearchOptions{
		Limit:            1,
		ComputeIndices:   false,
		RespectGitignore: false,
	}
	results, err := filesearch.Run(idStr, []string{root}, options, nil)
	if err != nil {
		return "", fmt.Errorf("file search failed: %w", err)
	}
	if len(results.Matches) == 0 {
		return "", nil
	}
	return results.Matches[0].FullPath(), nil
}
