package rollout

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppendRolloutItemToPath(t *testing.T) {
	// Pin the UTC clock so the line timestamp is deterministic.
	prev := nowUTC
	nowUTC = func() time.Time { return time.Date(2025, 5, 7, 17, 24, 21, 123_000_000, time.UTC) }
	defer func() { nowUTC = prev }()

	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("create file: %v", err)
	}

	if err := AppendRolloutItemToPath(path, agentMessageItem("appended")); err != nil {
		t.Fatalf("append: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	text := string(data)
	if !strings.HasSuffix(text, "\n") {
		t.Fatalf("line should end with newline: %q", text)
	}
	if !strings.Contains(text, `"timestamp":"2025-05-07T17:24:21.123Z"`) {
		t.Fatalf("expected millisecond UTC timestamp, got: %s", text)
	}
	if !strings.Contains(text, "appended") {
		t.Fatalf("expected appended content, got: %s", text)
	}

	// Append to a non-existent file should error.
	if err := AppendRolloutItemToPath(filepath.Join(t.TempDir(), "missing.jsonl"), agentMessageItem("x")); err == nil {
		t.Fatalf("expected error appending to missing file")
	}
}

func TestTurnContextRoundTripPreservesUnknownFields(t *testing.T) {
	raw := `{"cwd":"/work","approval_policy":"on-request","model":"gpt-5","brand_new_field":{"nested":[1,2,3]}}`
	var item TurnContextItem
	if err := item.UnmarshalJSON([]byte(raw)); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if item.Cwd != "/work" {
		t.Fatalf("cwd = %q", item.Cwd)
	}
	out, err := item.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !jsonEqual(t, []byte(raw), out) {
		t.Fatalf("turn context round-trip mismatch:\n want %s\n got  %s", raw, out)
	}
}
