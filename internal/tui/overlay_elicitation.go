package tui

import (
	"encoding/json"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// ElicitationFieldKind discriminates a form field's input type.
type ElicitationFieldKind int

const (
	// ElicitationText is a free-text field.
	ElicitationText ElicitationFieldKind = iota
	// ElicitationSelect is a single-choice field.
	ElicitationSelect
)

// ElicitationOption is one choice in a select field.
//
// Port of mcp_server_elicitation.rs McpServerElicitationOption (Value carried as
// raw JSON).
type ElicitationOption struct {
	Label       string
	Description string
	Value       json.RawMessage
}

// ElicitationField is one field of an MCP elicitation form.
//
// Port of mcp_server_elicitation.rs McpServerElicitationField.
type ElicitationField struct {
	ID         string
	Label      string
	Prompt     string
	Required   bool
	Kind       ElicitationFieldKind
	Secret     bool
	Options    []ElicitationOption
	DefaultIdx int // selected option index for select fields; -1 for none
}

// ElicitationFormRequest is an MCP server elicitation form prompt.
//
// Port of mcp_server_elicitation.rs McpServerElicitationFormRequest (form-content
// mode; the tool-approval / tool-suggestion modes are deviations).
type ElicitationFormRequest struct {
	ThreadID   string
	ServerName string
	RequestID  string
	Message    string
	Fields     []ElicitationField
}

// elicitationFieldState holds per-field edit state.
type elicitationFieldState struct {
	selectState ScrollState
	text        string
}

// ElicitationFormOverlay collects answers for an MCP elicitation form and routes
// accept/decline/cancel back to the engine.
//
// Port of mcp_server_elicitation.rs form view. Esc maps to cancel (safe abort),
// matching the elicitation contract.
type ElicitationFormOverlay struct {
	sender   *AppEventSender
	request  ElicitationFormRequest
	fields   []elicitationFieldState
	focusIdx int
	done     bool
}

// NewElicitationFormOverlay builds the overlay for a form request.
func NewElicitationFormOverlay(request ElicitationFormRequest, sender *AppEventSender) *ElicitationFormOverlay {
	states := make([]elicitationFieldState, len(request.Fields))
	for i, f := range request.Fields {
		st := elicitationFieldState{selectState: ScrollState{Selected: -1}}
		if f.Kind == ElicitationSelect && len(f.Options) > 0 {
			if f.DefaultIdx >= 0 && f.DefaultIdx < len(f.Options) {
				st.selectState.Selected = f.DefaultIdx
			} else {
				st.selectState.Selected = 0
			}
		}
		states[i] = st
	}
	return &ElicitationFormOverlay{sender: sender, request: request, fields: states}
}

func (o *ElicitationFormOverlay) currentField() *ElicitationField {
	if o.focusIdx < 0 || o.focusIdx >= len(o.request.Fields) {
		return nil
	}
	return &o.request.Fields[o.focusIdx]
}

// accept builds the content payload and resolves the elicitation as accepted.
func (o *ElicitationFormOverlay) accept() {
	content := make(map[string]any, len(o.request.Fields))
	for i, f := range o.request.Fields {
		st := o.fields[i]
		switch f.Kind {
		case ElicitationSelect:
			if st.selectState.Selected >= 0 && st.selectState.Selected < len(f.Options) {
				opt := f.Options[st.selectState.Selected]
				if opt.Value != nil {
					content[f.ID] = opt.Value
				} else {
					content[f.ID] = opt.Label
				}
			}
		case ElicitationText:
			content[f.ID] = st.text
		}
	}
	raw, _ := json.Marshal(content)
	o.sender.ResolveElicitation(o.request.ThreadID, o.request.ServerName, o.request.RequestID, elicitationAccept, raw)
	o.done = true
}

func (o *ElicitationFormOverlay) decline() {
	o.sender.ResolveElicitation(o.request.ThreadID, o.request.ServerName, o.request.RequestID, elicitationDecline, nil)
	o.done = true
}

func (o *ElicitationFormOverlay) cancel() {
	o.sender.ResolveElicitation(o.request.ThreadID, o.request.ServerName, o.request.RequestID, elicitationCancel, nil)
	o.done = true
}

// HandleKey implements OverlayView.
func (o *ElicitationFormOverlay) HandleKey(msg tea.KeyMsg) tea.Cmd {
	key := msg.String()
	switch key {
	case "esc":
		o.cancel()
		return nil
	case "ctrl+d":
		o.decline()
		return nil
	case "tab", "down":
		o.advanceField(1)
		return nil
	case "shift+tab", "up":
		o.advanceField(-1)
		return nil
	case "enter":
		if o.focusIdx >= len(o.request.Fields)-1 {
			o.accept()
		} else {
			o.advanceField(1)
		}
		return nil
	}

	f := o.currentField()
	if f == nil {
		return nil
	}
	st := &o.fields[o.focusIdx]
	switch f.Kind {
	case ElicitationSelect:
		switch key {
		case "left", "k":
			st.selectState.MoveUpWrap(len(f.Options))
		case "right", "j":
			st.selectState.MoveDownWrap(len(f.Options))
		default:
			if len(msg.Runes) == 1 {
				r := msg.Runes[0]
				if r >= '1' && r <= '9' {
					idx := int(r-'0') - 1
					if idx < len(f.Options) {
						st.selectState.Selected = idx
					}
				}
			}
		}
	case ElicitationText:
		switch key {
		case "backspace":
			if len(st.text) > 0 {
				runes := []rune(st.text)
				st.text = string(runes[:len(runes)-1])
			}
		default:
			if len(msg.Runes) == 1 && !msg.Alt {
				st.text += string(msg.Runes)
			}
		}
	}
	return nil
}

func (o *ElicitationFormOverlay) advanceField(dir int) {
	n := len(o.request.Fields)
	if n == 0 {
		return
	}
	o.focusIdx = (o.focusIdx + dir + n) % n
}

// OnCtrlC implements overlayCtrlC: cancel is the safe abort.
func (o *ElicitationFormOverlay) OnCtrlC() CancellationEvent {
	o.cancel()
	return CancellationHandled
}

// IsComplete implements OverlayView.
func (o *ElicitationFormOverlay) IsComplete() bool { return o.done }

// TerminalTitleRequiresAction implements overlayTerminalTitle.
func (o *ElicitationFormOverlay) TerminalTitleRequiresAction() bool { return true }

// DesiredHeight implements OverlayView.
func (o *ElicitationFormOverlay) DesiredHeight(width int) int {
	h := 2 // server + message
	for _, f := range o.request.Fields {
		h++ // label/prompt
		if f.Kind == ElicitationSelect {
			h += len(f.Options)
		} else {
			h++
		}
	}
	return h + 1 // footer
}

// View implements OverlayView.
func (o *ElicitationFormOverlay) View(theme Theme, area Rect) string {
	var b strings.Builder
	b.WriteString(theme.UserMessage.Render(o.request.ServerName))
	b.WriteByte('\n')
	if o.request.Message != "" {
		b.WriteString(o.request.Message)
		b.WriteByte('\n')
	}
	for i, f := range o.request.Fields {
		focused := i == o.focusIdx
		label := f.Label
		if focused {
			label = theme.Accent().Bold(true).Render("› " + label)
		} else {
			label = "  " + label
		}
		b.WriteString(label)
		b.WriteByte('\n')
		st := o.fields[i]
		switch f.Kind {
		case ElicitationSelect:
			for j, opt := range f.Options {
				marker := "  "
				if st.selectState.Selected == j {
					marker = theme.Accent().Render("• ")
				}
				b.WriteString("    " + marker + opt.Label)
				b.WriteByte('\n')
			}
		case ElicitationText:
			val := st.text
			if f.Secret {
				val = strings.Repeat("•", len([]rune(st.text)))
			}
			if val == "" {
				val = theme.Dimmed().Render("(empty)")
			}
			b.WriteString("    " + val)
			b.WriteByte('\n')
		}
	}
	b.WriteString(theme.Dimmed().Render("tab to move · enter to submit · ctrl+d decline · esc cancel"))
	return b.String()
}

var _ OverlayView = (*ElicitationFormOverlay)(nil)
