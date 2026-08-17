package realtimewebrtc

import (
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

func TestOfferSdpEvent(t *testing.T) {
	ev := OfferSdpEvent("v=0 offer")
	if ev.Type != protocol.EventMsgKindRealtimeConversationSdp {
		t.Fatalf("Type = %v", ev.Type)
	}
	if ev.RealtimeConversationSdp == nil || ev.RealtimeConversationSdp.SDP != "v=0 offer" {
		t.Fatalf("unexpected SDP event: %+v", ev.RealtimeConversationSdp)
	}
}

func TestEventToProtocol(t *testing.T) {
	tests := []struct {
		name     string
		event    Event
		wantOK   bool
		wantType protocol.EventMsgKind
		check    func(t *testing.T, msg protocol.EventMsg)
	}{
		{
			name:     "connected",
			event:    Connected(),
			wantOK:   true,
			wantType: protocol.EventMsgKindRealtimeConversationStarted,
			check: func(t *testing.T, msg protocol.EventMsg) {
				if msg.RealtimeConversationStarted == nil {
					t.Fatal("nil started event")
				}
				if msg.RealtimeConversationStarted.Version != protocol.RealtimeConversationVersionV2 {
					t.Fatalf("version = %v", msg.RealtimeConversationStarted.Version)
				}
			},
		},
		{
			name:     "closed",
			event:    Closed(),
			wantOK:   true,
			wantType: protocol.EventMsgKindRealtimeConversationClosed,
			check: func(t *testing.T, msg protocol.EventMsg) {
				if msg.RealtimeConversationClosed == nil {
					t.Fatal("nil closed event")
				}
				if msg.RealtimeConversationClosed.Reason != nil {
					t.Fatalf("expected nil reason, got %v", *msg.RealtimeConversationClosed.Reason)
				}
			},
		},
		{
			name:     "failed",
			event:    Failed("kaput"),
			wantOK:   true,
			wantType: protocol.EventMsgKindRealtimeConversationClosed,
			check: func(t *testing.T, msg protocol.EventMsg) {
				if msg.RealtimeConversationClosed == nil || msg.RealtimeConversationClosed.Reason == nil {
					t.Fatal("expected reason")
				}
				if *msg.RealtimeConversationClosed.Reason != "kaput" {
					t.Fatalf("reason = %q", *msg.RealtimeConversationClosed.Reason)
				}
			},
		},
		{
			name:   "audio level not projected",
			event:  LocalAudioLevel(10),
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, ok := EventToProtocol(tt.event)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if msg.Type != tt.wantType {
				t.Fatalf("Type = %v, want %v", msg.Type, tt.wantType)
			}
			if tt.check != nil {
				tt.check(t, msg)
			}
		})
	}
}

func TestAnswerSDPFromOp(t *testing.T) {
	webrtcTransport := &protocol.ConversationStartTransport{
		Type: protocol.ConversationStartTransportWebrtc,
		SDP:  "answer-sdp",
	}
	wsTransport := &protocol.ConversationStartTransport{
		Type: protocol.ConversationStartTransportWebsocket,
	}

	tests := []struct {
		name    string
		op      protocol.Op
		want    string
		wantErr bool
	}{
		{
			name: "valid webrtc",
			op: protocol.Op{
				Type: protocol.OpRealtimeConversationStart,
				RealtimeConversationStartParams: &protocol.ConversationStartParams{
					Transport: webrtcTransport,
				},
			},
			want: "answer-sdp",
		},
		{
			name:    "wrong op type",
			op:      protocol.Op{Type: protocol.OpInterrupt},
			wantErr: true,
		},
		{
			name:    "missing params",
			op:      protocol.Op{Type: protocol.OpRealtimeConversationStart},
			wantErr: true,
		},
		{
			name: "missing transport",
			op: protocol.Op{
				Type:                            protocol.OpRealtimeConversationStart,
				RealtimeConversationStartParams: &protocol.ConversationStartParams{},
			},
			wantErr: true,
		},
		{
			name: "websocket transport",
			op: protocol.Op{
				Type: protocol.OpRealtimeConversationStart,
				RealtimeConversationStartParams: &protocol.ConversationStartParams{
					Transport: wsTransport,
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := AnswerSDPFromOp(tt.op)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
