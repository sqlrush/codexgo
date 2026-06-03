package realtimeconv

import (
	"encoding/json"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

func TestConversationEndReason(t *testing.T) {
	tests := []struct {
		end  ConversationEnd
		want string
	}{
		{EndRequested, "requested"},
		{EndTransportClosed, "transport_closed"},
		{EndError, "error"},
	}
	for _, tt := range tests {
		if got := tt.end.reason(); got != tt.want {
			t.Fatalf("reason(%d) = %q, want %q", tt.end, got, tt.want)
		}
	}
}

func TestRealtimeEventMsg(t *testing.T) {
	msg, err := RealtimeEventMsg(NewError("oops"))
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if msg.Type != protocol.EventMsgKindRealtimeConversationRealtime {
		t.Fatalf("Type = %v", msg.Type)
	}
	if msg.RealtimeConversationRealtime == nil {
		t.Fatalf("nil realtime event")
	}
	var decoded Event
	if err := json.Unmarshal(msg.RealtimeConversationRealtime.Payload, &decoded); err != nil {
		t.Fatalf("payload decode: %v", err)
	}
	if !decoded.IsError() || decoded.ErrorMessage != "oops" {
		t.Fatalf("unexpected decoded payload %+v", decoded)
	}
}

func TestStartedEventMsg(t *testing.T) {
	msg := StartedEventMsg(strPtr("sess-1"), protocol.RealtimeConversationVersionV2)
	if msg.Type != protocol.EventMsgKindRealtimeConversationStarted {
		t.Fatalf("Type = %v", msg.Type)
	}
	got := msg.RealtimeConversationStarted
	if got == nil || got.RealtimeSessionID == nil || *got.RealtimeSessionID != "sess-1" {
		t.Fatalf("unexpected started event %+v", got)
	}
	if got.Version != protocol.RealtimeConversationVersionV2 {
		t.Fatalf("version = %v", got.Version)
	}
}

func TestSdpEventMsg(t *testing.T) {
	msg := SdpEventMsg("v=0 answer")
	if msg.Type != protocol.EventMsgKindRealtimeConversationSdp {
		t.Fatalf("Type = %v", msg.Type)
	}
	if msg.RealtimeConversationSdp == nil || msg.RealtimeConversationSdp.SDP != "v=0 answer" {
		t.Fatalf("unexpected SDP event %+v", msg.RealtimeConversationSdp)
	}
}

func TestClosedEventMsg(t *testing.T) {
	msg := ClosedEventMsg(EndError)
	if msg.Type != protocol.EventMsgKindRealtimeConversationClosed {
		t.Fatalf("Type = %v", msg.Type)
	}
	got := msg.RealtimeConversationClosed
	if got == nil || got.Reason == nil || *got.Reason != "error" {
		t.Fatalf("unexpected closed event %+v", got)
	}
}

func TestErrorEventMsg(t *testing.T) {
	msg := ErrorEventMsg("bad input")
	if msg.Type != protocol.EventMsgKindError {
		t.Fatalf("Type = %v", msg.Type)
	}
	if msg.Error == nil || msg.Error.Message != "bad input" {
		t.Fatalf("unexpected error event %+v", msg.Error)
	}
	if msg.Error.CodexErrorInfo == nil || msg.Error.CodexErrorInfo.Kind != protocol.CodexErrorInfoBadRequest {
		t.Fatalf("unexpected codex error info %+v", msg.Error.CodexErrorInfo)
	}
	// The whole EventMsg should round-trip through JSON.
	encoded, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back protocol.EventMsg
	if err := json.Unmarshal(encoded, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Type != protocol.EventMsgKindError {
		t.Fatalf("round-trip type = %v", back.Type)
	}
}
