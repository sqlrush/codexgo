package rollout

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseTimestampUUIDFromFilename(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantOK  bool
		wantTS  time.Time
		wantUID string
	}{
		{
			name:    "valid",
			input:   "rollout-2025-05-07T17-24-21-5973b6c0-94b8-487b-a530-2aeb6098ae0e.jsonl",
			wantOK:  true,
			wantTS:  time.Date(2025, 5, 7, 17, 24, 21, 0, time.UTC),
			wantUID: "5973b6c0-94b8-487b-a530-2aeb6098ae0e",
		},
		{"missing prefix", "2025-05-07T17-24-21-5973b6c0-94b8-487b-a530-2aeb6098ae0e.jsonl", false, time.Time{}, ""},
		{"missing suffix", "rollout-2025-05-07T17-24-21-5973b6c0-94b8-487b-a530-2aeb6098ae0e", false, time.Time{}, ""},
		{"bad uuid", "rollout-2025-05-07T17-24-21-not-a-uuid.jsonl", false, time.Time{}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseTimestampUUIDFromFilename(tc.input)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if !got.Timestamp.Equal(tc.wantTS) {
				t.Fatalf("timestamp = %v, want %v", got.Timestamp, tc.wantTS)
			}
			if got.UUID != tc.wantUID {
				t.Fatalf("uuid = %q, want %q", got.UUID, tc.wantUID)
			}
		})
	}
}

func TestRolloutDateParts(t *testing.T) {
	year, month, day, ok := RolloutDateParts("rollout-2025-05-07T17-24-21-5973b6c0-94b8-487b-a530-2aeb6098ae0e.jsonl")
	if !ok {
		t.Fatalf("expected ok")
	}
	if year != "2025" || month != "05" || day != "07" {
		t.Fatalf("got %s/%s/%s", year, month, day)
	}
	if _, _, _, ok := RolloutDateParts("notarollout.jsonl"); ok {
		t.Fatalf("expected not ok for non-rollout filename")
	}
}

func writeSessionFile(t *testing.T, home, ts, uuid string) string {
	t.Helper()
	dayDir := filepath.Join(home, "sessions", "2025", "01", "03")
	if err := os.MkdirAll(dayDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dayDir, "rollout-"+ts+"-"+uuid+".jsonl")
	writeLines(t, path,
		`{"timestamp":"2025-01-03T12:00:00Z","type":"session_meta","payload":{"id":"`+uuid+`","timestamp":"2025-01-03T12:00:00Z","cwd":".","originator":"o","cli_version":"v","source":"cli","model_provider":"p"}}`,
		`{"timestamp":"2025-01-03T12:00:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}}`,
	)
	return path
}

func TestFindThreadPathByIDStr(t *testing.T) {
	home := t.TempDir()
	uuid := "5973b6c0-94b8-487b-a530-2aeb6098ae0e"
	want := writeSessionFile(t, home, "2025-01-03T12-00-00", uuid)

	got, err := FindThreadPathByIDStr(home, uuid)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got != want {
		t.Fatalf("found %q, want %q", got, want)
	}

	// Unknown id.
	missing, err := FindThreadPathByIDStr(home, "00000000-0000-0000-0000-000000000000")
	if err != nil {
		t.Fatalf("find missing: %v", err)
	}
	if missing != "" {
		t.Fatalf("expected empty for missing id, got %q", missing)
	}

	// Invalid id format.
	invalid, err := FindThreadPathByIDStr(home, "not-a-uuid")
	if err != nil {
		t.Fatalf("find invalid: %v", err)
	}
	if invalid != "" {
		t.Fatalf("expected empty for invalid id, got %q", invalid)
	}
}

func TestReadHeadForSummaryAndSessionMeta(t *testing.T) {
	home := t.TempDir()
	uuid := "5973b6c0-94b8-487b-a530-2aeb6098ae0e"
	path := writeSessionFile(t, home, "2025-01-03T12-00-00", uuid)

	head, err := ReadHeadForSummary(path)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if len(head) != 2 {
		t.Fatalf("expected 2 head records (meta + response item), got %d", len(head))
	}

	meta, err := ReadSessionMetaLine(path)
	if err != nil {
		t.Fatalf("meta: %v", err)
	}
	if meta.Meta.ID.String() != uuid {
		t.Fatalf("meta id = %q, want %q", meta.Meta.ID.String(), uuid)
	}
}
