package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// UserInputOption is one selectable answer for a request_user_input question.
//
// Port of codex_app_server_protocol::ToolRequestUserInputOption.
type UserInputOption struct {
	Label       string
	Description string
}

// UserInputQuestion is one question in a request_user_input prompt.
//
// Port of ToolRequestUserInputQuestion (TUI-relevant fields).
type UserInputQuestion struct {
	ID       string
	Header   string
	Question string
	IsOther  bool
	Options  []UserInputOption
}

// UserInputRequest is a tool request_user_input prompt.
//
// Port of ToolRequestUserInputParams.
type UserInputRequest struct {
	ThreadID  string
	ItemID    string
	TurnID    string
	Questions []UserInputQuestion
}

// otherOptionLabel is the synthetic "None of the above" choice for is_other
// questions.
//
// Port of request_user_input/mod.rs OTHER_OPTION_LABEL.
const otherOptionLabel = "None of the above"

// answerState is the per-question UI state.
//
// Port of request_user_input/mod.rs AnswerState (notes reduced to plain text).
type answerState struct {
	optionsState    ScrollState
	notes           string
	answerCommitted bool
	notesVisible    bool
}

// uiFocus is whether the overlay is editing options or notes.
type uiFocus int

const (
	focusOptions uiFocus = iota
	focusNotes
)

// RequestUserInputOverlay is the multi-question request_user_input prompt.
//
// Port of request_user_input/mod.rs RequestUserInputOverlay. The shared chat
// composer is replaced by a plain notes buffer (the composer is owned by another
// area); the question/option/notes state machine and submission payload match
// the Rust behavior.
type RequestUserInputOverlay struct {
	sender   *AppEventSender
	request  UserInputRequest
	queue    []UserInputRequest
	answers  []answerState
	current  int
	focus    uiFocus
	done     bool
	notesBuf string
}

// NewRequestUserInputOverlay builds the overlay for an initial request.
func NewRequestUserInputOverlay(request UserInputRequest, sender *AppEventSender) *RequestUserInputOverlay {
	o := &RequestUserInputOverlay{sender: sender, request: request}
	o.resetForRequest()
	o.ensureFocusAvailable()
	o.restoreCurrentDraft()
	return o
}

// EnqueueRequest queues a follow-up request (FIFO).
//
// Port of try_consume_user_input_request.
func (o *RequestUserInputOverlay) EnqueueRequest(req UserInputRequest) {
	o.queue = append(o.queue, req)
}

func (o *RequestUserInputOverlay) resetForRequest() {
	o.answers = make([]answerState, len(o.request.Questions))
	for i, q := range o.request.Questions {
		st := ScrollState{Selected: -1}
		hasOpts := len(q.Options) > 0
		if hasOpts {
			st.Selected = 0
		}
		o.answers[i] = answerState{optionsState: st, notesVisible: !hasOpts}
	}
	o.current = 0
	o.focus = focusOptions
	o.notesBuf = ""
}

func (o *RequestUserInputOverlay) currentQuestion() *UserInputQuestion {
	if o.current < 0 || o.current >= len(o.request.Questions) {
		return nil
	}
	return &o.request.Questions[o.current]
}

func (o *RequestUserInputOverlay) currentAnswer() *answerState {
	if o.current < 0 || o.current >= len(o.answers) {
		return nil
	}
	return &o.answers[o.current]
}

func (o *RequestUserInputOverlay) questionCount() int { return len(o.request.Questions) }

func (o *RequestUserInputOverlay) hasOptions() bool {
	q := o.currentQuestion()
	return q != nil && len(q.Options) > 0
}

// optionsLen returns the number of selectable options including a synthetic
// "other" option when enabled.
func (o *RequestUserInputOverlay) optionsLen() int {
	q := o.currentQuestion()
	if q == nil {
		return 0
	}
	n := len(q.Options)
	if otherEnabled(q) {
		n++
	}
	return n
}

func otherEnabled(q *UserInputQuestion) bool { return q.IsOther && len(q.Options) > 0 }

func optionLabelForIndex(q *UserInputQuestion, idx int) (string, bool) {
	if idx < len(q.Options) {
		return q.Options[idx].Label, true
	}
	if idx == len(q.Options) && otherEnabled(q) {
		return otherOptionLabel, true
	}
	return "", false
}

