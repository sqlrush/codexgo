package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// recordingTranscript counts the core events fed to it and records the last
// message delivered through Update.
type recordingTranscript struct {
	appended     int
	userMessages int
	lastMsg      tea.Msg
}

func (r recordingTranscript) Update(msg tea.Msg) (TranscriptView, tea.Cmd) {
	r.lastMsg = msg
	return r, nil
}
func (recordingTranscript) View(Rect) string { return "TRANSCRIPT" }
func (r recordingTranscript) AppendCoreEvent(CoreEventMsg) TranscriptView {
	r.appended++
	return r
}
func (r recordingTranscript) AppendUserMessage(string) TranscriptView {
	r.userMessages++
	return r
}

// recordingBottom reports a fixed desired height and records updates.
type recordingBottom struct {
	desired int
	lastMsg tea.Msg
}

func (b recordingBottom) Update(msg tea.Msg) (BottomPane, tea.Cmd) {
	b.lastMsg = msg
	return b, nil
}
func (recordingBottom) View(Rect) string        { return "BOTTOM" }
func (b recordingBottom) DesiredHeight(int) int { return b.desired }

func newTestModel(tr TranscriptView, bp BottomPane) Model {
	return NewModel(ModelConfig{
		Caps:       Capabilities{ColorLevel: ColorLevelTrueColor},
		Transcript: tr,
		Bottom:     bp,
	})
}

func TestModelInitReturnsRedraw(t *testing.T) {
	m := newTestModel(noopTranscript{}, noopBottomPane{})
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init should return a command")
	}
	if _, ok := cmd().(RedrawEvent); !ok {
		t.Fatalf("Init cmd produced %T, want RedrawEvent", cmd())
	}
}

func TestModelWindowSizeUpdatesDimensions(t *testing.T) {
	m := newTestModel(noopTranscript{}, recordingBottom{desired: 2})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	got := updated.(Model)
	if got.width != 100 || got.height != 40 {
		t.Fatalf("dimensions = %dx%d, want 100x40", got.width, got.height)
	}
}

func TestModelCoreEventAppendsTranscript(t *testing.T) {
	m := newTestModel(recordingTranscript{}, noopBottomPane{})
	ev := CoreEventMsg{Event: protocol.Event{ID: "1", Msg: protocol.EventMsg{Type: protocol.EventMsgKindAgentMessage}}}
	updated, _ := m.Update(ev)
	got := updated.(Model).transcript.(recordingTranscript)
	if got.appended != 1 {
		t.Fatalf("appended = %d, want 1", got.appended)
	}
}

func TestModelExitEventQuits(t *testing.T) {
	m := newTestModel(noopTranscript{}, noopBottomPane{})
	updated, cmd := m.Update(ExitEvent{Mode: ExitShutdownFirst})
	if !updated.(Model).quitting {
		t.Fatal("model should be quitting after ExitEvent")
	}
	if cmd == nil {
		t.Fatal("ExitEvent should return tea.Quit command")
	}
	if msg := cmd(); msg == nil {
		t.Fatal("quit command should produce a message")
	}
}

func TestModelEngineClosedQuits(t *testing.T) {
	m := newTestModel(noopTranscript{}, noopBottomPane{})
	updated, cmd := m.Update(EngineClosedMsg{})
	if !updated.(Model).quitting {
		t.Fatal("model should be quitting after EngineClosedMsg")
	}
	if cmd == nil {
		t.Fatal("EngineClosedMsg should quit")
	}
}

func TestModelViewStacksRegions(t *testing.T) {
	m := newTestModel(recordingTranscript{}, recordingBottom{desired: 1})
	m.width = 20
	m.height = 5
	out := m.View()
	if out != "TRANSCRIPT\nBOTTOM" {
		t.Fatalf("View = %q, want stacked regions", out)
	}
}

func TestModelViewEmptyWhenUnsized(t *testing.T) {
	m := newTestModel(recordingTranscript{}, recordingBottom{desired: 1})
	if out := m.View(); out != "" {
		t.Fatalf("unsized View = %q, want empty", out)
	}
}

func TestModelDelegatesUnknownMsg(t *testing.T) {
	m := newTestModel(recordingTranscript{}, recordingBottom{desired: 1})
	type customMsg struct{}
	updated, _ := m.Update(customMsg{})
	tr := updated.(Model).transcript.(recordingTranscript)
	bp := updated.(Model).bottom.(recordingBottom)
	if _, ok := tr.lastMsg.(customMsg); !ok {
		t.Fatal("transcript should have received the custom message")
	}
	if _, ok := bp.lastMsg.(customMsg); !ok {
		t.Fatal("bottom pane should have received the custom message")
	}
}

func TestModelSubmitUserMessageWithoutEngineIsNoop(t *testing.T) {
	m := newTestModel(noopTranscript{}, noopBottomPane{})
	_, cmd := m.Update(SubmitUserMessageEvent{Text: "hello"})
	// No engine: command path returns nil (nothing to submit).
	if cmd != nil {
		if msg := cmd(); msg != nil {
			t.Fatalf("expected no-op submit without engine, got %T", msg)
		}
	}
}

// TestModelSubmitUserMessageEchoesIntoTranscript verifies that submitting a user
// message echoes it into the transcript TUI-side (codex's on_user_message_display),
// independent of the engine.
func TestModelSubmitUserMessageEchoesIntoTranscript(t *testing.T) {
	m := newTestModel(recordingTranscript{}, recordingBottom{desired: 1})
	updated, _ := m.Update(SubmitUserMessageEvent{Text: "hello"})
	tr := updated.(Model).transcript.(recordingTranscript)
	if tr.userMessages != 1 {
		t.Fatalf("userMessages = %d, want 1 (submit should echo into transcript)", tr.userMessages)
	}
}

// TestModelSubmitEmptyUserMessageNoEcho verifies a whitespace-only submission
// does not echo a user cell.
func TestModelSubmitEmptyUserMessageNoEcho(t *testing.T) {
	m := newTestModel(recordingTranscript{}, recordingBottom{desired: 1})
	updated, _ := m.Update(SubmitUserMessageEvent{Text: "   "})
	tr := updated.(Model).transcript.(recordingTranscript)
	if tr.userMessages != 0 {
		t.Fatalf("userMessages = %d, want 0 (empty submit should not echo)", tr.userMessages)
	}
}
