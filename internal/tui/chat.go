package tui

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sqlrush/codexgo/internal/protocol"
)

// ChatTranscript is the transcript-area implementation of [TranscriptView]. It
// owns the ordered list of committed [HistoryCell]s plus the transient active
// streaming cell (agent message or reasoning) and converts decoded engine events
// into cells.
//
// It is the Go/Elm analogue of the transcript half of codex-rs/tui/src/
// chatwidget (the parts that translate Event -> HistoryCell and stream agent
// markdown). Streaming uses a [MarkdownStreamCollector] so deltas commit on
// newline boundaries and the whole committed source re-renders on resize.
//
// ChatTranscript follows the immutability convention: Update/AppendCoreEvent
// return a new value.
type ChatTranscript struct {
	theme    Theme
	markdown MarkdownRenderer
	diff     DiffRenderer

	// cells are the committed history cells, oldest first.
	cells []HistoryCell

	// active streaming state.
	streaming  *streamState
	scrollOff  int // rows scrolled up from the bottom (0 = pinned to bottom)
	lastWidth  int
	lastHeight int

	// execIndex maps an in-flight exec call_id to its cell index.
	execIndex map[string]int
	// toolIndex maps an in-flight tool call_id to its cell index.
	toolIndex map[string]int

	// flushedCells counts committed cells already drained into native terminal
	// scrollback (inline-mode rendering). Cells before this index are NOT
	// re-rendered in the live viewport; they live above it in scrollback. See
	// DrainScrollback.
	flushedCells int
	// scrollbackEmitted records whether any cell has been flushed yet, so the
	// inter-cell leading blank line is only inserted between cells (matching
	// codex's display_lines_for_history_insert in app/resize_reflow.rs).
	scrollbackEmitted bool

	// turnStartedAt anchors the elapsed time for the completed-turn separator;
	// hadWorkActivity gates the separator to turns that ran exec/tool calls
	// (port of turn_runtime.rs had_work_activity).
	turnStartedAt   time.Time
	hadWorkActivity bool

	// renderedThisTurn dedups MCP direct-renders within a turn: a tool whose
	// rendered markdown matches one already shown this turn is skipped, so a tool
	// shown twice does not double-render. Reset on TurnStarted.
	renderedThisTurn map[string]bool

	// toolRenders stashes each model-invoked MCP tool's user-addressed render this
	// turn, keyed by lowercase tool name. Tool output is NOT auto-rendered; instead
	// the model declares which results are relevant via a trailing "@show: a, b"
	// line in its final reply, and only those tables are rendered (relevance gate).
	// Reset on TurnStarted.
	toolRenders map[string]string

	// header retains the session-header seed (version/model/directory) supplied to
	// WithSessionHeader so a fresh session after /clear can re-seed the same
	// welcome card. seeded records whether a header was ever attached, so
	// ResetForClear can faithfully rebuild the fresh transcript.
	header struct {
		version, model, directory string
		seeded                    bool
	}
}

// streamState tracks the in-flight streaming cell.
type streamState struct {
	kind      streamKind
	collector *MarkdownStreamCollector
	// rendered source committed so far (mirrors collector.CommittedSource).
	committed string
	// pending is the uncommitted trailing buffer (shown live, unparsed).
	pending string
}

// streamKind discriminates the active stream type.
type streamKind int

const (
	streamAgent streamKind = iota
	streamReasoning
)

// NewChatTranscript builds an empty transcript bound to a theme.
func NewChatTranscript(theme Theme) ChatTranscript {
	return ChatTranscript{
		theme:     theme,
		markdown:  NewMarkdownRenderer(theme),
		diff:      NewDiffRenderer(theme),
		execIndex: map[string]int{},
		toolIndex: map[string]int{},
	}
}

// WithSessionHeader returns a copy of the transcript with the session-header
// welcome card seeded as the first history cell, so a fresh session renders the
// bordered "OpenAI Codex" card at the top of the transcript (mirroring codex's
// SessionInfoCell header inserted into history on session start). It is a no-op
// when a cell is already present.
func (t ChatTranscript) WithSessionHeader(version, model, directory string) ChatTranscript {
	// Always retain the seed so a fresh session after /clear can re-render the
	// same welcome card, even if the header cell is already present.
	t.header.version = version
	t.header.model = model
	t.header.directory = directory
	t.header.seeded = true
	if len(t.cells) > 0 {
		return t
	}
	t.cells = t.appendCell(NewSessionHeaderCell(t.theme, version, model, directory))
	return t
}

