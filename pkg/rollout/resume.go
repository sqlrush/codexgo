package rollout

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// ErrEmptySessionFile is returned when a rollout file contains no records.
var ErrEmptySessionFile = errors.New("empty session file")

// LoadRolloutItems reads and parses every rollout line in the file at path.
//
// It returns the parsed items (in file order), the canonical thread id (from the
// FIRST session_meta line encountered, if any), and the count of lines that
// failed to parse. Legacy `ghost_snapshot` response items are skipped, and
// legacy ghost-snapshot entries are removed from compaction replacement
// histories, mirroring the Rust `load_rollout_items`.
func LoadRolloutItems(path string) (items []RolloutItem, threadID *protocol.ThreadID, parseErrors int, err error) {
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		return nil, nil, 0, fmt.Errorf("read rollout file: %w", readErr)
	}
	if strings.TrimSpace(string(data)) == "" {
		return nil, nil, 0, ErrEmptySessionFile
	}

	items = make([]RolloutItem, 0)
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}

		var raw map[string]json.RawMessage
		if jsonErr := json.Unmarshal([]byte(line), &raw); jsonErr != nil {
			parseErrors++
			continue
		}
		skip, stripErr := stripLegacyGhostSnapshotRolloutLine(raw)
		if stripErr != nil {
			parseErrors++
			continue
		}
		if skip {
			continue
		}
		normalized, marshalErr := json.Marshal(raw)
		if marshalErr != nil {
			parseErrors++
			continue
		}

		var rolloutLine RolloutLine
		if parseErr := json.Unmarshal(normalized, &rolloutLine); parseErr != nil {
			parseErrors++
			continue
		}

		item := rolloutLine.Item
		if item.Kind == RolloutItemKindSessionMeta && item.SessionMeta != nil && threadID == nil {
			id := item.SessionMeta.Meta.ID
			threadID = &id
		}
		items = append(items, item)
	}

	return items, threadID, parseErrors, nil
}

// GetRolloutHistory loads a rollout file into an InitialHistory.
//
// Returns InitialHistory{Kind: New} when the file parses but contains no items.
// Mirrors the Rust `get_rollout_history`.
func GetRolloutHistory(path string) (InitialHistory, error) {
	items, threadID, _, err := LoadRolloutItems(path)
	if err != nil {
		return InitialHistory{}, err
	}
	if threadID == nil {
		return InitialHistory{}, fmt.Errorf("failed to parse thread ID from rollout file")
	}
	if len(items) == 0 {
		return NewInitialHistory(), nil
	}
	pathCopy := path
	return InitialHistory{
		Kind: InitialHistoryKindResumed,
		Resumed: &ResumedHistory{
			ConversationID: *threadID,
			History:        items,
			RolloutPath:    &pathCopy,
		},
	}, nil
}

// stripLegacyGhostSnapshotRolloutLine mutates the raw rollout-line map in place,
// matching the Rust pre-parse cleanup:
//   - response_item lines whose payload is a legacy ghost_snapshot are dropped
//     (returns true to skip the line).
//   - compacted lines have any legacy ghost_snapshot entries removed from
//     payload.replacement_history.
//
// It returns whether the whole line should be skipped.
func stripLegacyGhostSnapshotRolloutLine(raw map[string]json.RawMessage) (bool, error) {
	typeRaw, ok := raw["type"]
	if !ok {
		return false, nil
	}
	var typ string
	if err := json.Unmarshal(typeRaw, &typ); err != nil {
		return false, nil
	}

	switch typ {
	case string(RolloutItemKindResponseItem):
		payload, ok := raw["payload"]
		if !ok {
			return false, nil
		}
		return isLegacyGhostSnapshotResponseItem(payload), nil
	case string(RolloutItemKindCompacted):
		payloadRaw, ok := raw["payload"]
		if !ok {
			return false, nil
		}
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(payloadRaw, &payload); err != nil {
			return false, nil
		}
		historyRaw, ok := payload["replacement_history"]
		if !ok {
			return false, nil
		}
		var history []json.RawMessage
		if err := json.Unmarshal(historyRaw, &history); err != nil {
			return false, nil
		}
		filtered := make([]json.RawMessage, 0, len(history))
		for _, entry := range history {
			if !isLegacyGhostSnapshotResponseItem(entry) {
				filtered = append(filtered, entry)
			}
		}
		newHistory, err := json.Marshal(filtered)
		if err != nil {
			return false, fmt.Errorf("re-marshal replacement history: %w", err)
		}
		payload["replacement_history"] = newHistory
		newPayload, err := json.Marshal(payload)
		if err != nil {
			return false, fmt.Errorf("re-marshal compacted payload: %w", err)
		}
		raw["payload"] = newPayload
		return false, nil
	default:
		return false, nil
	}
}

// isLegacyGhostSnapshotResponseItem reports whether the JSON value is a legacy
// ghost_snapshot response item (`{"type":"ghost_snapshot", ...}`).
func isLegacyGhostSnapshotResponseItem(value json.RawMessage) bool {
	var obj struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(value, &obj); err != nil {
		return false
	}
	return obj.Type == "ghost_snapshot"
}
