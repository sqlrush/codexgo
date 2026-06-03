package cloud

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDiffSummaryFromDiff(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		diff string
		want DiffSummary
	}{
		{
			name: "single_file",
			diff: "diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1,2 +1,3 @@\n a\n-b\n+c\n+d\n",
			want: DiffSummary{FilesChanged: 1, LinesAdded: 2, LinesRemoved: 1},
		},
		{
			name: "no_git_header_nonempty",
			diff: "+added line\n",
			want: DiffSummary{FilesChanged: 1, LinesAdded: 1, LinesRemoved: 0},
		},
		{
			name: "empty",
			diff: "",
			want: DiffSummary{},
		},
		{
			name: "two_files",
			diff: "diff --git a/x b/x\n@@\n+a\ndiff --git a/y b/y\n@@\n-b\n",
			want: DiffSummary{FilesChanged: 2, LinesAdded: 1, LinesRemoved: 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := diffSummaryFromDiff(tt.diff); got != tt.want {
				t.Errorf("diffSummaryFromDiff = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestIsUnifiedDiff(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		diff string
		want bool
	}{
		{name: "git_header", diff: "diff --git a/x b/x\n", want: true},
		{name: "dash_hunk", diff: "header\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n", want: true},
		{name: "leading_hunk", diff: "@@ -1 +1 @@\n-a\n+b", want: false},
		{name: "plain_text", diff: "just some text", want: false},
		{name: "codex_patch", diff: "*** Begin Patch\n*** End Patch\n", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isUnifiedDiff(tt.diff); got != tt.want {
				t.Errorf("isUnifiedDiff(%q) = %v, want %v", tt.diff, got, tt.want)
			}
		})
	}
}

func TestAttemptStatusFromStr(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want AttemptStatus
	}{
		{"failed", AttemptStatusFailed},
		{"completed", AttemptStatusCompleted},
		{"in_progress", AttemptStatusInProgress},
		{"pending", AttemptStatusPending},
		{"unknown-value", AttemptStatusPending},
		{"", AttemptStatusPending},
	}
	for _, tt := range tests {
		if got := attemptStatusFromStr(tt.in); got != tt.want {
			t.Errorf("attemptStatusFromStr(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestMapStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		json string
		want TaskStatus
	}{
		{name: "nil", json: "", want: TaskStatusPending},
		{name: "turn_completed", json: `{"latest_turn_status_display":{"turn_status":"completed"}}`, want: TaskStatusReady},
		{name: "turn_failed", json: `{"latest_turn_status_display":{"turn_status":"failed"}}`, want: TaskStatusError},
		{name: "turn_cancelled", json: `{"latest_turn_status_display":{"turn_status":"cancelled"}}`, want: TaskStatusError},
		{name: "state_applied", json: `{"state":"applied"}`, want: TaskStatusApplied},
		{name: "state_ready", json: `{"state":"ready"}`, want: TaskStatusReady},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sd := decodeStatusDisplay(t, tt.json)
			if got := mapStatus(sd); got != tt.want {
				t.Errorf("mapStatus = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDiffSummaryAndAttemptTotalFromStatusDisplay(t *testing.T) {
	t.Parallel()
	sd := decodeStatusDisplay(t, `{
		"latest_turn_status_display": {
			"diff_stats": {"files_modified": 3, "lines_added": 10, "lines_removed": 4},
			"sibling_turn_ids": ["a", "b"]
		}
	}`)
	gotSummary := diffSummaryFromStatusDisplay(sd)
	want := DiffSummary{FilesChanged: 3, LinesAdded: 10, LinesRemoved: 4}
	if gotSummary != want {
		t.Errorf("diffSummaryFromStatusDisplay = %+v, want %+v", gotSummary, want)
	}
	total := attemptTotalFromStatusDisplay(sd)
	if total == nil || *total != 3 {
		t.Errorf("attemptTotalFromStatusDisplay = %v, want 3", total)
	}
}

func TestSortAttempts(t *testing.T) {
	t.Parallel()
	p := func(v int64) *int64 { return &v }
	tm := func(s int64) *time.Time { v := time.Unix(s, 0).UTC(); return &v }
	attempts := []TurnAttempt{
		{TurnID: "c", AttemptPlacement: nil, CreatedAt: tm(200)},
		{TurnID: "a", AttemptPlacement: p(2)},
		{TurnID: "b", AttemptPlacement: p(1)},
		{TurnID: "d", AttemptPlacement: nil, CreatedAt: tm(100)},
	}
	sortAttempts(attempts)
	gotOrder := []string{attempts[0].TurnID, attempts[1].TurnID, attempts[2].TurnID, attempts[3].TurnID}
	want := []string{"b", "a", "d", "c"}
	for i := range want {
		if gotOrder[i] != want[i] {
			t.Errorf("order = %v, want %v", gotOrder, want)
			break
		}
	}
}

func decodeStatusDisplay(t *testing.T, jsonStr string) statusDisplay {
	t.Helper()
	if jsonStr == "" {
		return nil
	}
	var sd statusDisplay
	if err := json.Unmarshal([]byte(jsonStr), &sd); err != nil {
		t.Fatalf("decode status display: %v", err)
	}
	return sd
}
