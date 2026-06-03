package realtimewebrtc

import "testing"

func TestEventConstructors(t *testing.T) {
	tests := []struct {
		name     string
		event    Event
		wantKind EventKind
		wantStr  string
	}{
		{name: "connected", event: Connected(), wantKind: EventKindConnected, wantStr: "Connected"},
		{name: "closed", event: Closed(), wantKind: EventKindClosed, wantStr: "Closed"},
		{name: "audio level", event: LocalAudioLevel(123), wantKind: EventKindLocalAudioLevel, wantStr: "LocalAudioLevel(123)"},
		{name: "failed", event: Failed("oops"), wantKind: EventKindFailed, wantStr: `Failed("oops")`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.event.Kind != tt.wantKind {
				t.Fatalf("Kind = %v, want %v", tt.event.Kind, tt.wantKind)
			}
			if got := tt.event.String(); got != tt.wantStr {
				t.Fatalf("String() = %q, want %q", got, tt.wantStr)
			}
		})
	}
}

func TestEventPayloads(t *testing.T) {
	if got := LocalAudioLevel(999).AudioLevel; got != 999 {
		t.Fatalf("AudioLevel = %d, want 999", got)
	}
	if got := Failed("bad").Message; got != "bad" {
		t.Fatalf("Message = %q, want %q", got, "bad")
	}
}

func TestEventKindString(t *testing.T) {
	if got := EventKind(99).String(); got != "EventKind(99)" {
		t.Fatalf("unknown kind String() = %q", got)
	}
}