func (o *RequestUserInputOverlay) selectedOptionIndex() int {
	if !o.hasOptions() {
		return -1
	}
	a := o.currentAnswer()
	if a == nil {
		return -1
	}
	return a.optionsState.Selected
}

func (o *RequestUserInputOverlay) ensureFocusAvailable() {
	if o.questionCount() == 0 {
		return
	}
	if !o.hasOptions() {
		o.focus = focusNotes
		if a := o.currentAnswer(); a != nil {
			a.notesVisible = true
		}
		return
	}
	if o.focus == focusNotes && !o.notesUIVisible() {
		o.focus = focusOptions
	}
}

func (o *RequestUserInputOverlay) notesUIVisible() bool {
	if !o.hasOptions() {
		return true
	}
	a := o.currentAnswer()
	if a == nil {
		return false
	}
	return a.notesVisible || strings.TrimSpace(o.notesBuf) != ""
}

func (o *RequestUserInputOverlay) saveCurrentDraft() {
	a := o.currentAnswer()
	if a == nil {
		return
	}
	if a.answerCommitted && a.notes != o.notesBuf {
		a.answerCommitted = false
	}
	a.notes = o.notesBuf
	if strings.TrimSpace(o.notesBuf) != "" {
		a.notesVisible = true
	}
}

func (o *RequestUserInputOverlay) restoreCurrentDraft() {
	a := o.currentAnswer()
	if a == nil {
		o.notesBuf = ""
		return
	}
	o.notesBuf = a.notes
}

func (o *RequestUserInputOverlay) moveQuestion(next bool) {
	n := o.questionCount()
	if n == 0 {
		return
	}
	o.saveCurrentDraft()
	if next {
		o.current = (o.current + 1) % n
	} else {
		o.current = (o.current + n - 1) % n
	}
	o.restoreCurrentDraft()
	o.ensureFocusAvailable()
}

func (o *RequestUserInputOverlay) jumpToQuestion(idx int) {
	if idx < 0 || idx >= o.questionCount() {
		return
	}
	o.saveCurrentDraft()
	o.current = idx
	o.restoreCurrentDraft()
	o.ensureFocusAvailable()
}

func (o *RequestUserInputOverlay) goNextOrSubmit() {
	if o.current+1 >= o.questionCount() {
		o.saveCurrentDraft()
		o.submitAnswers()
	} else {
		o.moveQuestion(true)
	}
}

// submitAnswers builds the response payload and dispatches it.
//
// Port of RequestUserInputOverlay::submit_answers.
func (o *RequestUserInputOverlay) submitAnswers() {
	o.saveCurrentDraft()
	answers := make(map[string]any, len(o.request.Questions))
	for idx, q := range o.request.Questions {
		a := o.answers[idx]
		var answerList []string
		hasOpts := len(q.Options) > 0
		if hasOpts && a.answerCommitted && a.optionsState.Selected >= 0 {
			if label, ok := optionLabelForIndex(&q, a.optionsState.Selected); ok {
				answerList = append(answerList, label)
			}
		}
		notes := ""
		if a.answerCommitted {
			notes = strings.TrimSpace(a.notes)
		}
		if notes != "" {
			answerList = append(answerList, "user_note: "+notes)
		}
		answers[q.ID] = map[string]any{"answers": answerList}
	}
	raw, _ := json.Marshal(map[string]any{"answers": answers})
	o.sender.UserInputAnswer(o.request.TurnID, raw)
	o.advanceQueueOrComplete()
}

func (o *RequestUserInputOverlay) advanceQueueOrComplete() {
	if len(o.queue) > 0 {
		o.request = o.queue[0]
		o.queue = o.queue[1:]
		o.resetForRequest()
		o.ensureFocusAvailable()
		o.restoreCurrentDraft()
		return
	}
	o.done = true
}

