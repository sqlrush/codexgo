package cloud

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// attemptStatusFromStr maps a backend turn status to an AttemptStatus. It
// mirrors the Rust `attempt_status_from_str` (default Pending).
func attemptStatusFromStr(raw string) AttemptStatus {
	switch raw {
	case "failed":
		return AttemptStatusFailed
	case "completed":
		return AttemptStatusCompleted
	case "in_progress":
		return AttemptStatusInProgress
	case "pending":
		return AttemptStatusPending
	default:
		return AttemptStatusPending
	}
}

// parseTimestampValue converts a JSON number (seconds, fractional) into a UTC
// time. It mirrors the Rust `parse_timestamp_value`.
func parseTimestampValue(raw json.RawMessage) *time.Time {
	if len(raw) == 0 {
		return nil
	}
	var ts float64
	if json.Unmarshal(raw, &ts) != nil {
		return nil
	}
	t := timeFromFloatSecs(ts)
	return &t
}

// parseUpdatedAt converts an optional seconds float into a UTC time, falling
// back to now. It mirrors the Rust `parse_updated_at`.
func parseUpdatedAt(ts *float64) time.Time {
	if ts != nil {
		return timeFromFloatSecs(*ts)
	}
	return time.Now().UTC()
}

func timeFromFloatSecs(ts float64) time.Time {
	secs := int64(ts)
	if secs < 0 {
		secs = 0
	}
	nanos := int64((ts - float64(int64(ts))) * 1_000_000_000.0)
	return time.Unix(secs, nanos).UTC()
}

// diffSummaryFromDiff computes a DiffSummary by scanning a unified diff. It
// mirrors the Rust `diff_summary_from_diff`.
func diffSummaryFromDiff(diff string) DiffSummary {
	filesChanged := 0
	linesAdded := 0
	linesRemoved := 0
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			filesChanged++
			continue
		}
		if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") || strings.HasPrefix(line, "@@") {
			continue
		}
		if len(line) > 0 {
			switch line[0] {
			case '+':
				linesAdded++
			case '-':
				linesRemoved++
			}
		}
	}
	if filesChanged == 0 && strings.TrimSpace(diff) != "" {
		filesChanged = 1
	}
	return DiffSummary{
		FilesChanged: filesChanged,
		LinesAdded:   linesAdded,
		LinesRemoved: linesRemoved,
	}
}

// statusDisplay is a decoded task_status_display object.
type statusDisplay map[string]json.RawMessage

// objectField decodes a nested object field of a status display.
func (s statusDisplay) objectField(key string) map[string]json.RawMessage {
	raw, ok := s[key]
	if !ok {
		return nil
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return nil
	}
	return obj
}

// mapStatus maps a status display into a TaskStatus. It mirrors the Rust
// `map_status`.
func mapStatus(s statusDisplay) TaskStatus {
	if s != nil {
		if turn := s.objectField("latest_turn_status_display"); turn != nil {
			if raw, ok := turn["turn_status"]; ok {
				var str string
				if json.Unmarshal(raw, &str) == nil {
					switch str {
					case "failed":
						return TaskStatusError
					case "completed":
						return TaskStatusReady
					case "in_progress":
						return TaskStatusPending
					case "pending":
						return TaskStatusPending
					case "cancelled":
						return TaskStatusError
					default:
						return TaskStatusPending
					}
				}
			}
		}
		if raw, ok := s["state"]; ok {
			var state string
			if json.Unmarshal(raw, &state) == nil {
				switch state {
				case "pending":
					return TaskStatusPending
				case "ready":
					return TaskStatusReady
				case "applied":
					return TaskStatusApplied
				case "error":
					return TaskStatusError
				default:
					return TaskStatusPending
				}
			}
		}
	}
	return TaskStatusPending
}

// envLabelFromStatusDisplay reads the environment_label, mirroring the Rust
// `env_label_from_status_display`.
func envLabelFromStatusDisplay(s statusDisplay) *string {
	if s == nil {
		return nil
	}
	raw, ok := s["environment_label"]
	if !ok {
		return nil
	}
	var label string
	if json.Unmarshal(raw, &label) != nil {
		return nil
	}
	return &label
}

