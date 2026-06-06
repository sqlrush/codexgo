package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// pickerBottomPane builds a bottom pane with a /model picker list wired.
func pickerBottomPane() (ChatBottomPane, *AppEventSender) {
	sender := NewAppEventSender()
	p := NewChatBottomPane(ChatBottomPaneConfig{
		Theme:  LoadTheme(nil, Capabilities{}),
		Sender: sender,
		Footer: ComposerFooter{Model: "gpt-5.5", Directory: "~/work"},
		Models: []ModelPickerEntry{
			{Slug: "gpt-5.5", DisplayName: "GPT-5.5", Description: "Frontier model"},
			{Slug: "glm-5.1", DisplayName: "GLM (Zhipu AI)"},
			{Slug: "deepseek-v4-pro", DisplayName: "DeepSeek"},
		},
	})
	return p, sender
}

// TestSlashModelOpensPicker verifies the OpenSlashOverlayEvent(SlashModel)
// pushes the picker overlay, which then owns rendering and key input.
func TestSlashModelOpensPicker(t *testing.T) {
	p, _ := pickerBottomPane()

	next, _ := p.Update(OpenSlashOverlayEvent{Command: SlashModel})
	p = next.(ChatBottomPane)
	if p.overlays.IsEmpty() {
		t.Fatalf("expected picker overlay to be pushed")
	}

	view := p.View(Rect{Width: 80, Height: 14})
	plain := stripSGR(view)
	for _, want := range []string{"Select model", "gpt-5.5", "glm-5.1", "deepseek-v4-pro"} {
		if !strings.Contains(plain, want) {
			t.Errorf("picker view missing %q:\n%s", want, plain)
		}
	}
	if !strings.Contains(plain, "(current)") {
		t.Errorf("picker view does not mark the current model:\n%s", plain)
	}
}

// TestSlashModelSelectionEmitsEventAndUpdatesFooter walks the picker to a
// custom-provider model, accepts it, and verifies the ModelSelectedEvent +
// footer sync.
func TestSlashModelSelectionEmitsEventAndUpdatesFooter(t *testing.T) {
	p, sender := pickerBottomPane()
	events := make(chan AppEvent, 4)
	sender.attachFunc(func(msg tea.Msg) {
		if ev, ok := msg.(AppEvent); ok {
			events <- ev
		}
	})

	next, _ := p.Update(OpenSlashOverlayEvent{Command: SlashModel})
	p = next.(ChatBottomPane)

	// Move from gpt-5.5 (current/initial) down to glm-5.1 and accept. The
	// accept callbacks are DEFERRED into the returned command (running them
	// inside Update deadlocks bubbletea's unbuffered Program.Send), so the
	// command must be executed for the event to fire.
	next, _ = p.Update(tea.KeyMsg{Type: tea.KeyDown})
	p = next.(ChatBottomPane)
	next, acceptCmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	p = next.(ChatBottomPane)
	if acceptCmd == nil {
		t.Fatalf("Enter on picker must return the deferred action command")
	}
	if msg := acceptCmd(); msg != nil {
		t.Fatalf("deferred action cmd returned unexpected msg %T", msg)
	}

	select {
	case ev := <-events:
		sel, ok := ev.(ModelSelectedEvent)
		if !ok {
			t.Fatalf("event = %T, want ModelSelectedEvent", ev)
		}
		if sel.Slug != "glm-5.1" {
			t.Fatalf("selected slug = %q, want glm-5.1", sel.Slug)
		}
		// The spine routes the event back to the pane for the footer sync.
		next, _ = p.Update(sel)
		p = next.(ChatBottomPane)
		if p.footer.Model != "glm-5.1" {
			t.Errorf("footer model = %q, want glm-5.1", p.footer.Model)
		}
	default:
		t.Fatalf("no ModelSelectedEvent emitted")
	}

	if !p.overlays.IsEmpty() {
		t.Errorf("picker overlay should dismiss on select")
	}
}

// TestUnwiredSlashCommandShowsNotice verifies delegated commands without an
// overlay surface a status notice instead of doing nothing.
func TestUnwiredSlashCommandShowsNotice(t *testing.T) {
	p, _ := pickerBottomPane()
	next, _ := p.Update(OpenSlashOverlayEvent{Command: SlashStatus})
	p = next.(ChatBottomPane)
	if !strings.Contains(p.status, "/status") || !strings.Contains(p.status, "not supported") {
		t.Errorf("status = %q, want /status not-supported notice", p.status)
	}
}

// TestPickerAcceptDoesNotSendSynchronously reproduces the reported TUI hang:
// the sender is attached to an UNBUFFERED consumer (the shape of bubbletea's
// Program.Send) with NO reader during Update — exactly the state of the real
// event loop while it is blocked inside Update. If the overlay ran its action
// callbacks synchronously, Update would block forever; the deferred-command
// fix makes Update return immediately and deliver via the command goroutine.
func TestPickerAcceptDoesNotSendSynchronously(t *testing.T) {
	p, sender := pickerBottomPane()
	blocked := make(chan tea.Msg) // unbuffered, mirrors Program.msgs
	sender.attachFunc(func(msg tea.Msg) { blocked <- msg })

	next, _ := p.Update(OpenSlashOverlayEvent{Command: SlashModel})
	p = next.(ChatBottomPane)

	done := make(chan tea.Cmd, 1)
	go func() {
		_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
		done <- cmd
	}()

	var cmd tea.Cmd
	select {
	case cmd = <-done:
		// Update returned without delivering — the deadlock is fixed.
	case <-time.After(3 * time.Second):
		t.Fatalf("Update blocked: overlay action sent synchronously into an unbuffered sender (TUI deadlock)")
	}

	// The deferred command delivers once a reader exists (the event loop after
	// Update returns).
	if cmd == nil {
		t.Fatalf("expected deferred action command")
	}
	go cmd()
	select {
	case msg := <-blocked:
		if sel, ok := msg.(ModelSelectedEvent); !ok || sel.Slug == "" {
			t.Errorf("delivered msg = %#v, want ModelSelectedEvent", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("deferred command never delivered the selection event")
	}
}