// ResetForClear returns a brand-new transcript for a fresh session, discarding
// all committed cells, the active stream, and the scrollback-drain bookkeeping,
// then re-seeding the session-header welcome card (if one was originally
// attached). The returned transcript starts with flushedCells = 0 so its header
// drains back into native scrollback, mirroring codex's
// reset_transcript_state_after_clear + start_fresh_session (the fresh session
// re-inserts the SessionHeaderHistoryCell).
//
// Port of App::reset_transcript_state_after_clear followed by the fresh-session
// header insertion (app/history_ui.rs + session_lifecycle.rs).
func (t ChatTranscript) ResetForClear() TranscriptView {
	fresh := NewChatTranscript(t.theme)
	if t.header.seeded {
		fresh = fresh.WithSessionHeader(t.header.version, t.header.model, t.header.directory)
	}
	return fresh
}

// Update implements TranscriptView. It handles scroll keys; engine events arrive
// via AppendCoreEvent.
func (t ChatTranscript) Update(msg tea.Msg) (TranscriptView, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		t.lastWidth = m.Width
		return t, nil
	case McpToolResultMsg:
		// Deterministic slash→tool-call result: render below the echoed command.
		// Success is markdown-rendered (same as the model-invoked McpDirectCell
		// path) so headings/tables render and code fences are not shown literally;
		// errors keep the red notice styling.
		t = t.commitStream()
		body := m.Text
		if strings.TrimSpace(body) == "" {
			body = "(no output)"
		}
		if m.IsError {
			t.cells = t.appendCell(NewNoticeCell(t.theme, NoticeError, body))
		} else {
			t.cells = t.appendCell(NewMcpDirectCell(t.markdown, body))
		}
		return t, nil
	case tea.KeyMsg:
		switch m.Type {
		case tea.KeyPgUp:
			t.scrollOff += 5
			return t, nil
		case tea.KeyPgDown:
			t.scrollOff -= 5
			if t.scrollOff < 0 {
				t.scrollOff = 0
			}
			return t, nil
		}
	}
	return t, nil
}

// AppendCoreEvent implements TranscriptView, translating one decoded engine
// event into transcript changes.
//
// Port of the Event match arms in chatwidget that produce history cells and
// stream agent output.
func (t ChatTranscript) AppendCoreEvent(ev CoreEventMsg) TranscriptView {
	return t.applyEvent(ev.Event)
}

// AppendUserMessage echoes a submitted user message into history as a user cell,
// mirroring codex's on_user_message_display (chatwidget.rs): the submitted text
// is inserted as a UserHistoryCell at submit time, TUI-side, rather than waiting
// for a core event. An empty (whitespace-only) message is ignored.
//
// Any active stream is committed first so the echo lands after the prior turn's
// finalized output, matching add_to_history ordering.
func (t ChatTranscript) AppendUserMessage(text string) TranscriptView {
	if strings.TrimSpace(text) == "" {
		return t
	}
	t = t.commitStream()
	t.cells = t.appendCell(NewUserCell(t.theme, text))
	return t
}