// HandleKey implements OverlayView.
//
// Port of RequestUserInputOverlay::handle_key_event (core paths; the confirm
// unanswered sub-modal and paste handling are omitted as deviations).
func (o *RequestUserInputOverlay) HandleKey(msg tea.KeyMsg) tea.Cmd {
	key := msg.String()

	// Esc clears notes back to options for option questions.
	if key == "esc" && o.hasOptions() && o.notesUIVisible() {
		o.clearNotesAndFocusOptions()
		return nil
	}

	// Question navigation is always available.
	switch key {
	case "ctrl+p", "pgup":
		o.moveQuestion(false)
		return nil
	case "ctrl+n", "pgdown":
		o.moveQuestion(true)
		return nil
	}
	if o.hasOptions() && o.focus == focusOptions {
		switch key {
		case "left", "h":
			o.moveQuestion(false)
			return nil
		case "right", "l":
			o.moveQuestion(true)
			return nil
		}
	}

	switch o.focus {
	case focusOptions:
		o.handleOptionsKey(msg)
	case focusNotes:
		o.handleNotesKey(msg)
	}
	return nil
}

func (o *RequestUserInputOverlay) handleOptionsKey(msg tea.KeyMsg) {
	n := o.optionsLen()
	a := o.currentAnswer()
	switch msg.String() {
	case "up", "k":
		if a != nil {
			a.optionsState.MoveUpWrap(n)
			a.answerCommitted = false
		}
	case "down", "j":
		if a != nil {
			a.optionsState.MoveDownWrap(n)
			a.answerCommitted = false
		}
	case " ":
		o.selectCurrentOption(true)
	case "backspace", "delete":
		o.clearSelection()
	case "tab":
		if o.selectedOptionIndex() >= 0 {
			o.focus = focusNotes
			if a != nil {
				a.notesVisible = true
			}
		}
	case "enter":
		if o.selectedOptionIndex() >= 0 {
			o.selectCurrentOption(true)
		}
		o.goNextOrSubmit()
	default:
		if len(msg.Runes) == 1 {
			if idx, ok := o.optionIndexForDigit(msg.Runes[0]); ok {
				if a != nil {
					a.optionsState.Selected = idx
				}
				o.selectCurrentOption(true)
				o.goNextOrSubmit()
			}
		}
	}
}

func (o *RequestUserInputOverlay) handleNotesKey(msg tea.KeyMsg) {
	notesEmpty := strings.TrimSpace(o.notesBuf) == ""
	key := msg.String()
	if o.hasOptions() && key == "tab" {
		o.clearNotesAndFocusOptions()
		return
	}
	if o.hasOptions() && key == "backspace" && notesEmpty {
		o.saveCurrentDraft()
		if a := o.currentAnswer(); a != nil {
			a.notesVisible = false
		}
		o.focus = focusOptions
		return
	}
	if o.hasOptions() && (key == "up" || key == "down") {
		n := o.optionsLen()
		a := o.currentAnswer()
		if a != nil {
			if key == "up" {
				a.optionsState.MoveUpWrap(n)
			} else {
				a.optionsState.MoveDownWrap(n)
			}
			a.answerCommitted = false
		}
		return
	}
	if a := o.currentAnswer(); a != nil {
		a.notesVisible = true
	}
	if key == "enter" {
		if a := o.currentAnswer(); a != nil {
			if o.hasOptions() {
				a.answerCommitted = true
			} else {
				a.answerCommitted = strings.TrimSpace(o.notesBuf) != ""
			}
			a.notes = o.notesBuf
		}
		o.goNextOrSubmit()
		return
	}
	// Plain text editing of the notes buffer.
	switch key {
	case "backspace":
		if len(o.notesBuf) > 0 {
			runes := []rune(o.notesBuf)
			o.notesBuf = string(runes[:len(runes)-1])
		}
	default:
		if len(msg.Runes) == 1 && !msg.Alt {
			o.notesBuf += string(msg.Runes)
		}
	}
	if a := o.currentAnswer(); a != nil {
		a.answerCommitted = false
	}
}

func (o *RequestUserInputOverlay) optionIndexForDigit(r rune) (int, bool) {
	if !o.hasOptions() || r < '1' || r > '9' {
		return 0, false
	}
	idx := int(r-'0') - 1
	if idx < o.optionsLen() {
		return idx, true
	}
	return 0, false
}

func (o *RequestUserInputOverlay) selectCurrentOption(committed bool) {
	if !o.hasOptions() {
		return
	}
	if a := o.currentAnswer(); a != nil {
		a.optionsState.ClampSelection(o.optionsLen())
		a.answerCommitted = committed
	}
}

