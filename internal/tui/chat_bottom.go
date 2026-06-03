package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// ChatBottomPane is the bottom-pane implementation of [BottomPane]. It wraps a
// [Composer], renders the input box / popups / reverse-search bar / status line,
// and translates submissions into [AppEvent]s for the model loop.
//
// It is the Go/Elm analogue of the composer-owning half of codex-rs/tui/src/
// bottom_pane. Slash commands map to engine ops or app events; plain text and
// @mention-expanded text submit as a user turn.
//
// ChatBottomPane follows the immutability convention: Update returns a new value.
type ChatBottomPane struct {
	theme    Theme
	composer Composer
	status   string
	// maxPopupRows caps popup height so the pane never grows unbounded.
	maxPopupRows int
}

// ChatBottomPaneConfig parameterizes a [ChatBottomPane].
type ChatBottomPaneConfig struct {
	// Theme is the resolved theme.
	Theme Theme
	// FileSearch resolves @mention queries (may be nil to disable completion).
	FileSearch FileSearchFunc
}

// NewChatBottomPane builds a bottom pane.
func NewChatBottomPane(cfg ChatBottomPaneConfig) ChatBottomPane {
	return ChatBottomPane{
		theme:        cfg.Theme,
		composer:     NewComposer(cfg.Theme, cfg.FileSearch),
		maxPopupRows: 8,
	}
}

// Update implements BottomPane. It routes key events to the composer and turns
// the composer's result into app events.
func (p ChatBottomPane) Update(msg tea.Msg) (BottomPane, tea.Cmd) {
	switch m := msg.(type) {
	case TaskRunningMsg:
		p.composer = p.composer.SetTaskRunning(bool(m))
		return p, nil
	case StatusMsg:
		p.status = string(m)
		return p, nil
	case tea.KeyMsg:
		return p.handleKey(m)
	}
	return p, nil
}

// handleKey processes a key event through the composer and dispatches the result.
func (p ChatBottomPane) handleKey(msg tea.KeyMsg) (BottomPane, tea.Cmd) {
	res := p.composer.HandleKey(msg)
	p.composer = res.Composer

	var cmds []tea.Cmd
	if res.Cmd != nil {
		cmds = append(cmds, res.Cmd)
	}
	if res.Submit != "" {
		cmds = append(cmds, p.dispatchSubmission(res.Submit, res.Queue))
	}
	return p, tea.Batch(cmds...)
}

// dispatchSubmission turns a submitted string into the right app event: a slash
// command maps to an op/app event; plain text submits as a user turn.
//
// Port of the submission fork in chat_composer + slash_dispatch.
func (p ChatBottomPane) dispatchSubmission(text string, queue bool) tea.Cmd {
	parsed := ParseInputForDispatch(text)
	if parsed.IsSlash {
		return p.dispatchSlash(parsed)
	}
	expanded := ExpandMentions(parsed.Text)
	return SubmitUserMessageCmd(expanded)
}

// dispatchSlash maps a parsed slash command to its app event/op.
func (p ChatBottomPane) dispatchSlash(parsed DispatchedInput) tea.Cmd {
	switch parsed.Command {
	case SlashQuit, SlashExit:
		return ExitCmd(ExitShutdownFirst)
	case SlashClear:
		return EventCmd(ClearUIEvent{})
	case SlashNew:
		return EventCmd(NewSessionEvent{})
	case SlashCompact:
		return CodexOpCmd(NewCompactCommand())
	case SlashDiff:
		return EventCmd(RequestDiffEvent{})
	case SlashRename:
		if parsed.Args != "" {
			return CodexOpCmd(NewSetThreadNameCommand(parsed.Args))
		}
		return nil
	case SlashReview:
		// Submit the review target as a user turn so the engine starts a review.
		if parsed.Args != "" {
			return SubmitUserMessageCmd("/review " + parsed.Args)
		}
		return SubmitUserMessageCmd("/review")
	default:
		// Other commands open pickers/overlays owned by sibling area agents; emit
		// a generic open event they can handle.
		return EventCmd(OpenSlashOverlayEvent{Command: parsed.Command, Args: parsed.Args})
	}
}

// View implements BottomPane, rendering the composer box, popup, search bar, and
// status line.
func (p ChatBottomPane) View(area Rect) string {
	if area.Width <= 0 || area.Height <= 0 {
		return ""
	}
	var rows []string

	if query, preview, active := p.composer.CurrentSearch(); active {
		rows = append(rows, p.renderSearchBar(query, preview, area.Width))
	} else {
		rows = append(rows, p.renderComposer(area.Width)...)
	}

	rows = append(rows, p.renderPopup(area.Width)...)
	if p.status != "" {
		rows = append(rows, p.theme.StatusLine.Render(truncateTo(p.status, area.Width)))
	}

	// Clamp to the area height.
	if len(rows) > area.Height {
		rows = rows[len(rows)-area.Height:]
	}
	return strings.Join(rows, "\n")
}

// renderComposer renders the input buffer with a prompt marker on the first row.
func (p ChatBottomPane) renderComposer(width int) []string {
	prompt := p.theme.ComposerPrompt.Render("› ")
	text := p.composer.Text()
	if text == "" {
		placeholder := lipglossDim(p.theme).Render("Send a message (/ for commands, @ to mention a file)")
		return []string{prompt + placeholder}
	}
	var rows []string
	for i, line := range strings.Split(text, "\n") {
		marker := "  "
		if i == 0 {
			marker = "› "
		}
		rows = append(rows, p.theme.ComposerPrompt.Render(marker)+truncateTo(line, width-2))
	}
	return rows
}

// renderSearchBar renders the Ctrl+R reverse-search line.
func (p ChatBottomPane) renderSearchBar(query, preview string, width int) string {
	label := fmt.Sprintf("(reverse-i-search)`%s': ", query)
	return lipglossDim(p.theme).Render(label) + truncateTo(preview, width-len([]rune(label)))
}

// renderPopup renders the active composer popup (slash or file search).
func (p ChatBottomPane) renderPopup(width int) []string {
	items, selected, ok := p.composer.PopupRows()
	if !ok {
		return nil
	}
	if len(items) > p.maxPopupRows {
		items = items[:p.maxPopupRows]
	}
	var rows []string
	for i, it := range items {
		marker := "  "
		style := lipglossDim(p.theme)
		if i == selected {
			marker = "▸ "
			style = p.theme.SystemMessage
		}
		label := it.Label
		if it.Detail != "" {
			label = fmt.Sprintf("%-16s %s", it.Label, it.Detail)
		}
		rows = append(rows, style.Render(marker+truncateTo(label, width-2)))
	}
	return rows
}

// DesiredHeight implements BottomPane.
func (p ChatBottomPane) DesiredHeight(width int) int {
	h := 1
	if query, _, active := p.composer.CurrentSearch(); active {
		_ = query
		h = 1
	} else {
		text := p.composer.Text()
		if text == "" {
			h = 1
		} else {
			h = strings.Count(text, "\n") + 1
		}
	}
	if items, _, ok := p.composer.PopupRows(); ok {
		n := len(items)
		if n > p.maxPopupRows {
			n = p.maxPopupRows
		}
		h += n
	}
	if p.status != "" {
		h++
	}
	if h < 1 {
		h = 1
	}
	return h
}

// truncateTo truncates s to at most width display cells, appending an ellipsis
// when truncated.
func truncateTo(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if len([]rune(s)) <= width {
		return s
	}
	runes := []rune(s)
	if width <= 1 {
		return string(runes[:width])
	}
	return string(runes[:width-1]) + "…"
}

// compile-time assertion.
var _ BottomPane = ChatBottomPane{}
