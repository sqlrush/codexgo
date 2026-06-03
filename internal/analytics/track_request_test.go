package analytics

import (
	"encoding/json"
	"testing"
)

func TestTrackEventRequestBatches(t *testing.T) {
	t.Parallel()

	skill := TrackEventRequest{kind: trackSkillInvocation, skillInvocation: &SkillInvocationEventRequest{}}
	hook := TrackEventRequest{kind: trackHookRun, hookRun: &CodexHookRunEventRequest{}}
	lines := TrackEventRequest{kind: trackAcceptedLines, acceptedLines: &CodexAcceptedLineFingerprintsEventRequest{}}

	tests := []struct {
		name      string
		events    []TrackEventRequest
		wantSizes []int
	}{
		{
			name:      "non_isolated_stay_in_one_batch",
			events:    []TrackEventRequest{skill, hook},
			wantSizes: []int{2},
		},
		{
			name:      "isolated_event_is_alone",
			events:    []TrackEventRequest{lines},
			wantSizes: []int{1},
		},
		{
			name:      "isolated_splits_surrounding_batches",
			events:    []TrackEventRequest{skill, lines, hook},
			wantSizes: []int{1, 1, 1},
		},
		{
			name:      "trailing_non_isolated_flushed",
			events:    []TrackEventRequest{lines, skill, hook},
			wantSizes: []int{1, 2},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			batches := trackEventRequestBatches(tt.events)
			if len(batches) != len(tt.wantSizes) {
				t.Fatalf("batch count: got %d want %d", len(batches), len(tt.wantSizes))
			}
			for i, size := range tt.wantSizes {
				if len(batches[i]) != size {
					t.Errorf("batch %d size: got %d want %d", i, len(batches[i]), size)
				}
			}
		})
	}
}

func TestTrackEventRequestUntaggedJSON(t *testing.T) {
	t.Parallel()
	// Untagged serialization writes the inner variant directly (no wrapper key).
	scope := "user"
	req := TrackEventRequest{
		kind: trackSkillInvocation,
		skillInvocation: &SkillInvocationEventRequest{
			EventType:   "codex_skill_invocation",
			SkillID:     "my-skill",
			SkillName:   "my-skill",
			EventParams: SkillInvocationEventParams{SkillScope: &scope},
		},
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["event_type"] != "codex_skill_invocation" {
		t.Errorf("event_type: got %v", m["event_type"])
	}
	if m["skill_id"] != "my-skill" {
		t.Errorf("skill_id: got %v", m["skill_id"])
	}
	if _, hasWrapper := m["SkillInvocation"]; hasWrapper {
		t.Error("expected untagged serialization, found wrapper key")
	}
}
