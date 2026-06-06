package tui

// /model selection and /new session handling on the app spine: persist the
// choice (host callback), start a fresh thread with the model override (the
// model→provider routing then picks the right backend), and reset the UI the
// same way /clear does.

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// NewSessionEvent and ModelSelectedEvent handlers.

// handleModelSelected persists the picked model and restarts the session on it.
func (m Model) handleModelSelected(ev ModelSelectedEvent) (tea.Model, tea.Cmd) {
	slug := strings.TrimSpace(ev.Slug)
	if slug == "" {
		return m, nil
	}

	var notices []tea.Cmd
	if m.persistModel != nil {
		if err := m.persistModel(slug); err != nil {
			notices = append(notices, MsgCmd(StatusMsg("model selection not saved: "+err.Error())))
		}
	}

	mm, cmd := m.startFreshSession(slug)
	model := mm.(Model)
	// Update the footer's model display (delegated to the bottom pane).
	model.bottom, _ = model.bottom.Update(ModelSelectedEvent{Slug: slug})
	notices = append(notices, cmd, MsgCmd(StatusMsg("model set to "+slug)))
	return model, tea.Batch(notices...)
}

// handleNewSession starts a fresh thread with the configured defaults (/new).
func (m Model) handleNewSession() (tea.Model, tea.Cmd) {
	return m.startFreshSession("")
}

// startFreshSession spawns a new engine thread (optionally overriding the
// model), re-seeds the transcript header, and replays the /clear screen reset
// so the new session starts on a clean live region.
func (m Model) startFreshSession(modelSlug string) (tea.Model, tea.Cmd) {
	// Re-seed the transcript header so the welcome card shows the new model.
	if modelSlug != "" {
		if t, ok := m.transcript.(ChatTranscript); ok {
			t.header.model = modelSlug
			m.transcript = t
		}
	}

	mm, clearCmd := m.handleClearUI()
	model := mm.(Model)

	engine := model.engine
	sender := model.sender
	restart := func() tea.Msg {
		go func() {
			if engine == nil {
				return
			}
			if _, err := engine.StartNewThread(context.Background(), modelSlug); err != nil {
				sender.SendMsg(EngineErrorMsg{Err: err})
			}
		}()
		return nil
	}
	return model, tea.Sequence(clearCmd, restart)
}

// MsgCmd wraps a tea.Msg into a command.
func MsgCmd(msg tea.Msg) tea.Cmd {
	return func() tea.Msg { return msg }
}
