package tui

import (
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

// Composer is the bottom-pane text input state machine: a multiline editor with
// slash-command and @file-search popups, shell-style history recall (Up/Down and
// Ctrl+R reverse search), and submit/queue handling.
//
// It is the Go/Elm analogue of codex-rs/tui/src/bottom_pane/chat_composer.rs.
// Where the Rust composer uses a rich TextArea widget and a paste-burst detector,
// this port uses a self-contained rune buffer with explicit cursor handling so
// the editing behavior is fully testable and deterministic. Cosmetic deviations
// (no bracketed-paste burst heuristics; large pastes are inserted inline rather
// than as placeholder elements) are documented on the relevant methods.
//
// Composer follows the immutability convention: every Update/edit method returns
// a new Composer value rather than mutating the receiver.
type Composer struct {
	theme Theme

	// text is the current input buffer (may contain newlines).
	text string
	// cursor is the byte offset of the caret within text.
	cursor int

	// history holds shell-style recall state.
	history ComposerHistory

	// popup is the active popup (slash or file search), if any.
	popup composerPopup

	// taskRunning reflects whether a turn is in progress; it gates submit/queue
	// behavior (Tab queues while running, Enter still submits).
	taskRunning bool

	// search holds reverse-incremental (Ctrl+R) search state when active.
	search *reverseSearch

	// filesearch performs @mention completion lookups.
	filesearch FileSearchFunc

	// placeholder is the idle-composer prompt shown when the buffer is empty. It
	// is one entry from the rotating pool (composer_placeholder.go), chosen at
	// construction to mirror codex's per-run random PLACEHOLDERS selection.
	placeholder string

	// width is the last laid-out content width, used for rendering.
	width int
}

// FileSearchFunc resolves an @mention query to candidate paths. It is injected
// so the composer can call internal/filesearch without importing it directly in
// tests. Returning fewer than the requested results is fine.
//
// Port of the file_search.rs session the Rust composer drives.
type FileSearchFunc func(query string) []string

// composerPopupKind discriminates the active popup.
type composerPopupKind int

const (
	popupNone composerPopupKind = iota
	popupSlash
	popupFile
)

// composerPopup is the state of the slash or file-search popup.
type composerPopup struct {
	kind     composerPopupKind
	items    []popupItem
	selected int
	// queryStart is the byte offset in text where the popup's query token begins
	// (the '/' for slash, the '@' for file search).
	queryStart int
}

// popupItem is one row in a composer popup.
type popupItem struct {
	// label is the primary text (command name or path).
	label string
	// detail is secondary text (command description); may be empty.
	detail string
	// insert is the text inserted when the item is accepted (without the sigil).
	insert string
	// cmd is the slash command, valid for slash popups.
	cmd SlashCommand
}

// reverseSearch holds Ctrl+R incremental search state.
type reverseSearch struct {
	query string
	// draft is the buffer that was active when search opened, restored on Esc.
	draft  string
	cursor int
	match  string
	found  bool
}

// NewComposer builds an empty composer bound to a theme. fileSearch may be nil
// (then @mention completion is disabled). The idle placeholder is picked at
// random from the pool, matching codex's startup selection.
func NewComposer(theme Theme, fileSearch FileSearchFunc) Composer {
	return Composer{
		theme:       theme,
		history:     NewComposerHistory(),
		filesearch:  fileSearch,
		placeholder: pickComposerPlaceholder(nil),
	}
}

// Placeholder returns the idle-composer prompt shown when the buffer is empty.
func (c Composer) Placeholder() string { return c.placeholder }

// WithPlaceholder returns a copy of the composer with the idle placeholder set.
// Tests use it to pin the otherwise-random placeholder for deterministic output.
func (c Composer) WithPlaceholder(text string) Composer {
	c.placeholder = text
	return c
}

// Text returns the current input buffer.
func (c Composer) Text() string { return c.text }

// IsEmpty reports whether the buffer has no content.
func (c Composer) IsEmpty() bool { return c.text == "" }

// SetTaskRunning returns a copy of the composer with the task-running flag set.
func (c Composer) SetTaskRunning(running bool) Composer {
	c.taskRunning = running
	return c
}

// RecordSubmission records a submitted entry in history for later recall.
func (c Composer) RecordSubmission(text string) Composer {
	c.history = c.history.Record(text)
	return c
}

// PopupVisible reports whether a popup is currently shown.
func (c Composer) PopupVisible() bool { return c.popup.kind != popupNone }

// SearchActive reports whether Ctrl+R reverse search is active.
func (c Composer) SearchActive() bool { return c.search != nil }

// ComposerResult is the outcome of handling a key in the composer.
type ComposerResult struct {
	// Composer is the updated composer.
	Composer Composer
	// Submit is non-empty when the key produced a submission to send as a turn.
	Submit string
	// Queue is true when the submission should be queued (Tab while running).
	Queue bool
	// Cmd is a follow-up tea.Cmd (e.g. an async file-search lookup).
	Cmd tea.Cmd
}

// HandleKey processes a key event against the composer, returning the result.
//
// Port of ChatComposer::handle_key_event. Routing order: reverse search, then
// the active popup, then the editor. After handling, popups are re-synced.
func (c Composer) HandleKey(msg tea.KeyMsg) ComposerResult {
	if c.search != nil {
		return c.handleSearchKey(msg)
	}
	if c.popup.kind != popupNone {
		if res, handled := c.handlePopupKey(msg); handled {
			return res
		}
	}
	return c.handleEditorKey(msg)
}

// handleEditorKey handles a key when no popup/search is active.
func (c Composer) handleEditorKey(msg tea.KeyMsg) ComposerResult {
	switch msg.Type {
	case tea.KeyEnter:
		if msg.Alt {
			c = c.insert("\n")
			return c.synced(nil)
		}
		return c.submit(false)
	case tea.KeyTab:
		if c.taskRunning {
			return c.submit(true)
		}
		// Not running: Tab submits like Enter unless typing a '!' shell command.
		if strings.HasPrefix(strings.TrimSpace(c.text), "!") {
			c = c.insert("\t")
			return c.synced(nil)
		}
		return c.submit(false)
	case tea.KeyCtrlR:
		c.search = &reverseSearch{draft: c.text, cursor: c.cursor}
		return ComposerResult{Composer: c}
	case tea.KeyUp:
		if c.history.ShouldNavigate(c.text, c.cursor) {
			if entry, ok := c.history.Up(); ok {
				c.text = entry
				c.cursor = len(entry)
				return c.synced(nil)
			}
		}
		c = c.moveLineUp()
		return c.synced(nil)
	case tea.KeyDown:
		if c.history.Browsing() {
			if entry, ok := c.history.Down(); ok {
				c.text = entry
				c.cursor = len(entry)
				return c.synced(nil)
			}
		}
		c = c.moveLineDown()
		return c.synced(nil)
	case tea.KeyLeft:
		c = c.moveLeft()
		return c.synced(nil)
	case tea.KeyRight:
		c = c.moveRight()
		return c.synced(nil)
	case tea.KeyHome, tea.KeyCtrlA:
		c = c.moveLineStart()
		return c.synced(nil)
	case tea.KeyEnd, tea.KeyCtrlE:
		c = c.moveLineEnd()
		return c.synced(nil)
	case tea.KeyBackspace:
		c = c.deleteBackward()
		return c.synced(nil)
	case tea.KeyDelete:
		c = c.deleteForward()
		return c.synced(nil)
	case tea.KeyCtrlU:
		c = c.deleteToLineStart()
		return c.synced(nil)
	case tea.KeyCtrlW:
		c = c.deleteWordBackward()
		return c.synced(nil)
	case tea.KeySpace:
		c = c.insert(" ")
		return c.synced(nil)
	case tea.KeyRunes:
		c = c.insert(string(msg.Runes))
		return c.synced(nil)
	}
	return ComposerResult{Composer: c}
}

// submit prepares the current buffer for submission, clears it, and records
// history. queue indicates the Tab/queue path.
func (c Composer) submit(queue bool) ComposerResult {
	text := strings.TrimSpace(c.text)
	if text == "" {
		c.popup = composerPopup{}
		return ComposerResult{Composer: c}
	}
	c = c.RecordSubmission(text)
	c.text = ""
	c.cursor = 0
	c.popup = composerPopup{}
	c.history = c.history.ResetNavigation()
	return ComposerResult{Composer: c, Submit: text, Queue: queue}
}

// synced re-syncs popups against the current buffer/cursor and returns a result.
//
// Port of ChatComposer::sync_popups.
func (c Composer) synced(cmd tea.Cmd) ComposerResult {
	c = c.syncPopups()
	return ComposerResult{Composer: c, Cmd: cmd}
}

// syncPopups recomputes popup visibility/contents from the current token under
// the cursor.
func (c Composer) syncPopups() Composer {
	token, start, sigil := c.activeToken()
	switch sigil {
	case '/':
		// Slash popup only when the slash is at the very start of the buffer.
		if start == 0 {
			c.popup = c.slashPopup(token, start)
			return c
		}
	case '@':
		c.popup = c.filePopup(token, start)
		return c
	}
	c.popup = composerPopup{}
	return c
}

// activeToken returns the word under the cursor and its start offset, plus a
// leading sigil ('/' or '@') if present. Used to drive popups.
func (c Composer) activeToken() (token string, start int, sigil rune) {
	// Scan back from the cursor to the start of the current word.
	i := c.cursor
	for i > 0 {
		r, size := utf8.DecodeLastRuneInString(c.text[:i])
		if r == ' ' || r == '\n' || r == '\t' {
			break
		}
		i -= size
	}
	word := c.text[i:c.cursor]
	if word == "" {
		return "", c.cursor, 0
	}
	first, _ := utf8.DecodeRuneInString(word)
	if first == '/' || first == '@' {
		return word[1:], i, first
	}
	return word, i, 0
}

// slashPopup builds the slash-command popup filtered by the typed prefix.
func (c Composer) slashPopup(prefix string, start int) composerPopup {
	flags := BuiltinCommandFlags{}
	var items []popupItem
	for _, cmd := range BuiltinsForInput(flags) {
		name := cmd.Command()
		if prefix != "" && !strings.HasPrefix(name, strings.ToLower(prefix)) {
			continue
		}
		items = append(items, popupItem{
			label:  "/" + name,
			detail: cmd.Description(),
			insert: name,
			cmd:    cmd,
		})
	}
	if len(items) == 0 {
		return composerPopup{}
	}
	return composerPopup{kind: popupSlash, items: items, queryStart: start}
}

// filePopup builds the @file-search popup for the typed query.
func (c Composer) filePopup(query string, start int) composerPopup {
	if c.filesearch == nil {
		return composerPopup{}
	}
	paths := c.filesearch(query)
	if len(paths) == 0 {
		return composerPopup{}
	}
	items := make([]popupItem, 0, len(paths))
	for _, p := range paths {
		items = append(items, popupItem{label: p, insert: p})
	}
	return composerPopup{kind: popupFile, items: items, queryStart: start}
}

// handlePopupKey routes navigation/accept keys to the active popup. It returns
// (result, true) when the key was consumed by the popup.
func (c Composer) handlePopupKey(msg tea.KeyMsg) (ComposerResult, bool) {
	switch msg.Type {
	case tea.KeyUp:
		c.popup.selected = wrapIndex(c.popup.selected-1, len(c.popup.items))
		return ComposerResult{Composer: c}, true
	case tea.KeyDown, tea.KeyTab:
		c.popup.selected = wrapIndex(c.popup.selected+1, len(c.popup.items))
		return ComposerResult{Composer: c}, true
	case tea.KeyEsc:
		c.popup = composerPopup{}
		return ComposerResult{Composer: c}, true
	case tea.KeyEnter:
		return c.acceptPopup(), true
	}
	return ComposerResult{}, false
}

// acceptPopup applies the selected popup item to the buffer.
func (c Composer) acceptPopup() ComposerResult {
	if c.popup.selected < 0 || c.popup.selected >= len(c.popup.items) {
		c.popup = composerPopup{}
		return ComposerResult{Composer: c}
	}
	item := c.popup.items[c.popup.selected]
	switch c.popup.kind {
	case popupSlash:
		// Replace the typed token with the full "/name " (trailing space lets the
		// user type args, matching the Rust completion behavior).
		newText := "/" + item.insert
		if item.cmd.SupportsInlineArgs() {
			newText += " "
		}
		c.text = newText + c.text[c.cursor:]
		c.cursor = len(newText)
		c.popup = composerPopup{}
		return c.synced(nil)
	case popupFile:
		// Replace from the '@' sigil through the cursor with "@path ".
		insert := "@" + item.insert + " "
		c.text = c.text[:c.popup.queryStart] + insert + c.text[c.cursor:]
		c.cursor = c.popup.queryStart + len(insert)
		c.popup = composerPopup{}
		return c.synced(nil)
	}
	c.popup = composerPopup{}
	return ComposerResult{Composer: c}
}

// handleSearchKey handles keys while Ctrl+R reverse search is active.
func (c Composer) handleSearchKey(msg tea.KeyMsg) ComposerResult {
	s := *c.search
	switch msg.Type {
	case tea.KeyEsc:
		// Restore the draft that was active when search began.
		c.text = s.draft
		c.cursor = s.cursor
		c.search = nil
		return ComposerResult{Composer: c}
	case tea.KeyEnter:
		// Accept the preview as the editable draft.
		if s.found {
			c.text = s.match
			c.cursor = len(s.match)
		} else {
			c.text = s.draft
			c.cursor = s.cursor
		}
		c.search = nil
		return c.synced(nil)
	case tea.KeyCtrlR:
		// Move to the next older match.
		c, s = c.runSearch(s, true)
		c.search = &s
		return ComposerResult{Composer: c}
	case tea.KeyBackspace:
		if s.query != "" {
			_, size := utf8.DecodeLastRuneInString(s.query)
			s.query = s.query[:len(s.query)-size]
		}
		c, s = c.runSearch(s, false)
		c.search = &s
		return ComposerResult{Composer: c}
	case tea.KeyRunes, tea.KeySpace:
		if msg.Type == tea.KeySpace {
			s.query += " "
		} else {
			s.query += string(msg.Runes)
		}
		c, s = c.runSearch(s, false)
		c.search = &s
		return ComposerResult{Composer: c}
	}
	return ComposerResult{Composer: c}
}

// runSearch updates the search match for the current query and returns both the
// composer (with the history search cursor advanced) and the updated search
// state. advance moves to the next older match (Ctrl+R again); otherwise the
// search restarts from the newest entry. Returning the composer is required so
// the history search offset (mutated via a pointer method) persists.
func (c Composer) runSearch(s reverseSearch, advance bool) (Composer, reverseSearch) {
	match, ok := c.history.Search(s.query, advance)
	s.match = match
	s.found = ok
	return c, s
}

// CurrentSearch returns the active reverse-search query and preview, plus
// whether search is active. Used by View.
func (c Composer) CurrentSearch() (query, preview string, active bool) {
	if c.search == nil {
		return "", "", false
	}
	return c.search.query, c.search.match, true
}

// PopupRow is one rendered popup row exposed to the bottom-pane view.
type PopupRow struct {
	// Label is the primary text (command name or path).
	Label string
	// Detail is secondary text (description); may be empty.
	Detail string
}

// PopupRows returns the active popup's rows, the selected index, and whether a
// popup is visible. Used by the bottom-pane View.
func (c Composer) PopupRows() ([]PopupRow, int, bool) {
	if c.popup.kind == popupNone {
		return nil, 0, false
	}
	rows := make([]PopupRow, len(c.popup.items))
	for i, it := range c.popup.items {
		rows[i] = PopupRow{Label: it.label, Detail: it.detail}
	}
	return rows, c.popup.selected, true
}

// --- editing primitives (immutable) -----------------------------------------

func (c Composer) insert(s string) Composer {
	c.text = c.text[:c.cursor] + s + c.text[c.cursor:]
	c.cursor += len(s)
	c.history = c.history.ResetNavigation()
	return c
}

func (c Composer) deleteBackward() Composer {
	if c.cursor == 0 {
		return c
	}
	_, size := utf8.DecodeLastRuneInString(c.text[:c.cursor])
	c.text = c.text[:c.cursor-size] + c.text[c.cursor:]
	c.cursor -= size
	return c
}

func (c Composer) deleteForward() Composer {
	if c.cursor >= len(c.text) {
		return c
	}
	_, size := utf8.DecodeRuneInString(c.text[c.cursor:])
	c.text = c.text[:c.cursor] + c.text[c.cursor+size:]
	return c
}

func (c Composer) deleteToLineStart() Composer {
	start := c.lineStart()
	c.text = c.text[:start] + c.text[c.cursor:]
	c.cursor = start
	return c
}

func (c Composer) deleteWordBackward() Composer {
	i := c.cursor
	// Skip trailing spaces.
	for i > 0 {
		r, size := utf8.DecodeLastRuneInString(c.text[:i])
		if r != ' ' {
			break
		}
		i -= size
	}
	for i > 0 {
		r, size := utf8.DecodeLastRuneInString(c.text[:i])
		if r == ' ' || r == '\n' {
			break
		}
		i -= size
	}
	c.text = c.text[:i] + c.text[c.cursor:]
	c.cursor = i
	return c
}

func (c Composer) moveLeft() Composer {
	if c.cursor == 0 {
		return c
	}
	_, size := utf8.DecodeLastRuneInString(c.text[:c.cursor])
	c.cursor -= size
	return c
}

func (c Composer) moveRight() Composer {
	if c.cursor >= len(c.text) {
		return c
	}
	_, size := utf8.DecodeRuneInString(c.text[c.cursor:])
	c.cursor += size
	return c
}

func (c Composer) lineStart() int {
	i := strings.LastIndexByte(c.text[:c.cursor], '\n')
	if i < 0 {
		return 0
	}
	return i + 1
}

func (c Composer) lineEnd() int {
	i := strings.IndexByte(c.text[c.cursor:], '\n')
	if i < 0 {
		return len(c.text)
	}
	return c.cursor + i
}

func (c Composer) moveLineStart() Composer {
	c.cursor = c.lineStart()
	return c
}

func (c Composer) moveLineEnd() Composer {
	c.cursor = c.lineEnd()
	return c
}

func (c Composer) moveLineUp() Composer {
	start := c.lineStart()
	col := c.cursor - start
	if start == 0 {
		c.cursor = 0
		return c
	}
	prevEnd := start - 1
	prevStart := strings.LastIndexByte(c.text[:prevEnd], '\n') + 1
	target := prevStart + col
	if target > prevEnd {
		target = prevEnd
	}
	c.cursor = target
	return c
}

func (c Composer) moveLineDown() Composer {
	end := c.lineEnd()
	col := c.cursor - c.lineStart()
	if end >= len(c.text) {
		c.cursor = len(c.text)
		return c
	}
	nextStart := end + 1
	nextEnd := strings.IndexByte(c.text[nextStart:], '\n')
	var ne int
	if nextEnd < 0 {
		ne = len(c.text)
	} else {
		ne = nextStart + nextEnd
	}
	target := nextStart + col
	if target > ne {
		target = ne
	}
	c.cursor = target
	return c
}

// wrapIndex wraps i into [0, n).
func wrapIndex(i, n int) int {
	if n == 0 {
		return 0
	}
	i %= n
	if i < 0 {
		i += n
	}
	return i
}
