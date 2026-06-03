package ollama

import (
	"encoding/json"
	"testing"
)

func decodeUpdate(t *testing.T, raw string) pullUpdate {
	t.Helper()
	var u pullUpdate
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	return u
}

func TestPullEventsFromValueStatusAndSuccess(t *testing.T) {
	events := pullEventsFromValue(decodeUpdate(t, `{"status":"verifying"}`))
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1: %+v", len(events), events)
	}
	if events[0].Kind != PullEventStatus || events[0].Status != "verifying" {
		t.Errorf("unexpected event: %+v", events[0])
	}

	events2 := pullEventsFromValue(decodeUpdate(t, `{"status":"success"}`))
	if len(events2) != 2 {
		t.Fatalf("got %d events, want 2: %+v", len(events2), events2)
	}
	if events2[0].Kind != PullEventStatus || events2[0].Status != "success" {
		t.Errorf("event[0] = %+v, want Status success", events2[0])
	}
	if events2[1].Kind != PullEventSuccess {
		t.Errorf("event[1] = %+v, want Success", events2[1])
	}
}

func TestPullEventsFromValueProgress(t *testing.T) {
	events := pullEventsFromValue(decodeUpdate(t, `{"digest":"sha256:abc","total":100}`))
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1: %+v", len(events), events)
	}
	ev := events[0]
	if ev.Kind != PullEventChunkProgress || ev.Digest != "sha256:abc" {
		t.Fatalf("unexpected event: %+v", ev)
	}
	if ev.Total == nil || *ev.Total != 100 {
		t.Errorf("total = %v, want 100", ev.Total)
	}
	if ev.Completed != nil {
		t.Errorf("completed = %v, want nil", ev.Completed)
	}

	events2 := pullEventsFromValue(decodeUpdate(t, `{"digest":"sha256:def","completed":42}`))
	if len(events2) != 1 {
		t.Fatalf("got %d events, want 1: %+v", len(events2), events2)
	}
	ev2 := events2[0]
	if ev2.Kind != PullEventChunkProgress || ev2.Digest != "sha256:def" {
		t.Fatalf("unexpected event: %+v", ev2)
	}
	if ev2.Total != nil {
		t.Errorf("total = %v, want nil", ev2.Total)
	}
	if ev2.Completed == nil || *ev2.Completed != 42 {
		t.Errorf("completed = %v, want 42", ev2.Completed)
	}
}

func TestPullEventsFromValueProgressNoDigest(t *testing.T) {
	events := pullEventsFromValue(decodeUpdate(t, `{"total":10}`))
	if len(events) != 1 || events[0].Kind != PullEventChunkProgress {
		t.Fatalf("unexpected events: %+v", events)
	}
	if events[0].Digest != "" {
		t.Errorf("digest = %q, want empty", events[0].Digest)
	}
}

func TestPullEventsFromValueNegativeAndFractionalIgnored(t *testing.T) {
	// as_u64 returns None for negative or fractional values, so no progress
	// event is produced.
	events := pullEventsFromValue(decodeUpdate(t, `{"digest":"d","total":-5,"completed":1.5}`))
	if len(events) != 0 {
		t.Fatalf("got %d events, want 0: %+v", len(events), events)
	}
}

func TestPullEventsFromValueEmpty(t *testing.T) {
	if events := pullEventsFromValue(decodeUpdate(t, `{}`)); len(events) != 0 {
		t.Fatalf("got %d events, want 0: %+v", len(events), events)
	}
}
