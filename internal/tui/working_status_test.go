package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sqlrush/codexgo/pkg/protocol"
)

// coreEvent builds a CoreEventMsg with the given message kind.
func coreEvent(kind protocol.EventMsgKind) CoreEventMsg {
	return CoreEventMsg{Event: protocol.Event{Msg: protocol.EventMsg{Type: kind}}}
}

// TestWorkingRowLifecycle verifies the live "Working (…)" row appears on
// TurnStarted, renders the elapsed + interrupt hint, and disappears on
// TurnComplete (the screenshot-confirmed codex behavior).
func TestWorkingRowLifecycle(t *testing.T) {
	p := newIdleBottomPane("Explain this codebase")

	// Idle: no working row.
	if strings.Contains(stripSGR(p.View(Rect{Width: 80, Height: 10})), "Working") {
		t.Fatalf("idle pane must not show the working row")
	}

	next, cmd := p.Update(coreEvent(protocol.EventMsgKindTurnStarted))
	p = next.(ChatBottomPane)
	if !p.taskRunning {
		t.Fatalf("TurnStarted must set taskRunning")
	}
	if cmd == nil {
		t.Fatalf("TurnStarted must start the spinner tick loop")
	}

	view := stripSGR(p.View(Rect{Width: 80, Height: 10}))
	for _, want := range []string{"Working", "esc to interrupt)"} {
		if !strings.Contains(view, want) {
			t.Errorf("running view missing %q:\n%s", want, view)
		}
	}
	// Elapsed renders inside the brackets, e.g. "(0s • esc to interrupt)".
	if !strings.Contains(view, "(0s • ") {
		t.Errorf("running view missing elapsed display:\n%s", view)
	}

	// Tick advances the loop while running.
	next, cmd = p.Update(SpinnerTickMsg{})
	p = next.(ChatBottomPane)
	if cmd == nil {
		t.Fatalf("tick while running must reschedule")
	}

	// Completion clears the row and lets the tick loop die.
	next, _ = p.Update(coreEvent(protocol.EventMsgKindTurnComplete))
	p = next.(ChatBottomPane)
	if p.taskRunning {
		t.Fatalf("TurnComplete must clear taskRunning")
	}
	next, cmd = p.Update(SpinnerTickMsg{})
	p = next.(ChatBottomPane)
	if cmd != nil {
		t.Fatalf("tick after completion must not reschedule")
	}
	if strings.Contains(stripSGR(p.View(Rect{Width: 80, Height: 10})), "Working") {
		t.Errorf("completed pane must not show the working row")
	}
}

// TestEscInterruptsRunningTurn verifies Esc maps to the interrupt op while a
// task runs (and only then).
func TestEscInterruptsRunningTurn(t *testing.T) {
	p := newIdleBottomPane("x")

	// Idle Esc: no interrupt.
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		if _, ok := cmd().(CodexOpEvent); ok {
			t.Fatalf("idle Esc must not interrupt")
		}
	}

	next, _ := p.Update(coreEvent(protocol.EventMsgKindTurnStarted))
	p = next.(ChatBottomPane)
	_, cmd = p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatalf("running Esc must produce the interrupt command")
	}
	ev, ok := cmd().(CodexOpEvent)
	if !ok || ev.Command.Kind != AppCommandInterrupt {
		t.Fatalf("running Esc produced %#v, want interrupt CodexOpEvent", ev)
	}
}

// TestWorkedForSeparator verifies the completed-turn separator: only after
// work activity, labeled over 60s, plain rule at or under 60s.
func TestWorkedForSeparator(t *testing.T) {
	over := NewWorkedForSeparatorCell(701) // 11m 41s
	lines := over.Lines(60)
	if len(lines) != 1 {
		t.Fatalf("separator lines = %d, want 1", len(lines))
	}
	text := lines[0].Spans[0].Text
	if !strings.HasPrefix(text, "─ Worked for 11m 41s ─") {
		t.Errorf("label = %q, want prefix %q", text, "─ Worked for 11m 41s ─")
	}
	if got := len([]rune(text)); got != 60 {
		t.Errorf("separator width = %d, want 60 (full-width fill)", got)
	}

	under := NewWorkedForSeparatorCell(45)
	text = under.Lines(20)[0].Spans[0].Text
	if text != strings.Repeat("─", 20) {
		t.Errorf("short-turn separator = %q, want plain 20-wide rule", text)
	}
}

// TestSeparatorGatedOnWorkActivity verifies the transcript only appends the
// separator after turns that ran exec/tool work.
func TestSeparatorGatedOnWorkActivity(t *testing.T) {
	tr := NewChatTranscript(LoadTheme(nil, Capabilities{}))

	// Text-only turn: no separator.
	tr = tr.applyEvent(protocol.Event{Msg: protocol.EventMsg{Type: protocol.EventMsgKindTurnStarted}})
	tr = tr.applyEvent(protocol.Event{Msg: protocol.EventMsg{Type: protocol.EventMsgKindTurnComplete,
		TurnComplete: &protocol.TurnCompleteEvent{}}})
	for _, c := range tr.cells {
		if _, ok := c.(WorkedForSeparatorCell); ok {
			t.Fatalf("text-only turn must not append a separator")
		}
	}

	// Turn with exec work: separator appended with the event-supplied duration.
	tr = tr.applyEvent(protocol.Event{Msg: protocol.EventMsg{Type: protocol.EventMsgKindTurnStarted}})
	tr = tr.applyEvent(protocol.Event{Msg: protocol.EventMsg{Type: protocol.EventMsgKindExecCommandBegin,
		ExecCommandBegin: &protocol.ExecCommandBeginEvent{CallID: "c1", Command: []string{"ls"}}}})
	dur := int64(125000) // 2m 05s
	tr = tr.applyEvent(protocol.Event{Msg: protocol.EventMsg{Type: protocol.EventMsgKindTurnComplete,
		TurnComplete: &protocol.TurnCompleteEvent{DurationMs: &dur}}})

	var sep *WorkedForSeparatorCell
	for _, c := range tr.cells {
		if s, ok := c.(WorkedForSeparatorCell); ok {
			sep = &s
		}
	}
	if sep == nil {
		t.Fatalf("work turn must append the separator")
	}
	if sep.ElapsedSeconds != 125 {
		t.Errorf("elapsed = %ds, want 125", sep.ElapsedSeconds)
	}
}

// TestExecCommandFolds verifies a multi-line / overlong command renders as a
// single folded header line with a " [...]" suffix (no viewport-flooding wrap).
func TestExecCommandFolds(t *testing.T) {
	heredoc := "/bin/zsh -lc cat > f << EOF\n" + strings.Repeat("x\n", 200) + "EOF"
	cell := NewExecCell(LoadTheme(nil, Capabilities{}), []string{"/bin/zsh", "-lc", heredoc})
	lines := cell.Lines(60)
	// Header must be one line; without folding the heredoc would explode into
	// hundreds of wrapped rows. The first line is the command header.
	if len(lines) == 0 {
		t.Fatalf("no lines rendered")
	}
	header := ""
	for _, sp := range lines[0].Spans {
		header += sp.Text
	}
	if strings.Contains(header, "\n") {
		t.Fatalf("header contains newline (not folded): %q", header)
	}
	if w := runeDisplayWidth(header); w > 60 {
		t.Fatalf("header width %d exceeds 60: %q", w, header)
	}
	if !strings.Contains(header, "[...]") {
		t.Errorf("folded header should carry the [...] suffix: %q", header)
	}
}
