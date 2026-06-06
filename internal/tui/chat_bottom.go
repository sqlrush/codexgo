package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/sqlrush/codexgo/internal/protocol"
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
	// footer holds the idle-composer status-line context (model · directory).
	footer ComposerFooter
	// colorLevel is the detected terminal color depth, used to resolve the
	// footer's softened theme accent colors (TrueColor → RGB, 256 → nearest
	// indexed, otherwise default fg).
	colorLevel StdoutColorLevel
	// maxPopupRows caps popup height so the pane never grows unbounded.
	maxPopupRows int
	// overlays is the stack of picker/list overlays opened by slash commands
	// (/model and friends). While non-empty, the top overlay owns key input and
	// the pane renders it instead of the composer.
	overlays OverlayStack
	// sender delivers app events from overlay action closures.
	sender *AppEventSender
	// models is the /model picker entry list supplied by the host.
	models []ModelPickerEntry

	// taskRunning tracks whether a turn is in flight; while true the pane
	// renders the live "Working (…)" status row above the composer (port of
	// StatusIndicatorWidget) and Esc interrupts the turn.
	taskRunning bool
	// taskStartedAt anchors the elapsed display.
	taskStartedAt time.Time
	// spinner is the working-status row state (blink frame + elapsed).
	spinner RunningSpinner
}

// ChatBottomPaneConfig parameterizes a [ChatBottomPane].
type ChatBottomPaneConfig struct {
	// Theme is the resolved theme.
	Theme Theme
	// FileSearch resolves @mention queries (may be nil to disable completion).
	FileSearch FileSearchFunc
	// Footer carries the idle-composer status-line context (model + reasoning +
	// directory) rendered beneath the input box. Mirrors codex's default
	// status line `model-with-reasoning · current-dir`.
	Footer ComposerFooter
	// ColorLevel is the detected terminal color depth, used to resolve the
	// footer's softened theme accent colors. Defaults to the no-color level (the
	// footer renders without accent colors) when unset.
	ColorLevel StdoutColorLevel
	// Sender delivers app events from overlay action closures (required for
	// slash-command overlays such as /model; nil disables them).
	Sender *AppEventSender
	// Models is the /model picker entry list (bundled catalog + custom-provider
	// models). Empty disables the /model picker.
	Models []ModelPickerEntry
}

// NewChatBottomPane builds a bottom pane.
func NewChatBottomPane(cfg ChatBottomPaneConfig) ChatBottomPane {
	return ChatBottomPane{
		theme:        cfg.Theme,
		composer:     NewComposer(cfg.Theme, cfg.FileSearch),
		footer:       cfg.Footer,
		colorLevel:   cfg.ColorLevel,
		maxPopupRows: 8,
		sender:       cfg.Sender,
		models:       cfg.Models,
	}
}

// Update implements BottomPane. It routes key events to the composer (or the
// active overlay) and turns the composer's result into app events.
func (p ChatBottomPane) Update(msg tea.Msg) (BottomPane, tea.Cmd) {
	switch m := msg.(type) {
	case TaskRunningMsg:
		p.composer = p.composer.SetTaskRunning(bool(m))
		return p, nil
	case StatusMsg:
		p.status = string(m)
		return p, nil
	case CoreEventMsg:
		return p.handleCoreEvent(m)
	case SpinnerTickMsg:
		if !p.taskRunning {
			return p, nil // task ended; let the tick loop die
		}
		p.spinner = p.spinner.Tick().WithElapsed(time.Since(p.taskStartedAt))
		return p, SpinnerTickCmd()
	case OpenSlashOverlayEvent:
		return p.handleSlashOverlay(m)
	case ModelSelectedEvent:
		// Keep the footer's model display in sync with the new selection.
		p.footer.Model = m.Slug
		return p, nil
	case tea.KeyMsg:
		if !p.overlays.IsEmpty() {
			stack, cmd := p.overlays.HandleKey(m)
			p.overlays = stack
			return p, cmd
		}
		// Esc interrupts the running turn (codex's interrupt_turn default
		// binding) when no popup/search owns the key.
		if m.Type == tea.KeyEsc && p.taskRunning &&
			!p.composer.PopupVisible() && !p.composer.SearchActive() {
			return p, EventCmd(CodexOpEvent{Command: NewInterruptCommand()})
		}
		return p.handleKey(m)
	}
	return p, nil
}