func (o *RequestUserInputOverlay) clearSelection() {
	if !o.hasOptions() {
		return
	}
	if a := o.currentAnswer(); a != nil {
		a.optionsState.Reset()
		a.notes = ""
		a.answerCommitted = false
		a.notesVisible = false
	}
	o.notesBuf = ""
}

func (o *RequestUserInputOverlay) clearNotesAndFocusOptions() {
	if !o.hasOptions() {
		return
	}
	if a := o.currentAnswer(); a != nil {
		a.notes = ""
		a.answerCommitted = false
		a.notesVisible = false
	}
	o.notesBuf = ""
	o.focus = focusOptions
}

// OnCtrlC implements overlayCtrlC: clear notes if present, else interrupt and
// finish.
//
// Port of RequestUserInputOverlay::on_ctrl_c.
func (o *RequestUserInputOverlay) OnCtrlC() CancellationEvent {
	if o.focus == focusNotes && strings.TrimSpace(o.notesBuf) != "" {
		o.notesBuf = ""
		if a := o.currentAnswer(); a != nil {
			a.notes = ""
			a.answerCommitted = false
			a.notesVisible = true
		}
		return CancellationHandled
	}
	o.sender.Interrupt()
	o.done = true
	return CancellationHandled
}

// PrefersEscToHandleKey implements overlayPrefersEscToKey.
func (o *RequestUserInputOverlay) PrefersEscToHandleKey() bool { return true }

// IsComplete implements OverlayView.
func (o *RequestUserInputOverlay) IsComplete() bool { return o.done }

// TerminalTitleRequiresAction implements overlayTerminalTitle.
func (o *RequestUserInputOverlay) TerminalTitleRequiresAction() bool { return true }

// DesiredHeight implements OverlayView.
func (o *RequestUserInputOverlay) DesiredHeight(width int) int {
	q := o.currentQuestion()
	if q == nil {
		return 1
	}
	h := 1 // header/question line
	if o.hasOptions() {
		h += o.optionsLen()
	}
	if o.notesUIVisible() {
		h += 3 // notes input region
	}
	h++ // footer tips
	return h
}

// View implements OverlayView.
func (o *RequestUserInputOverlay) View(theme Theme, area Rect) string {
	q := o.currentQuestion()
	if q == nil {
		return ""
	}
	var b strings.Builder
	if o.questionCount() > 1 {
		b.WriteString(theme.Dimmed().Render(fmt.Sprintf("Question %d of %d", o.current+1, o.questionCount())))
		b.WriteByte('\n')
	}
	if q.Header != "" {
		b.WriteString(theme.UserMessage.Render(q.Header))
		b.WriteByte('\n')
	}
	b.WriteString(q.Question)
	b.WriteByte('\n')

	if o.hasOptions() {
		sel := o.selectedOptionIndex()
		for i := 0; i < len(q.Options); i++ {
			b.WriteString(renderUserInputOption(theme, i, q.Options[i].Label, q.Options[i].Description, i == sel))
			b.WriteByte('\n')
		}
		if otherEnabled(q) {
			idx := len(q.Options)
			b.WriteString(renderUserInputOption(theme, idx, otherOptionLabel, "", idx == sel))
			b.WriteByte('\n')
		}
	}
	if o.notesUIVisible() {
		notes := o.notesBuf
		if notes == "" {
			notes = theme.Dimmed().Render("Add notes")
		}
		b.WriteString(theme.Dimmed().Render("Notes: ") + notes)
		b.WriteByte('\n')
	}
	b.WriteString(theme.Dimmed().Render("enter to submit · ctrl+c to interrupt"))
	return b.String()
}

func renderUserInputOption(theme Theme, idx int, label, desc string, selected bool) string {
	prefix := "  "
	if selected {
		prefix = "› "
	}
	name := fmt.Sprintf("%s%d. %s", prefix, idx+1, label)
	if selected {
		name = theme.Accent().Bold(true).Render(name)
	}
	if desc != "" {
		name += theme.Dimmed().Render("  " + desc)
	}
	return name
}

var _ OverlayView = (*RequestUserInputOverlay)(nil)
