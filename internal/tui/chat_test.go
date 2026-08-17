package tui

import (
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

func agentDelta(delta string) CoreEventMsg {
	return CoreEventMsg{Event: protocol.Event{Msg: protocol.EventMsg{
		Type:                     protocol.EventMsgKindAgentMessageContentDelta,
		AgentMessageContentDelta: &protocol.AgentMessageContentDeltaEvent{Delta: delta},
	}}}
}

func turnComplete() CoreEventMsg {
	return CoreEventMsg{Event: protocol.Event{Msg: protocol.EventMsg{
		Type:         protocol.EventMsgKindTurnComplete,
		TurnComplete: &protocol.TurnCompleteEvent{},
	}}}
}

func TestTranscriptUserMessageCell(t *testing.T) {
	tr := NewChatTranscript(testTheme())
	ev := CoreEventMsg{Event: protocol.Event{Msg: protocol.EventMsg{
		Type:        protocol.EventMsgKindUserMessage,
		UserMessage: &protocol.UserMessageEvent{Message: "hi there"},
	}}}
	tr2 := tr.AppendCoreEvent(ev).(ChatTranscript)
	out := tr2.View(Rect{Width: 40, Height: 5})
	if !strings.Contains(out, "hi there") {
		t.Fatalf("user message not rendered: %q", out)
	}
}

func TestTranscriptStreamingCommitsOnNewline(t *testing.T) {
	tr := NewChatTranscript(testTheme())
	tr = tr.AppendCoreEvent(agentDelta("Hello ")).(ChatTranscript)
	tr = tr.AppendCoreEvent(agentDelta("world\n")).(ChatTranscript)
	out := tr.View(Rect{Width: 40, Height: 5})
	if !strings.Contains(out, "Hello world") {
		t.Fatalf("streamed agent text missing: %q", out)
	}
}

func TestTranscriptStreamFinalizedOnTurnComplete(t *testing.T) {
	tr := NewChatTranscript(testTheme())
	tr = tr.AppendCoreEvent(agentDelta("final answer")).(ChatTranscript)
	// No newline yet: pending tail still shown live.
	tr = tr.AppendCoreEvent(turnComplete()).(ChatTranscript)
	out := tr.View(Rect{Width: 40, Height: 5})
	if !strings.Contains(out, "final answer") {
		t.Fatalf("finalized stream missing: %q", out)
	}
}

func TestTranscriptExecCell(t *testing.T) {
	tr := NewChatTranscript(testTheme())
	begin := CoreEventMsg{Event: protocol.Event{Msg: protocol.EventMsg{
		Type:             protocol.EventMsgKindExecCommandBegin,
		ExecCommandBegin: &protocol.ExecCommandBeginEvent{CallID: "c1", Command: []string{"ls", "-la"}},
	}}}
	end := CoreEventMsg{Event: protocol.Event{Msg: protocol.EventMsg{
		Type:           protocol.EventMsgKindExecCommandEnd,
		ExecCommandEnd: &protocol.ExecCommandEndEvent{CallID: "c1", ExitCode: 0, AggregatedOutput: "file1\nfile2\n"},
	}}}
	tr = tr.AppendCoreEvent(begin).(ChatTranscript)
	tr = tr.AppendCoreEvent(end).(ChatTranscript)
	out := tr.View(Rect{Width: 40, Height: 8})
	if !strings.Contains(out, "ls -la") {
		t.Fatalf("exec command not rendered: %q", out)
	}
	if !strings.Contains(out, "file1") {
		t.Fatalf("exec output not rendered: %q", out)
	}
}

func TestTranscriptErrorCell(t *testing.T) {
	tr := NewChatTranscript(testTheme())
	ev := CoreEventMsg{Event: protocol.Event{Msg: protocol.EventMsg{
		Type:  protocol.EventMsgKindError,
		Error: &protocol.ErrorEvent{Message: "boom"},
	}}}
	tr = tr.AppendCoreEvent(ev).(ChatTranscript)
	out := tr.View(Rect{Width: 40, Height: 5})
	if !strings.Contains(out, "boom") {
		t.Fatalf("error not rendered: %q", out)
	}
}
