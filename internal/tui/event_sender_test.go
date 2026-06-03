package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestAppEventSenderUnattachedIsNoop(t *testing.T) {
	s := NewAppEventSender()
	// Should not panic when no program/func is attached.
	s.Send(RedrawEvent{})
	s.SendMsg(EngineClosedMsg{})
	s.Interrupt()
}

func TestAppEventSenderDeliversEvents(t *testing.T) {
	var got []tea.Msg
	s := NewAppEventSender()
	s.attachFunc(func(msg tea.Msg) { got = append(got, msg) })

	s.Send(RedrawEvent{})
	s.Interrupt()
	s.Compact()

	if len(got) != 3 {
		t.Fatalf("delivered %d messages, want 3", len(got))
	}
	if _, ok := got[0].(RedrawEvent); !ok {
		t.Fatalf("first msg %T, want RedrawEvent", got[0])
	}
	op, ok := got[1].(CodexOpEvent)
	if !ok || op.Command.Kind != AppCommandInterrupt {
		t.Fatalf("second msg = %#v, want interrupt op", got[1])
	}
	op2, ok := got[2].(CodexOpEvent)
	if !ok || op2.Command.Kind != AppCommandCompact {
		t.Fatalf("third msg = %#v, want compact op", got[2])
	}
}

func TestEventCmdHelpers(t *testing.T) {
	if msg := ExitCmd(ExitImmediate)(); func() bool {
		ev, ok := msg.(ExitEvent)
		return ok && ev.Mode == ExitImmediate
	}() == false {
		t.Fatalf("ExitCmd produced %#v", msg)
	}
	if msg := SubmitUserMessageCmd("hi")(); func() bool {
		ev, ok := msg.(SubmitUserMessageEvent)
		return ok && ev.Text == "hi"
	}() == false {
		t.Fatalf("SubmitUserMessageCmd produced %#v", msg)
	}
	if msg := CodexOpCmd(NewInterruptCommand())(); func() bool {
		ev, ok := msg.(CodexOpEvent)
		return ok && ev.Command.Kind == AppCommandInterrupt
	}() == false {
		t.Fatalf("CodexOpCmd produced %#v", msg)
	}
}

func TestApprovalSendersTargetThread(t *testing.T) {
	var got tea.Msg
	s := NewAppEventSender()
	s.attachFunc(func(msg tea.Msg) { got = msg })

	s.ExecApproval("thread-9", "call-1", "approved")
	ev, ok := got.(SubmitThreadOpEvent)
	if !ok {
		t.Fatalf("ExecApproval produced %T, want SubmitThreadOpEvent", got)
	}
	if ev.ThreadID != "thread-9" {
		t.Fatalf("thread id = %q, want thread-9", ev.ThreadID)
	}
	if ev.Command.Kind != AppCommandExecApproval || ev.Command.ID != "call-1" {
		t.Fatalf("command = %#v", ev.Command)
	}
}