// applyEvent dispatches on the event's message type.
func (t ChatTranscript) applyEvent(ev protocol.Event) ChatTranscript {
	switch ev.Msg.Type {
	case protocol.EventMsgKindTurnStarted:
		// Anchor the turn duration and reset the work-activity flag that gates
		// the completed-turn separator (turn_runtime.rs had_work_activity).
		t.turnStartedAt = time.Now()
		t.hadWorkActivity = false
		t.renderedThisTurn = nil
		t.toolRenders = nil

	case protocol.EventMsgKindUserMessage:
		if ev.Msg.UserMessage != nil && strings.TrimSpace(ev.Msg.UserMessage.Message) != "" {
			t = t.commitStream()
			t.cells = t.appendCell(NewUserCell(t.theme, ev.Msg.UserMessage.Message))
		}

	case protocol.EventMsgKindAgentMessageContentDelta:
		if ev.Msg.AgentMessageContentDelta != nil {
			t = t.pushDelta(streamAgent, ev.Msg.AgentMessageContentDelta.Delta)
		}

	case protocol.EventMsgKindAgentMessage:
		// A full (non-streamed) agent message: finalize any stream then commit.
		if ev.Msg.AgentMessage != nil {
			if t.streaming != nil && t.streaming.kind == streamAgent {
				t = t.commitStream()
			} else if strings.TrimSpace(ev.Msg.AgentMessage.Message) != "" {
				t = t.appendAgentText(strings.TrimRight(ev.Msg.AgentMessage.Message, "\n"))
			}
		}

	case protocol.EventMsgKindReasoningContentDelta:
		if ev.Msg.ReasoningContentDelta != nil {
			t = t.pushDelta(streamReasoning, ev.Msg.ReasoningContentDelta.Delta)
		}

	case protocol.EventMsgKindAgentReasoning:
		if t.streaming != nil && t.streaming.kind == streamReasoning {
			t = t.commitStream()
		} else if ev.Msg.AgentReasoning != nil && strings.TrimSpace(ev.Msg.AgentReasoning.Text) != "" {
			t.cells = t.appendCell(NewReasoningCell(t.theme, ev.Msg.AgentReasoning.Text))
		}

	case protocol.EventMsgKindExecCommandBegin:
		if ev.Msg.ExecCommandBegin != nil {
			t = t.commitStream()
			cell := NewExecCell(t.theme, ev.Msg.ExecCommandBegin.Command)
			t.cells = t.appendCell(cell)
			t.execIndex = cloneIndex(t.execIndex)
			t.execIndex[ev.Msg.ExecCommandBegin.CallID] = len(t.cells) - 1
			t.hadWorkActivity = true
		}

	case protocol.EventMsgKindExecCommandOutputDelta:
		if ev.Msg.ExecCommandOutputDelta != nil {
			t = t.appendExecOutput(ev.Msg.ExecCommandOutputDelta.CallID, string(ev.Msg.ExecCommandOutputDelta.Chunk))
		}

	case protocol.EventMsgKindExecCommandEnd:
		if ev.Msg.ExecCommandEnd != nil {
			t = t.completeExec(ev.Msg.ExecCommandEnd.CallID, ev.Msg.ExecCommandEnd.ExitCode, ev.Msg.ExecCommandEnd.AggregatedOutput)
		}

	case protocol.EventMsgKindMcpToolCallBegin:
		if ev.Msg.McpToolCallBegin != nil {
			t = t.commitStream()
			title := fmt.Sprintf("%s · %s", ev.Msg.McpToolCallBegin.Invocation.Server, ev.Msg.McpToolCallBegin.Invocation.Tool)
			t.cells = t.appendCell(NewToolCallCell(t.theme, title))
			t.toolIndex = cloneIndex(t.toolIndex)
			t.toolIndex[ev.Msg.McpToolCallBegin.CallID] = len(t.cells) - 1
			t.hadWorkActivity = true
		}

	case protocol.EventMsgKindMcpToolCallEnd:
		if ev.Msg.McpToolCallEnd != nil {
			end := ev.Msg.McpToolCallEnd
			t = t.completeTool(end.CallID, end.Result)
			// Two kinds of user-addressed output (see splitUserAudience):
			//   - autoShow: a deliberately-invoked report (health/sqltune/wdranalyze,
			//     audience user+assistant) — render NOW so it never vanishes.
			//   - gated: a per-call monitoring table (audience user-only) — stash it;
			//     the model declares the relevant ones via a trailing "@show: <tools>"
			//     line and only those render (see appendAgentText), so exploring with
			//     many tools doesn't flood the transcript.
			// The model still receives the full result (core.mcpResultText) either way.
			autoShow, gated := splitUserAudience(end.Result)
			if autoShow != "" && !t.renderedThisTurn[autoShow] {
				t = t.commitStream()
				t.renderedThisTurn = cloneStringSet(t.renderedThisTurn)
				t.renderedThisTurn[autoShow] = true
				t.cells = t.appendCell(NewMcpDirectCell(t.markdown, autoShow))
			}
			if gated != "" {
				t.toolRenders = cloneStringMap(t.toolRenders)
				t.toolRenders[strings.ToLower(strings.TrimSpace(end.Invocation.Tool))] = gated
			}
		}

	case protocol.EventMsgKindTurnDiff:
		if ev.Msg.TurnDiff != nil {
			t = t.commitStream()
			t.cells = t.appendCell(NewDiffCell(t.diff, ev.Msg.TurnDiff.UnifiedDiff))
		}

	case protocol.EventMsgKindPatchApplyBegin:
		if ev.Msg.PatchApplyBegin != nil {
			t = t.commitStream()
			t.cells = t.appendCell(NewFileChangesCell(t.diff, ev.Msg.PatchApplyBegin.Changes))
		}

	case protocol.EventMsgKindError, protocol.EventMsgKindStreamError:
		t = t.commitStream()
		msg := errorMessage(ev.Msg)
		if msg != "" {
			t.cells = t.appendCell(NewNoticeCell(t.theme, NoticeError, msg))
		}

	case protocol.EventMsgKindWarning, protocol.EventMsgKindGuardianWarning:
		w := ev.Msg.Warning
		if w == nil {
			w = ev.Msg.GuardianWarning
		}
		if w != nil && strings.TrimSpace(w.Message) != "" {
			t.cells = t.appendCell(NewNoticeCell(t.theme, NoticeWarning, w.Message))
		}

	case protocol.EventMsgKindTurnComplete, protocol.EventMsgKindTurnAborted:
		t = t.commitStream()
		// Completed-turn separator (FinalMessageSeparator): only after a turn
		// that performed real work (exec/tool calls), labeled with the elapsed
		// time when over a minute.
		if t.hadWorkActivity {
			elapsed := int64(0)
			if ev.Msg.Type == protocol.EventMsgKindTurnComplete &&
				ev.Msg.TurnComplete != nil && ev.Msg.TurnComplete.DurationMs != nil {
				elapsed = *ev.Msg.TurnComplete.DurationMs / 1000
			} else if !t.turnStartedAt.IsZero() {
				elapsed = int64(time.Since(t.turnStartedAt).Seconds())
			}
			t.cells = t.appendCell(NewWorkedForSeparatorCell(elapsed))
		}
		t.hadWorkActivity = false
	}
	return t
}