// diffSummaryFromStatusDisplay reads diff stats, mirroring the Rust
// `diff_summary_from_status_display`.
func diffSummaryFromStatusDisplay(s statusDisplay) DiffSummary {
	var out DiffSummary
	if s == nil {
		return out
	}
	latest := s.objectField("latest_turn_status_display")
	if latest == nil {
		return out
	}
	var ds map[string]json.RawMessage
	if raw, ok := latest["diff_stats"]; ok {
		_ = json.Unmarshal(raw, &ds)
	}
	if ds == nil {
		return out
	}
	out.FilesChanged = nonNegInt(ds, "files_modified")
	out.LinesAdded = nonNegInt(ds, "lines_added")
	out.LinesRemoved = nonNegInt(ds, "lines_removed")
	return out
}

func nonNegInt(m map[string]json.RawMessage, key string) int {
	raw, ok := m[key]
	if !ok {
		return 0
	}
	var n int64
	if json.Unmarshal(raw, &n) != nil {
		return 0
	}
	if n < 0 {
		return 0
	}
	return int(n)
}

// latestTurnTimestamp reads updated_at/created_at, mirroring the Rust
// `latest_turn_timestamp`.
func latestTurnTimestamp(s statusDisplay) *float64 {
	if s == nil {
		return nil
	}
	latest := s.objectField("latest_turn_status_display")
	if latest == nil {
		return nil
	}
	for _, key := range []string{"updated_at", "created_at"} {
		if raw, ok := latest[key]; ok {
			var v float64
			if json.Unmarshal(raw, &v) == nil {
				return &v
			}
		}
	}
	return nil
}

// attemptTotalFromStatusDisplay reads the sibling-turn count, mirroring the Rust
// `attempt_total_from_status_display`.
func attemptTotalFromStatusDisplay(s statusDisplay) *int {
	if s == nil {
		return nil
	}
	latest := s.objectField("latest_turn_status_display")
	if latest == nil {
		return nil
	}
	raw, ok := latest["sibling_turn_ids"]
	if !ok {
		return nil
	}
	var siblings []json.RawMessage
	if json.Unmarshal(raw, &siblings) != nil {
		return nil
	}
	total := len(siblings) + 1
	return &total
}

// isUnifiedDiff reports whether the string looks like a unified git diff. It
// mirrors the Rust `is_unified_diff`.
func isUnifiedDiff(diff string) bool {
	t := strings.TrimLeft(diff, " \t\r\n")
	if strings.HasPrefix(t, "diff --git ") {
		return true
	}
	hasDashHeaders := strings.Contains(diff, "\n--- ") && strings.Contains(diff, "\n+++ ")
	hasHunk := strings.Contains(diff, "\n@@ ") || strings.HasPrefix(diff, "@@ ")
	return hasDashHeaders && hasHunk
}

// compareAttempts orders attempts by placement, then created_at, then turn id.
// It mirrors the Rust `compare_attempts`.
func compareAttempts(a, b TurnAttempt) bool {
	switch {
	case a.AttemptPlacement != nil && b.AttemptPlacement != nil:
		if *a.AttemptPlacement != *b.AttemptPlacement {
			return *a.AttemptPlacement < *b.AttemptPlacement
		}
	case a.AttemptPlacement != nil && b.AttemptPlacement == nil:
		return true
	case a.AttemptPlacement == nil && b.AttemptPlacement != nil:
		return false
	default:
		switch {
		case a.CreatedAt != nil && b.CreatedAt != nil:
			if !a.CreatedAt.Equal(*b.CreatedAt) {
				return a.CreatedAt.Before(*b.CreatedAt)
			}
		case a.CreatedAt != nil && b.CreatedAt == nil:
			return true
		case a.CreatedAt == nil && b.CreatedAt != nil:
			return false
		}
	}
	return a.TurnID < b.TurnID
}

// sortAttempts sorts attempts in place using compareAttempts.
func sortAttempts(attempts []TurnAttempt) {
	sort.SliceStable(attempts, func(i, j int) bool {
		return compareAttempts(attempts[i], attempts[j])
	})
}
