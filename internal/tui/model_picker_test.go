package tui

import (
	"strings"
	"testing"

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

	// Move from gpt-5.5 (current/initial) down to glm-5.1 and accept.
	next, _ = p.Update(tea.KeyMsg{Type: tea.KeyDown})
	p = next.(ChatBottomPane)
	next, _ = p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	p = next.(ChatBottomPane)

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
