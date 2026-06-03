package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// TaskRunningMsg signals the bottom pane that a turn is/ isn't in progress so the
// composer can gate Tab/queue behavior. Sent by the model when a turn starts or
// completes.
type TaskRunningMsg bool

// StatusMsg sets the bottom pane's status line text.
type StatusMsg string

// RequestDiffEvent asks the app to compute and show a git diff (the `/diff`
// command). The diff result arrives back as a [DiffResultEvent].
//
// Port of the /diff dispatch path that calls get_git_diff and emits
// AppEvent::DiffResult.
type RequestDiffEvent struct {
	BaseAppEvent
}

// OpenSlashOverlayEvent requests that a sibling area open the picker/overlay for
// a slash command the chat surface does not handle directly (e.g. /model,
// /theme, /status, /mcp). Area agents route on the Command field.
//
// Port of the slash_dispatch arms that open bottom-pane views.
type OpenSlashOverlayEvent struct {
	BaseAppEvent
	// Command is the slash command to open.
	Command SlashCommand
	// Args holds any inline argument text.
	Args string
}

// compile-time assertions.
var (
	_ AppEvent = RequestDiffEvent{}
	_ AppEvent = OpenSlashOverlayEvent{}
)

// DispatchedInput is the classification of a composer submission for dispatch.
// It mirrors [ParsedInput] but is named distinctly so the chat surface owns its
// own dispatch parsing semantics.
type DispatchedInput struct {
	// IsSlash reports whether the input was a recognized slash command.
	IsSlash bool
	// Command is the resolved slash command (valid when IsSlash).
	Command SlashCommand
	// Args holds inline argument text following the command.
	Args string
	// Text is the plain message text (valid when not IsSlash).
	Text string
}

// ParseInputForDispatch classifies a submitted composer string for dispatch. A
// leading "/name" that resolves to a known command (after feature gating) is a
// slash command; everything else is plain text.
//
// Port of the leading-slash detection + find_builtin_command lookup in the
// composer submission path.
func ParseInputForDispatch(raw string) DispatchedInput {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "/") || trimmed == "/" {
		return DispatchedInput{Text: raw}
	}
	body := trimmed[1:]
	name := body
	args := ""
	if idx := strings.IndexAny(body, " \t\n"); idx >= 0 {
		name = body[:idx]
		args = strings.TrimSpace(body[idx+1:])
	}
	cmd, ok := FindBuiltinCommand(strings.ToLower(name), BuiltinCommandFlags{})
	if !ok {
		return DispatchedInput{Text: raw}
	}
	return DispatchedInput{IsSlash: true, Command: cmd, Args: args}
}

// ExpandMentions rewrites bare "@path" mentions in submission text into the
// engine's mention syntax. The chat surface keeps the literal "@path" form,
// which the engine resolves; this hook exists so a future mention codec can be
// slotted in without touching the dispatch path.
//
// Port of the placeholder/mention expansion in chat_composer's submit path,
// reduced to a passthrough (the @path token is already the on-wire form here).
func ExpandMentions(text string) string {
	return text
}

// lipglossDim returns a dim-foreground lipgloss style for placeholder/secondary
// text, derived from the theme.
func lipglossDim(theme Theme) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.Dim)
}