// pushDelta feeds a streaming delta into the active stream, starting one if
// needed, switching stream kinds when the type changes.
func (t ChatTranscript) pushDelta(kind streamKind, delta string) ChatTranscript {
	if t.streaming != nil && t.streaming.kind != kind {
		t = t.commitStream()
	}
	if t.streaming == nil {
		t.streaming = &streamState{kind: kind, collector: NewMarkdownStreamCollector()}
	} else {
		// copy to preserve immutability of the prior value
		s := *t.streaming
		t.streaming = &s
	}
	t.streaming.collector.PushDelta(delta)
	if _, ok := t.streaming.collector.CommitCompleteSource(); ok {
		t.streaming.committed = t.streaming.collector.CommittedSource()
	}
	t.streaming.pending = uncommittedTail(t.streaming.collector)
	return t
}

// commitStream finalizes any active streaming cell into a committed history cell.
func (t ChatTranscript) commitStream() ChatTranscript {
	if t.streaming == nil {
		return t
	}
	source := t.streaming.collector.CommittedSource() + t.streaming.collector.FinalizeAndDrainSource()
	source = strings.TrimRight(source, "\n")
	kind := t.streaming.kind
	t.streaming = nil
	switch kind {
	case streamReasoning:
		if strings.TrimSpace(source) != "" {
			t.cells = t.appendCell(NewReasoningCell(t.theme, source))
		}
		return t
	default:
		return t.appendAgentText(source)
	}
}

// appendAgentText commits a finalized agent message: it honors the relevance
// gate. The model declares which tool results to show the user via a trailing
// "@show: <tool>, <tool>" line; only those stashed renders (see toolRenders) are
// rendered — as the plugin's deterministic tables — before the analysis text,
// and the directive is stripped from the displayed text. A turn that stashed
// exactly one tool and declared nothing falls back to showing it (an unambiguous
// single-tool answer should not require the directive).
func (t ChatTranscript) appendAgentText(source string) ChatTranscript {
	cleaned, names := extractShowDirective(source)
	if len(names) == 0 && len(t.toolRenders) == 1 {
		for name := range t.toolRenders {
			names = []string{name}
		}
	}
	for _, name := range names {
		md, ok := t.toolRenders[name]
		if !ok || md == "" || t.renderedThisTurn[md] {
			continue
		}
		t.renderedThisTurn = cloneStringSet(t.renderedThisTurn)
		t.renderedThisTurn[md] = true
		t.cells = t.appendCell(NewMcpDirectCell(t.markdown, md))
	}
	if strings.TrimSpace(cleaned) != "" {
		t.cells = t.appendCell(NewAgentCell(t.markdown, cleaned))
	}
	return t
}

