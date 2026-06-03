package rollout

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

const testThreadID = "5973b6c0-94b8-487b-a530-2aeb6098ae0e"

func writeLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func metaLine(ts string) string {
	return `{"timestamp":"` + ts + `","type":"session_meta","payload":{"id":"` + testThreadID + `","timestamp":"` + ts + `","cwd":".","originator":"test_originator","cli_version":"test_version","source":"cli","model_provider":"test-provider"}}`
}

func TestLoadRolloutItemsSkipsLegacyGhostSnapshotLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	ts := "2025-01-03T12:00:00Z"
	writeLines(t, path,
		metaLine(ts),
		`{"timestamp":"`+ts+`","type":"response_item","payload":{"type":"ghost_snapshot","ghost_commit":{"id":"deadbeef"}}}`,
		`{"timestamp":"`+ts+`","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}}`,
	)

	items, threadID, parseErrors, err := LoadRolloutItems(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if threadID == nil || threadID.String() != testThreadID {
		t.Fatalf("thread id = %v", threadID)
	}
	if parseErrors != 0 {
		t.Fatalf("parse errors = %d", parseErrors)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].Kind != RolloutItemKindSessionMeta {
		t.Fatalf("item[0] should be session_meta, got %q", items[0].Kind)
	}
	if items[1].Kind != RolloutItemKindResponseItem ||
		items[1].ResponseItem.Type != protocol.ResponseItemKindMessage {
		t.Fatalf("item[1] should be a message response item")
	}
}

func TestLoadRolloutItemsFiltersGhostSnapshotsFromCompactionHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	ts := "2025-01-03T12:00:00Z"
	writeLines(t, path,
		metaLine(ts),
		`{"timestamp":"`+ts+`","type":"compacted","payload":{"message":"summary","replacement_history":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"kept"}]},{"type":"ghost_snapshot","ghost_commit":{"id":"deadbeef"}}]}}`,
	)

	items, _, parseErrors, err := LoadRolloutItems(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if parseErrors != 0 {
		t.Fatalf("parse errors = %d", parseErrors)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	compacted := items[1].Compacted
	if compacted == nil || compacted.ReplacementHistory == nil {
		t.Fatalf("expected compacted item with replacement history")
	}
	history := *compacted.ReplacementHistory
	if len(history) != 1 {
		t.Fatalf("expected 1 replacement item after filtering, got %d", len(history))
	}
	if history[0].Type != protocol.ResponseItemKindMessage {
		t.Fatalf("remaining item should be a message")
	}
}

func TestLoadRolloutItemsEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	if err := os.WriteFile(path, []byte("   \n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, _, _, err := LoadRolloutItems(path)
	if err == nil {
		t.Fatalf("expected error for empty session file")
	}
}

func TestLoadRolloutItemsCountsParseErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	ts := "2025-01-03T12:00:00Z"
	writeLines(t, path,
		metaLine(ts),
		`{not valid json`,
		`{"timestamp":"`+ts+`","type":"response_item","payload":{"type":"message","role":"user","content":[]}}`,
	)
	items, _, parseErrors, err := LoadRolloutItems(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if parseErrors != 1 {
		t.Fatalf("expected 1 parse error, got %d", parseErrors)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
}

func TestGetRolloutHistoryResumed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	ts := "2025-01-03T12:00:00Z"
	writeLines(t, path,
		metaLine(ts),
		`{"timestamp":"`+ts+`","type":"response_item","payload":{"type":"message","role":"user","content":[]}}`,
	)
	history, err := GetRolloutHistory(path)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if history.Kind != InitialHistoryKindResumed {
		t.Fatalf("expected resumed history, got %q", history.Kind)
	}
	if history.Resumed.ConversationID.String() != testThreadID {
		t.Fatalf("conversation id mismatch")
	}
	if len(history.GetRolloutItems()) != 2 {
		t.Fatalf("expected 2 items")
	}
	if cwd := history.SessionCwd(); cwd == nil || *cwd != "." {
		t.Fatalf("session cwd = %v", cwd)
	}
}
