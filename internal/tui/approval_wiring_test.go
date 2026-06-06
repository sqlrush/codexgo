package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// execApprovalEvent builds a CoreEventMsg carrying an exec approval request.
func execApprovalEvent(callID string, cmd []string) CoreEventMsg {
	return CoreEventMsg{Event: protocol.Event{Msg: protocol.EventMsg{
		Type: protocol.EventMsgKindExecApprovalRequest,
		ExecApprovalRequest: &protocol.ExecApprovalRequestEvent{
			CallID:  callID,
			Command: cmd,
		},
	}}}
}

// TestExecApprovalRequestPushesOverlay verifies an exec approval event opens
// the approval modal (A1) and that confirming with 'y' does NOT deadlock on an
// unbuffered sender — the decision must come back as a deferred command.
func TestExecApprovalRequestPushesOverlay(t *testing.T) {
	p := newIdleBottomPane("x")
	sender := NewAppEventSender()
	p.sender = sender

	next, _ := p.Update(execApprovalEvent("call-1", []string{"rm", "-rf", "build"}))
	p = next.(ChatBottomPane)
	if p.overlays.IsEmpty() {
		t.Fatalf("exec approval event must push an approval overlay")
	}
	view := stripSGR(p.View(Rect{Width: 80, Height: 16}))
	for _, want := range []string{"run the following command", "Yes, proceed"} {
		if !contains(view, want) {
			t.Errorf("approval view missing %q:\n%s", want, view)
		}
	}

	// Confirm with 'y' through an UNBUFFERED sender with no reader during
	// Update — the deadlock shape. Update must return promptly.
	blocked := make(chan tea.Msg)
	sender.attachFunc(func(msg tea.Msg) { blocked <- msg })

	done := make(chan tea.Cmd, 1)
	go func() {
		_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
		done <- cmd
	}()
	var cmd tea.Cmd
	select {
	case cmd = <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("Update blocked: approval decision sent synchronously (deadlock)")
	}
	if cmd == nil {
		t.Fatalf("approval confirm must return a deferred decision command")
	}
	// The deferred command delivers the SubmitThreadOpEvent once a reader exists.
	go cmd()
	select {
	case msg := <-blocked:
		ev, ok := msg.(SubmitThreadOpEvent)
		if !ok {
			t.Fatalf("delivered %T, want SubmitThreadOpEvent", msg)
		}
		if ev.Command.Kind != AppCommandExecApproval {
			t.Errorf("op kind = %v, want exec approval", ev.Command.Kind)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("deferred approval decision never delivered")
	}
}

// TestRequestUserInputPushesOverlay verifies a request_user_input event opens
// the input overlay (A3).
func TestRequestUserInputPushesOverlay(t *testing.T) {
	p := newIdleBottomPane("x")
	p.sender = NewAppEventSender()

	ev := CoreEventMsg{Event: protocol.Event{Msg: protocol.EventMsg{
		Type: protocol.EventMsgKindRequestUserInput,
		RequestUserInput: &protocol.RequestUserInputEvent{
			TurnID:    "t1",
			CallID:    "c1",
			Questions: []protocol.RequestUserInputQuestion{{ID: "q1", Question: "Pick a port"}},
		},
	}}}
	next, _ := p.Update(ev)
	p = next.(ChatBottomPane)
	if p.overlays.IsEmpty() {
		t.Fatalf("request_user_input event must push the input overlay")
	}
	if _, ok := p.overlays.views[len(p.overlays.views)-1].(*RequestUserInputOverlay); !ok {
		t.Fatalf("top overlay is not the user-input overlay")
	}
}

// TestPermissionsRequestPushesApproval verifies a request_permissions event
// opens the approval modal in permissions mode (A3).
func TestPermissionsRequestPushesApproval(t *testing.T) {
	p := newIdleBottomPane("x")
	p.sender = NewAppEventSender()

	ev := CoreEventMsg{Event: protocol.Event{Msg: protocol.EventMsg{
		Type:               protocol.EventMsgKindRequestPermissions,
		RequestPermissions: &protocol.RequestPermissionsEvent{CallID: "c1", TurnID: "t1"},
	}}}
	next, _ := p.Update(ev)
	p = next.(ChatBottomPane)
	if p.overlays.IsEmpty() {
		t.Fatalf("request_permissions event must push the approval overlay")
	}
	view := stripSGR(p.View(Rect{Width: 80, Height: 16}))
	if !contains(view, "permission") && !contains(view, "Permission") && !contains(view, "grant") {
		t.Errorf("permissions approval view unexpected:\n%s", view)
	}
}

// contains is a tiny strings.Contains wrapper kept local to avoid importing
// strings in this test file's hot assertions.
func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
