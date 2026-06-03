package feedback

import "testing"

func TestUploadTagsReservedAndOverrides(t *testing.T) {
	t.Parallel()

	snap := &FeedbackSnapshot{
		tags: map[string]string{
			"thread_id":      "wrong-thread",
			"turn_id":        "wrong-turn",
			"classification": "wrong-classification",
			"cli_version":    "wrong-version",
			"session_source": "wrong-source",
			"reason":         "wrong-reason",
			"account_id":     "actual-account",
			"model":          "gpt-5",
		},
		ThreadID: "thread-123",
	}

	clientTags := map[string]string{
		"thread_id":      "wrong-client-thread",
		"turn_id":        "turn-456",
		"classification": "wrong-client-classification",
		"cli_version":    "wrong-client-version",
		"session_source": "wrong-client-source",
		"reason":         "wrong-client-reason",
		"client_tag":     "from-client",
	}

	reason := "actual reason"
	source := "cli"
	tags := snap.UploadTags("bug", &reason, clientTags, &source)

	want := map[string]string{
		"thread_id":      "thread-123",
		"turn_id":        "turn-456",
		"classification": "bug",
		"session_source": "cli",
		"reason":         "actual reason",
		"account_id":     "actual-account",
		"client_tag":     "from-client",
		"model":          "gpt-5",
		"cli_version":    cliVersion,
	}
	for k, v := range want {
		if tags[k] != v {
			t.Errorf("tag %q: got %q want %q", k, tags[k], v)
		}
	}
}

func TestDisplayClassification(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"bug":          "Bug",
		"bad_result":   "Bad result",
		"good_result":  "Good result",
		"safety_check": "Safety check",
		"other":        "Other",
		"unknown":      "Other",
	}
	for in, want := range tests {
		if got := displayClassification(in); got != want {
			t.Errorf("displayClassification(%q): got %q want %q", in, got, want)
		}
	}
}

func TestTitleFor(t *testing.T) {
	t.Parallel()
	if got := titleFor("bug", "thread-1"); got != "[Bug]: Codex session thread-1" {
		t.Errorf("titleFor: got %q", got)
	}
}
