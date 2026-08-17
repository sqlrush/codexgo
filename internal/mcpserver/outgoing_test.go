package mcpserver

import (
	"encoding/json"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

func TestSendRequestRegistersCallbackAndResolves(t *testing.T) {
	w := &captureWriter{}
	s := newOutgoingSender(w)

	ch := s.sendRequest("elicitation/create", mustMarshal(map[string]any{"k": "v"}))

	// One request frame must have been written with an integer id and method.
	reqs := w.serverRequestsByMethod("elicitation/create")
	if len(reqs) != 1 {
		t.Fatalf("want 1 elicitation request, got %d", len(reqs))
	}
	var id int64
	if err := json.Unmarshal(reqs[0]["id"], &id); err != nil {
		t.Fatalf("decode request id: %v", err)
	}
	if id != 0 {
		t.Fatalf("first request id = %d, want 0", id)
	}

	// Resolving the callback delivers the result on the channel.
	s.notifyClientResponse(NewIntRequestID(id), json.RawMessage(`{"decision":"approved"}`))
	got := <-ch
	if string(got) != `{"decision":"approved"}` {
		t.Fatalf("callback result = %s", got)
	}
}

func TestNotifyUnknownCallbackIsNoop(t *testing.T) {
	w := &captureWriter{}
	s := newOutgoingSender(w)
	// Must not panic or block.
	s.notifyClientResponse(NewIntRequestID(99), json.RawMessage("null"))
}

func TestSendEventAsNotificationFlattensWithMeta(t *testing.T) {
	w := &captureWriter{}
	s := newOutgoingSender(w)

	configured := protocol.SessionConfiguredEvent{
		Model:             "gpt-test",
		ModelProviderID:   "openai",
		ApprovalPolicy:    protocol.AskForApproval{Kind: protocol.AskForApprovalOnRequest},
		ApprovalsReviewer: protocol.ApprovalsReviewerUser,
	}
	ev := protocol.Event{
		ID: "evt-1",
		Msg: protocol.EventMsg{
			Type:              protocol.EventMsgKindSessionConfigured,
			SessionConfigured: &configured,
		},
	}
	tid := "thread-1"
	reqID := NewIntRequestID(5)
	s.sendEventAsNotification(ev, &outgoingMeta{RequestID: &reqID, ThreadID: &tid})

	notes := w.notificationsByMethod("codex/event")
	if len(notes) != 1 {
		t.Fatalf("want 1 codex/event, got %d", len(notes))
	}
	var params map[string]json.RawMessage
	if err := json.Unmarshal(notes[0]["params"], &params); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	// The event id is flattened at the params top level.
	var eid string
	if err := json.Unmarshal(params["id"], &eid); err != nil || eid != "evt-1" {
		t.Fatalf("flattened event id = %q (err=%v)", eid, err)
	}
	// _meta carries requestId and threadId.
	var meta map[string]any
	if err := json.Unmarshal(params["_meta"], &meta); err != nil {
		t.Fatalf("decode _meta: %v", err)
	}
	if meta["threadId"] != "thread-1" {
		t.Fatalf("_meta.threadId = %v", meta["threadId"])
	}
	if meta["requestId"] == nil {
		t.Fatalf("_meta.requestId missing")
	}
}

func TestSendEventAsNotificationWithoutMeta(t *testing.T) {
	w := &captureWriter{}
	s := newOutgoingSender(w)
	ev := protocol.Event{ID: "x", Msg: protocol.EventMsg{Type: protocol.EventMsgKindShutdownComplete}}
	s.sendEventAsNotification(ev, nil)

	notes := w.notificationsByMethod("codex/event")
	if len(notes) != 1 {
		t.Fatalf("want 1 codex/event, got %d", len(notes))
	}
	var params map[string]json.RawMessage
	if err := json.Unmarshal(notes[0]["params"], &params); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if _, ok := params["_meta"]; ok {
		t.Fatalf("params should not carry _meta when meta is nil")
	}
}

func TestDecodeDecisionDefaultsToDenied(t *testing.T) {
	tests := []struct {
		name string
		raw  json.RawMessage
		want protocol.ReviewDecisionKind
	}{
		{"empty", nil, protocol.ReviewDecisionDenied},
		{"null", json.RawMessage("null"), protocol.ReviewDecisionDenied},
		{"garbage", json.RawMessage("123"), protocol.ReviewDecisionDenied},
		{"approved", json.RawMessage(`{"decision":"approved"}`), protocol.ReviewDecisionApproved},
		{"denied", json.RawMessage(`{"decision":"denied"}`), protocol.ReviewDecisionDenied},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeDecision(tc.raw)
			if got.Kind != tc.want {
				t.Fatalf("decodeDecision(%s).Kind = %q, want %q", tc.raw, got.Kind, tc.want)
			}
		})
	}
}

func TestShellJoinQuotesArgsWithSpaces(t *testing.T) {
	tests := []struct {
		in   []string
		want string
	}{
		{[]string{"ls", "-la"}, "ls -la"},
		{[]string{"echo", "hello world"}, "echo 'hello world'"},
		{[]string{"cat", "a'b"}, `cat 'a'\''b'`},
	}
	for _, tc := range tests {
		if got := shellJoin(tc.in); got != tc.want {
			t.Fatalf("shellJoin(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
