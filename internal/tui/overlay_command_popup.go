package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// aliasCommands are hidden from the default (empty-filter) popup list so each
// unique action appears once.
//
// Port of command_popup.rs ALIAS_COMMANDS.
var aliasCommands = map[SlashCommand]bool{
	SlashQuit: true,
	SlashBtw:  true,
}

// CommandPopup is the slash-command popup shown when the composer line starts
// with '/'.
//
// Port of command_popup.rs CommandPopup. ServiceTier commands and rich
// rendering are omitted (cosmetic); the behavioral filtering, navigation, and
// selection are preserved.
type CommandPopup struct {
	filter   string
	commands []SlashCommand
	state    ScrollState
	selected SlashCommand
	accepted bool
	complete bool
}

// NewCommandPopup builds the popup from the gated built-in command set.
//
// Port of CommandPopup::new (built-in path; debug-prefixed commands and /apps
// are filtered out of the popup menu).
func NewCommandPopup(flags BuiltinCommandFlags) *CommandPopup {
	var commands []SlashCommand
	for _, cmd := range BuiltinsForInput(flags) {
		name := cmd.Command()
		if strings.HasPrefix(name, "debug") || cmd == SlashApps {
			continue
		}
		commands = append(commands, cmd)
	}
	p := &CommandPopup{commands: commands, state: NewScrollState()}
	p.OnComposerTextChange("/")
	return p
}

// OnComposerTextChange updates the filter from the current composer text. The
// text is expected to start with '/'; everything after the first '/' on the
// first line up to whitespace becomes the filter.
//
// Port of CommandPopup::on_composer_text_change.
func (p *CommandPopup) OnComposerTextChange(text string) {
	firstLine := text
	if idx := strings.IndexByte(text, '\n'); idx >= 0 {
		firstLine = text[:idx]
	}
	if rest, ok := strings.CutPrefix(firstLine, "/"); ok {
		token := strings.TrimLeft(rest, " \t")
		if sp := strings.IndexAny(token, " \t"); sp >= 0 {
			token = token[:sp]
		}
		p.filter = token
	} else {
		p.filter = ""
	}
	n := len(p.filteredItems())
	p.state.ClampSelection(n)
	visible := MaxPopupRows
	if n < visible {
		visible = n
	}
	p.state.EnsureVisible(n, visible)
}

// filteredItems returns the commands matching the current filter, in
// exact-then-prefix order (matching the Rust filtering).
//
// Port of CommandPopup::filtered_items.
func (p *CommandPopup) filteredItems() []SlashCommand {
	filter := strings.TrimSpace(p.filter)
	if filter == "" {
		out := make([]SlashCommand, 0, len(p.commands))
		for _, cmd := range p.commands {
			if aliasCommands[cmd] {
				continue
			}
			out = append(out, cmd)
		}
		return out
	}

	fl := strings.ToLower(filter)
	var exact, prefix []SlashCommand
	for _, cmd := range p.commands {
		name := strings.ToLower(cmd.Command())
		if name == fl {
			exact = append(exact, cmd)
		} else if strings.HasPrefix(name, fl) {
			prefix = append(prefix, cmd)
		}
	}
	return append(exact, prefix...)
}

// MoveUp moves the selection up one row.
func (p *CommandPopup) MoveUp() {
	n := len(p.filteredItems())
	p.state.MoveUpWrap(n)
	p.state.EnsureVisible(n, minInt(MaxPopupRows, n))
}

// MoveDown moves the selection down one row.
func (p *CommandPopup) MoveDown() {
	n := len(p.filteredItems())
	p.state.MoveDownWrap(n)
	p.state.EnsureVisible(n, minInt(MaxPopupRows, n))
}

// SelectedItem returns the currently selected command, if any.
//
// Port of CommandPopup::selected_item.
func (p *CommandPopup) SelectedItem() (SlashCommand, bool) {
	items := p.filteredItems()
	if !p.state.HasSelection() || p.state.Selected >= len(items) {
		return 0, false
	}
	return items[p.state.Selected], true
}

// Accepted reports whether a command was accepted, and which one.
func (p *CommandPopup) Accepted() (SlashCommand, bool) {
	if !p.accepted {
		return 0, false
	}
	return p.selected, true
}

// HandleKey implements OverlayView.
func (p *CommandPopup) HandleKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "up", "ctrl+p":
		p.MoveUp()
	case "down", "ctrl+n":
		p.MoveDown()
	case "enter", "tab":
		if cmd, ok := p.SelectedItem(); ok {
			p.selected = cmd
			p.accepted = true
			p.complete = true
		}
	case "esc":
		p.complete = true
	}
	return nil
}

// IsComplete implements OverlayView.
func (p *CommandPopup) IsComplete() bool { return p.complete }

// DesiredHeight implements OverlayView.
func (p *CommandPopup) DesiredHeight(width int) int {
	n := len(p.filteredItems())
	if n == 0 {
		return 1
	}
	if n > MaxPopupRows {
		return MaxPopupRows
	}
	return n
}

// View implements OverlayView.
func (p *CommandPopup) View(theme Theme, area Rect) string {
	items := p.filteredItems()
	rows := make([]DisplayRow, 0, len(items))
	filterLen := len([]rune(strings.TrimSpace(p.filter)))
	for _, cmd := range items {
		var indices []int
		if filterLen > 0 {
			// Highlight the matched prefix (offset 1 accounts for the leading '/').
			for i := 0; i < filterLen; i++ {
				indices = append(indices, i+1)
			}
		}
		rows = append(rows, DisplayRow{
			Name:         "/" + cmd.Command(),
			MatchIndices: indices,
			Description:  cmd.Description(),
		})
	}
	return renderRows(theme, rows, p.state, MaxPopupRows, "no matches")
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var _ OverlayView = (*CommandPopup)(nil)
