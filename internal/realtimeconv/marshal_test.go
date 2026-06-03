package realtimeconv

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

func TestEventMarshalRoundTrip(t *testing.T) {
	tests := []struct {
		name     string
		event    Event
		wantJSON string
	}{
		{
			name:     "error",
			event:    NewError("boom"),
			wantJSON: `{"Error":"boom"}`,
		},
		{
			name: "session updated",
			event: Event{Kind: EventKindSessionUpdated, SessionUpdated: &SessionUpdated{
				RealtimeSessionID: "sess-1",
				Instructions:      strPtr("be nice"),
			}},
			wantJSON: `{"SessionUpdated":{"realtime_session_id":"sess-1","instructions":"be nice"}}`,
		},
		{
			name:     "input transcript delta",
			event:    Event{Kind: EventKindInputTranscriptDelta, TranscriptDelta: &TranscriptDelta{Delta: "hel"}},
			wantJSON: `{"InputTranscriptDelta":{"delta":"hel"}}`,
		},
		{
			name:     "output transcript done",
			event:    Event{Kind: EventKindOutputTranscriptDone, TranscriptDone: &TranscriptDone{Text: "done"}},
			wantJSON: `{"OutputTranscriptDone":{"text":"done"}}`,
		},
		{
			name:     "conversation item done",
			event:    Event{Kind: EventKindConversationItemDone, ItemID: "item-7"},
			wantJSON: `{"ConversationItemDone":{"item_id":"item-7"}}`,
		},
		{
			name:     "response created with id",
			event:    Event{Kind: EventKindResponseCreated, Response: &ResponseLifecycle{ResponseID: strPtr("r1")}},
			wantJSON: `{"ResponseCreated":{"response_id":"r1"}}`,
		},
		{
			name:     "speech started no item",
			event:    Event{Kind: EventKindInputAudioSpeechStarted, InputAudioSpeechStarted: &InputAudioSpeechStarted{}},
			wantJSON: `{"InputAudioSpeechStarted":{"item_id":null}}`,
		},
		{
			name: "noop",
			event: Event{Kind: EventKindNoopRequested, Noop: &NoopRequested{
				CallID: "call-1", ItemID: "item-1",
			}},
			wantJSON: `{"NoopRequested":{"call_id":"call-1","item_id":"item-1"}}`,
		},
		{
			name: "handoff",
			event: Event{Kind: EventKindHandoffRequested, Handoff: &HandoffRequested{
				HandoffID:        "h1",
				ItemID:           "i1",
				InputTranscript:  "go",
				ActiveTranscript: []TranscriptEntry{{Role: "user", Text: "hi"}},
			}},
			wantJSON: `{"HandoffRequested":{"handoff_id":"h1","item_id":"i1","input_transcript":"go","active_transcript":[{"role":"user","text":"hi"}]}}`,
		},
		{
			name: "audio out",
			event: Event{Kind: EventKindAudioOut, AudioOut: &protocol.RealtimeAudioFrame{
				Data: "AAA=", SampleRate: 24000, NumChannels: 1,
			}},
			wantJSON: `{"AudioOut":{"data":"AAA=","sample_rate":24000,"num_channels":1}}`,
		},
		{
			name:     "conversation item added raw",
			event:    Event{Kind: EventKindConversationItemAdded, ConversationItem: json.RawMessage(`{"role":"user"}`)},
			wantJSON: `{"ConversationItemAdded":{"role":"user"}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.event)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(got) != tt.wantJSON {
				t.Fatalf("JSON = %s, want %s", got, tt.wantJSON)
			}

			// Round-trip back to an Event and re-marshal to confirm parity.
			var decoded Event
			if err := json.Unmarshal(got, &decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			reencoded, err := json.Marshal(decoded)
			if err != nil {
				t.Fatalf("re-marshal: %v", err)
			}
			if string(reencoded) != tt.wantJSON {
				t.Fatalf("re-encoded JSON = %s, want %s", reencoded, tt.wantJSON)
			}
		})
	}
}

func TestEventUnmarshalErrors(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{name: "not an object", in: `"x"`},
		{name: "empty object", in: `{}`},
		{name: "too many keys", in: `{"Error":"a","ResponseDone":{}}`},
		{name: "unknown variant", in: `{"Mystery":1}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var e Event
			if err := json.Unmarshal([]byte(tt.in), &e); err == nil {
				t.Fatalf("expected error for %q", tt.in)
			}
		})
	}
}

func TestHandoffWireRoundTrip(t *testing.T) {
	h := &HandoffRequested{
		HandoffID:        "h",
		ActiveTranscript: []TranscriptEntry{{Role: "a", Text: "b"}},
	}
	wire := handoffToWire(h)
	back := wireToHandoff(wire)
	if !reflect.DeepEqual(h, back) {
		t.Fatalf("round trip mismatch: %+v vs %+v", h, back)
	}
}