// showDirectiveRe matches a standalone declaration line "@show: a, b" (also
// tolerating 「[[show: ...]]」, full-width colon, leading quote/bullet). It
// requires an "@" or a "[" before "show" so a normal "show: ..." sentence does
// not falsely match. Group 1 is the comma/space/、-separated tool list.
var showDirectiveRe = regexp.MustCompile(`(?im)^[ \t>*\-]*(?:@\s*show|\[+\s*show)\s*[:：]\s*(.+?)\s*\]*\s*$`)

// extractShowDirective pulls the @show declaration out of an agent message,
// returning the message with that line removed and the normalized tool names the
// model asked to show (lowercased, stripped of any "mcp__server__"/"/" prefix).
func extractShowDirective(source string) (string, []string) {
	m := showDirectiveRe.FindStringSubmatch(source)
	if m == nil {
		return source, nil
	}
	cleaned := strings.TrimRight(showDirectiveRe.ReplaceAllString(source, ""), "\n")
	var names []string
	for _, tok := range strings.FieldsFunc(m[1], func(r rune) bool {
		return r == ',' || r == '，' || r == '、' || r == ' ' || r == '\t' || r == '/'
	}) {
		tok = strings.ToLower(strings.TrimSpace(tok))
		if i := strings.LastIndex(tok, "__"); i >= 0 {
			tok = tok[i+2:]
		}
		tok = strings.Trim(tok, "`\"'。.")
		if tok != "" {
			names = append(names, tok)
		}
	}
	return cleaned, names
}

// appendExecOutput appends streamed output to a running exec cell.
func (t ChatTranscript) appendExecOutput(callID, delta string) ChatTranscript {
	idx, ok := t.execIndex[callID]
	if !ok || idx >= len(t.cells) {
		return t
	}
	cell, ok := t.cells[idx].(ExecCell)
	if !ok {
		return t
	}
	t.cells = cloneCells(t.cells)
	t.cells[idx] = cell.AppendOutput(delta)
	return t
}

// completeExec marks a running exec cell complete.
func (t ChatTranscript) completeExec(callID string, exit int32, aggregated string) ChatTranscript {
	idx, ok := t.execIndex[callID]
	if !ok || idx >= len(t.cells) {
		return t
	}
	cell, ok := t.cells[idx].(ExecCell)
	if !ok {
		return t
	}
	if aggregated != "" {
		cell = NewExecCell(t.theme, cell.command).AppendOutput(aggregated)
	}
	t.cells = cloneCells(t.cells)
	t.cells[idx] = cell.Complete(exit)
	return t
}

// completeTool marks a running tool-call cell complete.
func (t ChatTranscript) completeTool(callID string, result []byte) ChatTranscript {
	idx, ok := t.toolIndex[callID]
	if !ok || idx >= len(t.cells) {
		return t
	}
	cell, ok := t.cells[idx].(ToolCallCell)
	if !ok {
		return t
	}
	text, failed := toolResultText(result)
	// On success the chip shows only its title (✔ server · tool): the model
	// receives the full tool result and summarizes it, so dumping the raw result
	// body here would be redundant noise. Failures still surface their body so the
	// user can see what broke.
	if !failed {
		text = ""
	}
	t.cells = cloneCells(t.cells)
	t.cells[idx] = cell.Complete(text, failed)
	return t
}

// appendCell returns a new cell slice with the cell appended (immutability).
func (t ChatTranscript) appendCell(c HistoryCell) []HistoryCell {
	return append(append([]HistoryCell(nil), t.cells...), c)
}

// View implements TranscriptView, rendering the cells (and active stream) into
// the area, applying scroll offset and pinning to the bottom.
func (t ChatTranscript) View(area Rect) string {
	if area.Width <= 0 || area.Height <= 0 {
		return ""
	}
	lines := t.allLines(area.Width)
	total := len(lines)
	height := area.Height

	// Determine the visible window: pinned to bottom minus scrollOff.
	end := total - t.scrollOff
	if end > total {
		end = total
	}
	if end < height {
		end = min(total, height)
	}
	start := end - height
	if start < 0 {
		start = 0
	}
	visible := lines[start:end]

	var b strings.Builder
	for i, l := range visible {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(l.Render())
	}
	// Pad to fill the area height so the bottom pane stays anchored.
	for i := len(visible); i < height; i++ {
		b.WriteByte('\n')
	}
	return b.String()
}