// handleCoreEvent tracks turn lifecycle events to drive the working-status row
// (the Rust ChatWidget flips its StatusIndicatorWidget on TaskStarted /
// TaskComplete; errors and aborts also clear it).
func (p ChatBottomPane) handleCoreEvent(ev CoreEventMsg) (BottomPane, tea.Cmd) {
	switch ev.Event.Msg.Type {
	case protocol.EventMsgKindTurnStarted:
		p.taskRunning = true
		p.taskStartedAt = time.Now()
		p.spinner = NewRunningSpinner(true).WithTrueColor(p.colorLevel == ColorLevelTrueColor)
		p.composer = p.composer.SetTaskRunning(true)
		return p, SpinnerTickCmd()
	case protocol.EventMsgKindTurnComplete,
		protocol.EventMsgKindTurnAborted,
		protocol.EventMsgKindError,
		protocol.EventMsgKindStreamError:
		p.taskRunning = false
		p.composer = p.composer.SetTaskRunning(false)
		return p, nil
	}
	return p, nil
}

// handleSlashOverlay opens the picker/overlay owned by a delegated slash
// command. Commands without a wired overlay surface a status notice instead of
// silently doing nothing.
func (p ChatBottomPane) handleSlashOverlay(ev OpenSlashOverlayEvent) (BottomPane, tea.Cmd) {
	switch ev.Command {
	case SlashModel:
		if len(p.models) == 0 || p.sender == nil {
			p.status = "no models available to pick from"
			return p, nil
		}
		params := BuildModelPickerParams(p.models, p.footer.Model)
		p.overlays = p.overlays.Push(NewListSelectionOverlay(params, p.sender))
		p.status = ""
		return p, nil
	default:
		name := slashCommandName[ev.Command]
		if name == "" {
			name = "command"
		}
		p.status = "/" + name + " is not supported yet"
		return p, nil
	}
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

// View implements BottomPane, rendering the bordered-less composer block (top
// padding, the prompt/textarea row(s), bottom padding), then the popup/search
// bar, and finally the footer status line.
//
// The idle layout mirrors codex's inline composer (chat_composer.rs
// render_with_mask): the textarea is inset top=1/bottom=1 inside a Min(3) block
// with a "› " prompt gutter, and the footer (default status line
// `model · directory`) renders one row below the block (FOOTER_SPACING_HEIGHT=0).
func (p ChatBottomPane) View(area Rect) string {
	if area.Width <= 0 || area.Height <= 0 {
		return ""
	}
	// An active overlay (e.g. the /model picker) replaces the composer block,
	// mirroring codex's bottom-pane view stack.
	if !p.overlays.IsEmpty() {
		return p.overlays.View(p.theme, area)
	}
	var rows []string

	// Live working-status row above the composer while a turn runs (port of
	// the StatusIndicatorWidget placement: status row → composer → footer).
	if p.taskRunning {
		rows = append(rows, p.spinner.Line(p.theme))
	}

	if query, preview, active := p.composer.CurrentSearch(); active {
		rows = append(rows, p.renderSearchBar(query, preview, area.Width))
	} else {
		rows = append(rows, p.renderComposerBlock(area.Width)...)
	}

	rows = append(rows, p.renderPopup(area.Width)...)
	rows = append(rows, p.renderFooter(area.Width)...)
	if p.status != "" {
		rows = append(rows, p.theme.StatusLine.Render(truncateTo(p.status, area.Width)))
	}

	// Clamp to the area height.
	if len(rows) > area.Height {
		rows = rows[len(rows)-area.Height:]
	}
	return strings.Join(rows, "\n")
}

// renderComposerBlock renders the composer block: a blank top-padding row, the
// prompt/textarea row(s), and a blank bottom-padding row. The empty buffer shows
// the dim idle placeholder on the prompt row.
//
// Port of the Min(3) composer_rect with a top=1/bottom=1 textarea inset
// (chat_composer.rs layout_areas). When the terminal background is known, the
// whole block is painted with the user-message surface color, mirroring
// codex's `Block::default().style(user_message_style()).render_ref(
// composer_rect, ...)` band.
func (p ChatBottomPane) renderComposerBlock(width int) []string {
	pad := ""
	if p.theme.HasComposerSurface {
		pad = p.surfaceStyle().Render(strings.Repeat(" ", max(width, 0)))
	}
	rows := []string{pad} // top padding
	rows = append(rows, p.renderPromptRows(width)...)
	rows = append(rows, pad) // bottom padding
	return rows
}

// surfaceStyle returns the composer surface background style.
func (p ChatBottomPane) surfaceStyle() lipgloss.Style {
	return lipgloss.NewStyle().Background(p.theme.ComposerSurfaceBg)
}

// renderPromptRows renders the textarea content rows with the "› " prompt
// gutter, or the dim placeholder when empty.
//
// codex styles ONLY the "›" marker (bold, no color); the gutter space and the
// textarea/placeholder content carry their own styling (chat_composer.rs renders
// the prompt span separately from the textarea content, with the space coming
// from the LIVE_PREFIX_COLS gutter). So the marker glyph is rendered with the
// bold ComposerPrompt style and the trailing gutter space is left unstyled.
//
// codex additionally parks the HARDWARE terminal cursor on the caret cell every
// draw (chat_composer.rs cursor_pos -> ratatui set_cursor), so the terminal
// renders its block cursor over the input. bubbletea v1 hides the hardware
// cursor for the program's lifetime and cannot reposition it, so the same
// visual is reproduced here as a reverse-video cell at the caret position
// (over the placeholder's first glyph when the buffer is empty).
func (p ChatBottomPane) renderPromptRows(width int) []string {
	text := p.composer.Text()
	// Per-span styles: when the terminal background is known every span carries
	// the surface background (codex paints the bg at the cell layer, so the
	// prompt/text/cursor spans all sit on the band), and each row is padded to
	// the full width so the band spans the terminal like composer_rect.
	promptStyle := p.theme.ComposerPrompt
	dimStyle := lipglossDim(p.theme)
	plainStyle := lipgloss.NewStyle()
	cursorStyle := composerCursorStyle
	if p.theme.HasComposerSurface {
		bg := p.theme.ComposerSurfaceBg
		promptStyle = promptStyle.Background(bg)
		dimStyle = dimStyle.Background(bg)
		plainStyle = plainStyle.Background(bg)
		cursorStyle = cursorStyle.Background(bg)
	}
	if text == "" {
		prompt := promptStyle.Render("›") + p.padSurface(" ")
		first, rest := splitFirstRune(p.composer.Placeholder())
		if first == "" {
			return []string{prompt + cursorStyle.Render(" ") + p.surfacePadding(width, 3)}
		}
		row := prompt + cursorStyle.Render(first) + dimStyle.Render(rest)
		used := 2 + runeDisplayWidth(first) + runeDisplayWidth(rest)
		return []string{row + p.surfacePadding(width, used)}
	}
	caretRow, caretCol := caretRowCol(text, p.composer.Cursor())
	var rows []string
	for i, line := range strings.Split(text, "\n") {
		marker := promptStyle.Render("›") + p.padSurface(" ")
		if i != 0 {
			marker = p.padSurface("  ")
		}
		visible := truncateTo(line, width-2)
		used := 2 + runeDisplayWidth(visible)
		if i == caretRow {
			row := marker + renderLineWithCursorStyled(visible, caretCol, plainStyle, cursorStyle)
			if caretCol >= len([]rune(visible)) {
				used++ // appended block cell
			}
			rows = append(rows, row+p.surfacePadding(width, used))
			continue
		}
		rows = append(rows, marker+plainStyle.Render(visible)+p.surfacePadding(width, used))
	}
	return rows
}

// padSurface renders s with the surface background when active, verbatim
// otherwise. Used for the prompt gutter spaces.
func (p ChatBottomPane) padSurface(s string) string {
	if !p.theme.HasComposerSurface {
		return s
	}
	return p.surfaceStyle().Render(s)
}

// surfacePadding fills the remainder of a width-wide composer row with the
// surface background. It renders nothing when the surface is inactive (the
// historical unpainted layout) or the row is already full.
func (p ChatBottomPane) surfacePadding(width, used int) string {
	if !p.theme.HasComposerSurface || used >= width {
		return ""
	}
	return p.surfaceStyle().Render(strings.Repeat(" ", width-used))
}

// runeDisplayWidth returns the display width of s in terminal cells.
func runeDisplayWidth(s string) int {
	return runewidth.StringWidth(s)
}

// composerCursorStyle is the reverse-video block standing in for the hardware
// cursor. SGR reverse matches the default terminal block-cursor look codex gets
// from set_cursor without assuming theme colors.
var composerCursorStyle = lipgloss.NewStyle().Reverse(true)

// caretRowCol converts a byte offset within text into a (row, rune-column)
// pair. Offsets are clamped to the text bounds.
func caretRowCol(text string, offset int) (row, col int) {
	if offset < 0 {
		offset = 0
	}
	if offset > len(text) {
		offset = len(text)
	}
	before := text[:offset]
	row = strings.Count(before, "\n")
	lineStart := strings.LastIndexByte(before, '\n') + 1
	col = utf8.RuneCountInString(before[lineStart:])
	return row, col
}

// renderLineWithCursorStyled renders line with the block cursor at rune column
// col, styling the surrounding text with textStyle (which carries the surface
// background when active). A caret past the last rune (the common end-of-input
// position) renders as a block on the cell after the text, exactly where the
// hardware cursor would sit. col is clamped into the visible line.
func renderLineWithCursorStyled(line string, col int, textStyle, cursorStyle lipgloss.Style) string {
	runes := []rune(line)
	if col >= len(runes) {
		return textStyle.Render(line) + cursorStyle.Render(" ")
	}
	if col < 0 {
		col = 0
	}
	return textStyle.Render(string(runes[:col])) +
		cursorStyle.Render(string(runes[col])) +
		textStyle.Render(string(runes[col+1:]))
}

// splitFirstRune splits s into its first rune (as a string) and the remainder.
func splitFirstRune(s string) (first, rest string) {
	if s == "" {
		return "", ""
	}
	_, size := utf8.DecodeRuneInString(s)
	return s[:size], s[size:]
}

// renderFooter renders the idle composer footer (default status line). It is
// suppressed while a popup or reverse-search bar is active, matching codex which
// replaces the footer hint area with the popup.
func (p ChatBottomPane) renderFooter(width int) []string {
	if _, _, active := p.composer.CurrentSearch(); active {
		return nil
	}
	if _, _, ok := p.composer.PopupRows(); ok {
		return nil
	}
	line := p.footer.styledLine(width, p.colorLevel)
	if len(line.Spans) == 0 {
		return nil
	}
	// Render the footer with the TrueColor renderer so the softened RGB accents
	// emit as 24-bit truecolor verbatim (codex/ratatui never downsample
	// Color::Rgb), instead of being quantized to a 256-indexed approximation by
	// lipgloss's auto-detected profile.
	return []string{line.RenderWith(trueColorRenderer)}
}

// renderSearchBar renders the Ctrl+R reverse-search line.
func (p ChatBottomPane) renderSearchBar(query, preview string, width int) string {
	label := fmt.Sprintf("(reverse-i-search)`%s': ", query)
	return lipglossDim(p.theme).Render(label) + truncateTo(preview, width-len([]rune(label)))
}

// renderPopup renders the active composer popup (slash or file search).
// PopupRows already windows the list to the visible rows with a
// window-relative selection (scroll-follow), so no truncation happens here.
func (p ChatBottomPane) renderPopup(width int) []string {
	items, selected, ok := p.composer.PopupRows()
	if !ok {
		return nil
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

// DesiredHeight implements BottomPane. The idle composer wants 4 rows
// (top-pad + prompt + bottom-pad + footer), matching codex's
// desired_height_with_textarea_right_reserve (textarea height + 2 + footer).
func (p ChatBottomPane) DesiredHeight(width int) int {
	if !p.overlays.IsEmpty() {
		return p.overlays.DesiredHeight(width)
	}
	if _, _, active := p.composer.CurrentSearch(); active {
		// Reverse-search collapses to a single bar row.
		return 1
	}

	textRows := 1
	if text := p.composer.Text(); text != "" {
		textRows = strings.Count(text, "\n") + 1
	}
	// Composer block: top padding + textarea rows + bottom padding.
	h := textRows + 2
	if p.taskRunning {
		h++ // live working-status row above the composer
	}

	if items, _, ok := p.composer.PopupRows(); ok {
		n := len(items)
		if n > p.maxPopupRows {
			n = p.maxPopupRows
		}
		h += n
	} else if p.footer.line(width) != "" {
		h++ // footer status line (only when no popup replaces it)
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