// allLines renders the cells NOT yet flushed to scrollback plus the active
// stream into wrapped lines. In alt-screen (non-inline) mode flushedCells is 0,
// so this renders the full transcript; in inline mode the drained cells live in
// native scrollback and only the tail (live) cells render here.
func (t ChatTranscript) allLines(width int) []Line {
	var out []Line
	first := t.flushedCells
	if first < 0 {
		first = 0
	}
	for i := first; i < len(t.cells); i++ {
		if i > first {
			out = append(out, Line{})
		}
		out = append(out, t.cells[i].Lines(width)...)
	}
	if t.streaming != nil {
		if len(t.cells) > first {
			out = append(out, Line{})
		}
		out = append(out, t.streamLines(width)...)
	}
	return out
}

// DrainScrollback renders the committed cells not yet flushed into native
// terminal scrollback and marks them flushed. It returns the lines (as plain
// strings ready for tea.Println) in insertion order, with a single blank line
// prepended before each cell after the first ever emitted — mirroring codex's
// display_lines_for_history_insert (a leading separator between history cells).
//
// It returns (nil, transcript-unchanged) when nothing new is pending. The active
// (streaming) cell is never drained; it stays in the live viewport until it
// commits. This is the inline-scrollback insertion path
// (codex-rs/tui/src/tui.rs insert_history_lines).
func (t ChatTranscript) DrainScrollback(width int) ([]string, TranscriptView) {
	if width <= 0 || t.flushedCells >= len(t.cells) {
		return nil, t
	}
	var lines []string
	emitted := t.scrollbackEmitted
	for i := t.flushedCells; i < len(t.cells); i++ {
		if emitted {
			lines = append(lines, "")
		}
		emitted = true
		for _, l := range t.cells[i].Lines(width) {
			lines = append(lines, l.Render())
		}
	}
	t.flushedCells = len(t.cells)
	t.scrollbackEmitted = emitted
	return lines, t
}

// streamLines renders the in-flight streaming content: committed source via the
// markdown renderer plus the uncommitted trailing buffer shown live.
func (t ChatTranscript) streamLines(width int) []Line {
	src := t.streaming.committed
	if t.streaming.pending != "" {
		src += t.streaming.pending
	}
	switch t.streaming.kind {
	case streamReasoning:
		return NewReasoningCell(t.theme, src).Lines(width)
	default:
		return NewAgentCell(t.markdown, src).Lines(width)
	}
}

// --- helpers ----------------------------------------------------------------

// uncommittedTail returns the buffer content after the last committed newline.
func uncommittedTail(c *MarkdownStreamCollector) string {
	full := c.buffer.String()
	committed := len(c.CommittedSource())
	if committed >= len(full) {
		return ""
	}
	return full[committed:]
}

// errorMessage extracts a human message from an error/stream-error event.
func errorMessage(m protocol.EventMsg) string {
	if m.Error != nil {
		return m.Error.Message
	}
	if m.StreamError != nil {
		return m.StreamError.Message
	}
	return ""
}

// toolResultText decodes an MCP tool result (Result<CallToolResult,String>) into
// display text and a failure flag. The raw payload is externally tagged
// ({"Ok":...} / {"Err":"..."}); we render it as best-effort text.
func toolResultText(raw []byte) (string, bool) {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return "", false
	}
	if strings.HasPrefix(s, "{\"Err\"") {
		return s, true
	}
	return s, false
}

// cloneCells returns a shallow copy of the cell slice.
func cloneCells(cells []HistoryCell) []HistoryCell {
	return append([]HistoryCell(nil), cells...)
}

// cloneIndex returns a copy of a call-id -> index map.
func cloneIndex(m map[string]int) map[string]int {
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// cloneStringSet returns a copy of a string set with room for one more entry
// (immutability for the turn-scoped MCP direct-render dedup). Handles a nil map.
func cloneStringSet(m map[string]bool) map[string]bool {
	out := make(map[string]bool, len(m)+1)
	for k := range m {
		out[k] = true
	}
	return out
}

// cloneStringMap returns a copy of a string map with room for one more entry
// (immutability for the turn-scoped tool-render stash). Handles a nil map.
func cloneStringMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	return out
}

// compile-time assertion.
var _ TranscriptView = ChatTranscript{}
